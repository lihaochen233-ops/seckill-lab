package mall

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type API struct {
	Engine                     *Engine
	Origins                    map[string]bool
	SecureCookie               bool
	Demo                       bool
	slots                      chan struct{}
	Accepted, Rejected, Failed atomic.Int64
}

func NewAPI(e *Engine, origins []string, secure, demo bool, ui http.Handler) *http.Server {
	a := &API{Engine: e, Origins: map[string]bool{}, SecureCookie: secure, Demo: demo, slots: make(chan struct{}, 256)}
	for _, o := range origins {
		a.Origins[strings.TrimRight(o, "/")] = true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, map[string]any{"demo": demo, "payment": "simulation", "name": "PULSE 脉冲生活", "server_time": nowMS()})
	})
	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	a.route(mux, "GET /api/auth/session", false, a.session)
	a.route(mux, "POST /api/auth/logout", false, a.logout)
	mux.HandleFunc("GET /api/products", a.products)
	mux.HandleFunc("GET /api/products/{id}", a.product)
	mux.HandleFunc("GET /api/activities", a.activities)
	a.route(mux, "GET /api/addresses", false, a.addresses)
	a.route(mux, "POST /api/addresses", false, a.saveAddress)
	a.route(mux, "PUT /api/addresses/{id}", false, a.saveAddress)
	a.route(mux, "DELETE /api/addresses/{id}", false, a.deleteAddress)
	a.route(mux, "GET /api/cart", false, a.cart)
	a.route(mux, "PUT /api/cart/{id}", false, a.setCart)
	a.route(mux, "POST /api/orders", false, a.checkout)
	a.route(mux, "GET /api/orders", false, a.orders)
	a.route(mux, "GET /api/orders/{id}", false, a.order)
	a.route(mux, "POST /api/orders/{id}/{action}", false, a.changeOrder)
	a.route(mux, "POST /api/activities/{id}/join", false, a.join)
	a.route(mux, "GET /api/tickets", false, a.tickets)
	a.route(mux, "GET /api/tickets/{id}", false, a.ticket)
	a.route(mux, "GET /api/admin/stats", true, a.stats)
	a.route(mux, "GET /api/admin/products", true, a.adminProducts)
	a.route(mux, "POST /api/admin/products", true, a.saveProduct)
	a.route(mux, "PUT /api/admin/products/{id}", true, a.saveProduct)
	a.route(mux, "POST /api/admin/products/{id}/restock", true, a.restock)
	a.route(mux, "GET /api/admin/orders", true, a.adminOrders)
	a.route(mux, "POST /api/admin/orders/{id}/ship", true, a.ship)
	a.route(mux, "GET /api/admin/activities", true, a.adminActivities)
	a.route(mux, "POST /api/admin/activities", true, a.createActivity)
	a.route(mux, "POST /api/admin/activities/{id}/{action}", true, a.manageActivity)
	a.route(mux, "GET /api/admin/ops", true, a.ops)
	a.route(mux, "POST /api/admin/ops/retry-dead", true, a.retryDead)
	a.route(mux, "GET /api/admin/audit", true, a.audit)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, notFound) })
	mux.Handle("/", ui)
	return &http.Server{Handler: a.middleware(mux), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	var f *Fault
	if !errors.As(err, &f) {
		slog.Error("mall request failed", "error", err)
		f = unavailable
	}
	send(w, f.Status, map[string]any{"code": f.Code, "message": f.Message, "request_id": w.Header().Get("X-Request-ID")})
}
// parse 限制请求体大小并拒绝未知字段，避免客户端传入意料之外的业务参数。
func parse(w http.ResponseWriter, r *http.Request, v any) error {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return &Fault{"unsupported_media_type", "请使用 application/json", 415}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return bad("请求数据格式不正确或包含不支持的字段")
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return bad("请求只能包含一个 JSON 对象")
	}
	return nil
}
func idFrom(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, bad("编号无效")
	}
	return id, nil
}
func paging(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	if size < 1 || size > 50 {
		size = 12
	}
	return page, size
}

type authed func(http.ResponseWriter, *http.Request, Session)

// route 在调用业务处理器前统一检查登录、管理员身份和写操作的 CSRF 令牌。
func (a *API) route(mux *http.ServeMux, pattern string, admin bool, fn authed) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("pulse_session")
		if err != nil {
			fail(w, &Fault{"unauthorized", "请先登录", 401})
			return
		}
		s, err := a.Engine.Cache.Session(r.Context(), cookie.Value)
		if err != nil {
			fail(w, err)
			return
		}
		if admin && s.User.Role != "admin" {
			fail(w, &Fault{"forbidden", "需要管理员权限", 403})
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
			fail(w, &Fault{"csrf_failed", "请求校验失败，请刷新页面后重试", 403})
			return
		}
		fn(w, r, s)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rid := randomID()[:16]
		w.Header().Set("X-Request-ID", rid)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if a.SecureCookie {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				slog.Error("request panic", "request_id", rid, "panic", v)
				if sw.status == 0 {
					fail(sw, unavailable)
				}
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				slog.Info("http", "request_id", rid, "method", r.Method, "route", r.Pattern, "status", sw.status, "duration_ms", time.Since(start).Milliseconds())
			}
		}()
		if r.Method != "GET" && r.Method != "HEAD" {
			// Cookie 会被浏览器自动携带，写请求还需限定来源，阻断跨站页面代用户提交。
			origin := r.Header.Get("Origin")
			if !a.Origins[origin] || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(sw, &Fault{"origin_rejected", "不接受来自此来源的请求", 403})
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// 通道满时立刻拒绝新请求，防止瞬时流量耗尽服务端内存和数据库连接。
			select {
			case a.slots <- struct{}{}:
				defer func() { <-a.slots }()
			default:
				sw.Header().Set("Retry-After", "1")
				fail(sw, &Fault{"busy", "服务繁忙，请稍后重试", 429})
				return
			}
		}
		next.ServeHTTP(sw, r)
	})
}
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
} // 不信任任意客户端传来的 X-Forwarded-For。
func (a *API) rate(w http.ResponseWriter, r *http.Request, key string, max int, window time.Duration) bool {
	ok, err := a.Engine.Cache.Allow(r.Context(), key, max, window)
	if err != nil {
		fail(w, err)
		return false
	}
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(maxInt(1, int(window.Seconds()))))
		fail(w, &Fault{"rate_limited", "操作太频繁，请稍后再试", 429})
		return false
	}
	return true
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
// 会话令牌放在 HttpOnly Cookie 中，页面脚本只能使用响应里的 CSRF 令牌。
func (a *API) setSession(w http.ResponseWriter, r *http.Request, u User) {
	token, s, err := a.Engine.Cache.NewSession(r.Context(), u)
	if err != nil {
		fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "pulse_session", Value: token, Path: "/", HttpOnly: true, Secure: a.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	send(w, 200, s)
}
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if !a.rate(w, r, "register:"+digest(remoteIP(r)), 5, time.Hour) {
		return
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	u, err := a.Engine.DB.CreateUser(r.Context(), in.Email, in.Name, in.Password, "customer")
	if err != nil {
		fail(w, err)
		return
	}
	a.setSession(w, r, u)
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if !a.rate(w, r, "login-ip:"+digest(remoteIP(r)), 20, time.Minute) {
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if len(in.Password) > 128 {
		fail(w, bad("密码过长"))
		return
	}
	if !a.rate(w, r, "login-user:"+digest(strings.ToLower(strings.TrimSpace(in.Email))), 8, time.Minute) {
		return
	}
	u, err := a.Engine.DB.LoginUser(r.Context(), in.Email, in.Password)
	if err != nil {
		fail(w, err)
		return
	}
	if cookie, err := r.Cookie("pulse_session"); err == nil {
		if err = a.Engine.Cache.Logout(r.Context(), cookie.Value); err != nil {
			fail(w, err)
			return
		}
	}
	a.setSession(w, r, u)
}
func (a *API) session(w http.ResponseWriter, r *http.Request, s Session) { send(w, 200, s) }
func (a *API) logout(w http.ResponseWriter, r *http.Request, s Session) {
	cookie, _ := r.Cookie("pulse_session")
	if err := a.Engine.Cache.Logout(r.Context(), cookie.Value); err != nil {
		fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "pulse_session", Value: "", Path: "/", HttpOnly: true, Secure: a.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	send(w, 200, map[string]bool{"ok": true})
}
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.Engine.DB.SQL.PingContext(ctx); err != nil {
		fail(w, err)
		return
	}
	if err := a.Engine.Cache.Client.Ping(ctx).Err(); err != nil {
		fail(w, err)
		return
	}
	if err := a.Engine.Broker.Ping(ctx); err != nil {
		fail(w, err)
		return
	}
	send(w, 200, map[string]any{"status": "ready", "mode": map[bool]string{true: "demo", false: "stack"}[a.Demo]})
}
func (a *API) products(w http.ResponseWriter, r *http.Request) {
	p, size := paging(r)
	q := r.URL.Query()
	if len(q.Get("q")) > 200 {
		fail(w, bad("搜索词过长"))
		return
	}
	items, err := a.Engine.DB.Products(r.Context(), q.Get("q"), q.Get("category"), q.Get("sort"), p, size, false)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, items)
}
func (a *API) product(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, err)
		return
	}
	p, err := a.Engine.DB.Product(r.Context(), id, false)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, p)
}
func (a *API) activities(w http.ResponseWriter, r *http.Request) {
	items, err := a.Engine.DB.Activities(r.Context(), false)
	if err != nil {
		fail(w, err)
		return
	}
	for i := range items {
		a.Engine.Cache.Decorate(r.Context(), &items[i])
	}
	send(w, 200, items)
}
func (a *API) addresses(w http.ResponseWriter, r *http.Request, s Session) {
	items, err := a.Engine.DB.Addresses(r.Context(), s.User.ID)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, items)
}
func (a *API) saveAddress(w http.ResponseWriter, r *http.Request, s Session) {
	var in Address
	if err := parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	in.ID = 0
	if r.Method == "PUT" {
		id, err := idFrom(r)
		if err != nil {
			fail(w, err)
			return
		}
		in.ID = id
	}
	out, err := a.Engine.DB.SaveAddress(r.Context(), s.User.ID, in)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
func (a *API) deleteAddress(w http.ResponseWriter, r *http.Request, s Session) {
	id, err := idFrom(r)
	if err == nil {
		err = a.Engine.DB.DeleteAddress(r.Context(), s.User.ID, id)
	}
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (a *API) cart(w http.ResponseWriter, r *http.Request, s Session) {
	items, err := a.Engine.DB.Cart(r.Context(), s.User.ID)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, items)
}
func (a *API) setCart(w http.ResponseWriter, r *http.Request, s Session) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Quantity int64 `json:"quantity"`
	}
	if err = parse(w, r, &in); err == nil {
		err = a.Engine.DB.SetCart(r.Context(), s.User.ID, id, in.Quantity)
	}
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (a *API) checkout(w http.ResponseWriter, r *http.Request, s Session) {
	if !a.rate(w, r, "checkout:"+strconv.FormatInt(s.User.ID, 10), 10, time.Minute) {
		return
	}
	var in struct {
		AddressID int64  `json:"address_id"`
		Items     []Line `json:"items"`
	}
	if err := parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	o, err := a.Engine.DB.Checkout(r.Context(), s.User.ID, in.AddressID, r.Header.Get("Idempotency-Key"), in.Items)
	if err != nil {
		fail(w, err)
		return
	}
	status := 201
	if o.Replayed {
		status = 200
	}
	send(w, status, o)
}
func (a *API) orders(w http.ResponseWriter, r *http.Request, s Session) {
	p, size := paging(r)
	out, err := a.Engine.DB.Orders(r.Context(), s.User.ID, r.URL.Query().Get("status"), p, size, false)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
func (a *API) order(w http.ResponseWriter, r *http.Request, s Session) {
	o, err := a.Engine.DB.Order(r.Context(), s.User.ID, r.PathValue("id"), false)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, o)
}
func (a *API) changeOrder(w http.ResponseWriter, r *http.Request, s Session) {
	action := r.PathValue("action")
	if action != "pay" && action != "cancel" && action != "refund" && action != "receive" {
		fail(w, notFound)
		return
	}
	if !a.rate(w, r, "order-action:"+strconv.FormatInt(s.User.ID, 10), 30, time.Minute) {
		return
	}
	o, err := a.Engine.DB.ChangeOrder(r.Context(), s.User.ID, r.PathValue("id"), action, "", false)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, o)
}
func (a *API) join(w http.ResponseWriter, r *http.Request, s Session) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, err)
		return
	}
	if !a.rate(w, r, "flash-user:"+strconv.FormatInt(s.User.ID, 10), 5, time.Second) {
		a.Rejected.Add(1)
		return
	}
	if !a.rate(w, r, "flash-global", 2000, time.Second) {
		a.Rejected.Add(1)
		return
	}
	var in struct {
		AddressID int64 `json:"address_id"`
	}
	if err = parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	t, err := a.Engine.Submit(r.Context(), s.User.ID, id, in.AddressID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			a.Rejected.Add(1)
		} else {
			a.Failed.Add(1)
		}
		fail(w, err)
		return
	}
	a.Accepted.Add(1)
	send(w, 202, t)
}
func (a *API) tickets(w http.ResponseWriter, r *http.Request, s Session) {
	out, err := a.Engine.DB.Tickets(r.Context(), s.User.ID)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
func (a *API) ticket(w http.ResponseWriter, r *http.Request, s Session) {
	if !a.rate(w, r, "poll:"+strconv.FormatInt(s.User.ID, 10), 10, time.Second) {
		return
	}
	t, err := a.Engine.DB.Ticket(r.Context(), s.User.ID, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, t)
}
func (a *API) stats(w http.ResponseWriter, r *http.Request, s Session) {
	out, err := a.Engine.DB.Stats(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	out["accepted_http"] = a.Accepted.Load()
	out["rejected_http"] = a.Rejected.Load()
	out["failed_http"] = a.Failed.Load()
	if count, err := a.Engine.Broker.DeadCount(r.Context()); err == nil {
		out["dead_letters"] = int64(count)
	} else {
		out["dead_letters"] = -1
	}
	send(w, 200, out)
}
func (a *API) adminProducts(w http.ResponseWriter, r *http.Request, s Session) {
	p, size := paging(r)
	out, err := a.Engine.DB.Products(r.Context(), r.URL.Query().Get("q"), "", "newest", p, size, true)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
func (a *API) saveProduct(w http.ResponseWriter, r *http.Request, s Session) {
	var p Product
	if err := parse(w, r, &p); err != nil {
		fail(w, err)
		return
	}
	p.ID = 0
	if r.Method == "PUT" {
		id, err := idFrom(r)
		if err != nil {
			fail(w, err)
			return
		}
		p.ID = id
	}
	out, err := a.Engine.DB.SaveProduct(r.Context(), s.User.ID, p)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
func (a *API) restock(w http.ResponseWriter, r *http.Request, s Session) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Quantity int64 `json:"quantity"`
	}
	if err = parse(w, r, &in); err == nil {
		err = a.Engine.DB.Restock(r.Context(), s.User.ID, id, in.Quantity)
	}
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (a *API) adminOrders(w http.ResponseWriter, r *http.Request, s Session) {
	p, size := paging(r)
	out, err := a.Engine.DB.Orders(r.Context(), 0, r.URL.Query().Get("status"), p, size, true)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
func (a *API) ship(w http.ResponseWriter, r *http.Request, s Session) {
	var in struct {
		Tracking string `json:"tracking"`
	}
	if err := parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	o, err := a.Engine.DB.ChangeOrder(r.Context(), s.User.ID, r.PathValue("id"), "ship", in.Tracking, true)
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, o)
}
func (a *API) adminActivities(w http.ResponseWriter, r *http.Request, s Session) {
	out, err := a.Engine.DB.Activities(r.Context(), true)
	if err != nil {
		fail(w, err)
		return
	}
	for i := range out {
		a.Engine.Cache.Decorate(r.Context(), &out[i])
	}
	send(w, 200, out)
}
func (a *API) createActivity(w http.ResponseWriter, r *http.Request, s Session) {
	var in struct {
		ProductID int64 `json:"product_id"`
		Price     int64 `json:"price_cents"`
		Stock     int64 `json:"stock"`
		StartsAt  int64 `json:"starts_at"`
		EndsAt    int64 `json:"ends_at"`
	}
	if err := parse(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	out, err := a.Engine.DB.CreateActivity(r.Context(), s.User.ID, Activity{ProductID: in.ProductID, Price: in.Price, Stock: in.Stock, StartsAt: in.StartsAt, EndsAt: in.EndsAt})
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 201, out)
}
func (a *API) manageActivity(w http.ResponseWriter, r *http.Request, s Session) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, err)
		return
	}
	switch r.PathValue("action") {
	case "publish", "rebuild":
		err = a.Engine.DB.RebuildActivity(r.Context(), a.Engine.Cache, s.User.ID, id)
	case "pause":
		err = a.Engine.DB.PauseActivity(r.Context(), a.Engine.Cache, s.User.ID, id)
	default:
		err = notFound
	}
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, map[string]bool{"ok": true})
}
func (a *API) ops(w http.ResponseWriter, r *http.Request, s Session) {
	events, err := a.Engine.DB.Outbox(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	activities, err := a.Engine.DB.Activities(r.Context(), true)
	if err != nil {
		fail(w, err)
		return
	}
	checks := []map[string]int64{}
	for _, act := range activities {
		check, err := a.Engine.DB.CheckInvariant(r.Context(), act.ID)
		if err != nil {
			fail(w, err)
			return
		}
		check["activity_id"] = act.ID
		checks = append(checks, check)
	}
	send(w, 200, map[string]any{"outbox": events, "invariants": checks})
}
func (a *API) retryDead(w http.ResponseWriter, r *http.Request, s Session) {
	n, err := a.Engine.Broker.RetryDead(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	_, err = a.Engine.DB.SQL.ExecContext(r.Context(), "INSERT INTO mall_audit(actor_id,action,target,created_at) VALUES(?,'queue.retry',?,?)", s.User.ID, strconv.Itoa(n), nowMS())
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, map[string]int{"requeued": n})
}
func (a *API) audit(w http.ResponseWriter, r *http.Request, s Session) {
	out, err := a.Engine.DB.AuditLog(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	send(w, 200, out)
}
