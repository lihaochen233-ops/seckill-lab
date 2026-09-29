package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"seckill-lab/internal/auth"
	"seckill-lab/internal/domain"
	"seckill-lab/internal/service"
)

type Limiter interface {
	Allow(context.Context) (bool, error)
	Ping(context.Context) error
}
type API struct {
	svc                                 *service.Service
	signer                              *auth.Signer
	limiter                             Limiter
	slots                               chan struct{}
	demo                                bool
	created, replayed, rejected, failed atomic.Int64
}

func New(svc *service.Service, signer *auth.Signer, limiter Limiter, maxInFlight int, demo bool, ui http.Handler) http.Handler {
	a := &API{svc: svc, signer: signer, limiter: limiter, slots: make(chan struct{}, maxInFlight), demo: demo}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "alive"}) })
	mux.HandleFunc("GET /readyz", a.ready)
	mux.HandleFunc("GET /metrics", a.metrics)
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]bool{"demo_auth": a.demo}) })
	mux.HandleFunc("GET /api/products", a.products)
	mux.HandleFunc("GET /api/products/{id}", a.product)
	mux.HandleFunc("POST /api/orders", a.buy)
	mux.HandleFunc("GET /api/orders/{id}", a.order)
	if demo {
		mux.HandleFunc("POST /api/demo/token", a.demoToken)
	}
	mux.Handle("/", ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]string{"code": code, "message": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return domain.ErrInvalid
	}
	return nil
}
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, domain.ErrInvalid
	}
	return id, nil
}
func (a *API) user(w http.ResponseWriter, r *http.Request) (int64, bool) {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		failure(w, 401, "unauthorized", "请提供有效令牌")
		return 0, false
	}
	id, err := a.signer.Verify(strings.TrimPrefix(v, "Bearer "), time.Now())
	if err != nil {
		failure(w, 401, "unauthorized", "令牌无效或已过期")
		return 0, false
	}
	return id, true
}
func (a *API) businessError(w http.ResponseWriter, err error) {
	status, code := 503, "unavailable"
	switch {
	case errors.Is(err, domain.ErrInvalid):
		status, code = 400, "invalid_argument"
	case errors.Is(err, domain.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, domain.ErrSoldOut):
		status, code = 409, "sold_out"
	case errors.Is(err, domain.ErrNotStarted):
		status, code = 409, "not_started"
	case errors.Is(err, domain.ErrEnded):
		status, code = 409, "ended"
	case errors.Is(err, domain.ErrPurchased):
		status, code = 409, "already_purchased"
	case errors.Is(err, domain.ErrKeyConflict):
		status, code = 409, "idempotency_conflict"
	}
	if status == 503 {
		slog.Error("request storage failure", "error", err)
		failure(w, status, code, "暂时无法确认结果；下单请使用原 Idempotency-Key 重试")
	} else {
		failure(w, status, code, err.Error())
	}
}
func (a *API) products(w http.ResponseWriter, r *http.Request) {
	ps, err := a.svc.Products(r.Context())
	if err != nil {
		a.businessError(w, err)
		return
	}
	respond(w, 200, ps)
}
func (a *API) product(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.businessError(w, err)
		return
	}
	p, err := a.svc.Product(r.Context(), id)
	if err != nil {
		a.businessError(w, err)
		return
	}
	respond(w, 200, p)
}
func (a *API) buy(w http.ResponseWriter, r *http.Request) {
	user, ok := a.user(w, r)
	if !ok {
		a.rejected.Add(1)
		return
	}
	var body struct {
		ProductID int64 `json:"product_id"`
	}
	if err := decode(w, r, &body); err != nil {
		a.rejected.Add(1)
		failure(w, 400, "invalid_argument", "请求体仅接受正整数 product_id")
		return
	}
	// 非阻塞获取名额：过载时立即拒绝，不为每次请求额外启动 goroutine。
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		a.rejected.Add(1)
		w.Header().Set("Retry-After", "1")
		failure(w, 429, "busy", "下单请求过多，请稍后重试")
		return
	}
	if a.limiter != nil {
		allowed, err := a.limiter.Allow(r.Context())
		if err != nil {
			a.failed.Add(1)
			a.businessError(w, err)
			return
		}
		if !allowed {
			a.rejected.Add(1)
			w.Header().Set("Retry-After", "1")
			failure(w, 429, "rate_limited", "触发全局秒杀限流")
			return
		}
	}
	result, err := a.svc.Buy(r.Context(), domain.BuyInput{UserID: user, ProductID: body.ProductID, RequestKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		if errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrSoldOut) || errors.Is(err, domain.ErrNotStarted) || errors.Is(err, domain.ErrEnded) || errors.Is(err, domain.ErrPurchased) || errors.Is(err, domain.ErrKeyConflict) {
			a.rejected.Add(1)
		} else {
			a.failed.Add(1)
		}
		a.businessError(w, err)
		return
	}
	status := 201
	if result.Replayed {
		status = 200
		a.replayed.Add(1)
	} else {
		a.created.Add(1)
	}
	respond(w, status, result)
}
func (a *API) order(w http.ResponseWriter, r *http.Request) {
	user, ok := a.user(w, r)
	if !ok {
		return
	}
	id, err := pathID(r)
	if err != nil {
		a.businessError(w, err)
		return
	}
	o, err := a.svc.Order(r.Context(), user, id)
	if err != nil {
		a.businessError(w, err)
		return
	}
	respond(w, 200, o)
}
func (a *API) demoToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int64 `json:"user_id"`
	}
	if decode(w, r, &body) != nil || body.UserID < 1 || body.UserID > 1000000 {
		failure(w, 400, "invalid_argument", "演示用户编号范围 1～1000000")
		return
	}
	respond(w, 200, map[string]string{"token": a.signer.Mint(body.UserID, time.Now().Add(time.Hour))})
}
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Ping(r.Context()); err != nil {
		failure(w, 503, "not_ready", "存储未就绪")
		return
	}
	if a.limiter != nil {
		if err := a.limiter.Ping(r.Context()); err != nil {
			failure(w, 503, "not_ready", "限流组件未就绪")
			return
		}
	}
	respond(w, 200, map[string]string{"status": "ready"})
}
func (a *API) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for _, m := range []struct {
		name  string
		value int64
	}{{"created", a.created.Load()}, {"replayed", a.replayed.Load()}, {"rejected", a.rejected.Load()}, {"failed", a.failed.Load()}} {
		fmt.Fprintf(w, "# TYPE seckill_orders_%s_total counter\nseckill_orders_%s_total %d\n", m.name, m.name, m.value)
	}
}
