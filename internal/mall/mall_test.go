package mall

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
)

var testCtx = context.Background()

type fixture struct {
	d         *DB
	c         *Cache
	e         *Engine
	mr        *miniredis.Miniredis
	p         Product
	a         Activity
	users     []User
	addresses []Address
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T, users int, stock int64) *fixture {
	t.Helper()
	dialect, dsn := "sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "mall.db"))+"?_pragma=busy_timeout(5000)"
	if source := os.Getenv("TEST_MYSQL_DSN"); source != "" {
		cfg, err := mysql.ParseDSN(source)
		must(t, err)
		cfg.DBName = ""
		admin, err := sql.Open("mysql", cfg.FormatDSN())
		must(t, err)
		name := "pulse_test_" + randomID()[:16]
		_, err = admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci")
		must(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec("DROP DATABASE " + name)
			if err != nil {
				t.Error(err)
			}
			admin.Close()
		})
		cfg.DBName = name
		dialect = "mysql"
		dsn = cfg.FormatDSN()
	}
	d, err := OpenDB(testCtx, dialect, dsn)
	must(t, err)
	t.Cleanup(func() { d.SQL.Close() })
	must(t, d.Migrate(testCtx))
	mr := miniredis.RunT(t)
	c := NewCache(mr.Addr(), "")
	t.Cleanup(func() { c.Client.Close() })
	f := &fixture{d: d, c: c, mr: mr}
	f.e = &Engine{DB: d, Cache: c, Broker: NewLocalBroker()}
	// 压测数据直接写入测试库，避免把 PBKDF2 注册耗时混进下单并发测试。
	for i := 0; i < users; i++ {
		r, err := d.SQL.Exec("INSERT INTO mall_users(email,name,password_hash,role,created_at) VALUES(?,?,'test-only','customer',?)", fmt.Sprintf("u%d@test.local", i), "测试用户", nowMS())
		must(t, err)
		id, err := r.LastInsertId()
		must(t, err)
		f.users = append(f.users, User{ID: id, Name: "测试用户", Role: "customer"})
		addr, err := d.SaveAddress(testCtx, id, Address{Recipient: "测试收件人", Phone: "00000000000", Region: "测试地区", Detail: "测试地址，不用于配送"})
		must(t, err)
		f.addresses = append(f.addresses, addr)
	}
	f.p, err = d.SaveProduct(testCtx, 0, Product{Name: "测试键盘", Category: "桌面数码", Image: "keyboard", Price: 10000, OriginalPrice: 20000, Stock: stock + 100, Status: "active"})
	must(t, err)
	f.a, err = d.CreateActivity(testCtx, 0, Activity{ProductID: f.p.ID, Price: 5000, Stock: stock, StartsAt: nowMS() - 60000, EndsAt: nowMS() + 3600000})
	must(t, err)
	must(t, d.RebuildActivity(testCtx, c, 0, f.a.ID))
	f.a, err = d.Activity(testCtx, f.a.ID)
	must(t, err)
	return f
}
func (f *fixture) submit(i int) (Ticket, error) {
	return f.e.Submit(testCtx, f.users[i].ID, f.a.ID, f.addresses[i].ID, fmt.Sprintf("request_%08d", i))
}
func (f *fixture) check(t *testing.T) {
	t.Helper()
	v, err := f.d.CheckInvariant(testCtx, f.a.ID)
	must(t, err)
	if v["difference"] != 0 || v["remaining_stock"] < 0 {
		t.Fatalf("stock invariant: %v", v)
	}
}
func (f *fixture) cacheStock(t *testing.T) int64 {
	t.Helper()
	n, err := f.c.Client.HGet(testCtx, flashKeys(f.a.ID)[0], "stock").Int64()
	must(t, err)
	return n
}
func (f *fixture) drain(t *testing.T) {
	t.Helper()
	for {
		ev, err := f.d.claimEvent(testCtx)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		must(t, err)
		must(t, f.e.handle(testCtx, ev))
		must(t, f.d.finishEvent(testCtx, ev, nil))
	}
}

func TestFlashConcurrencyAndDuplicateDelivery(t *testing.T) {
	users := 200
	if s := os.Getenv("MALL_STRESS_USERS"); s != "" {
		n, err := strconv.Atoi(s)
		must(t, err)
		if n < 100 || n > 20000 {
			t.Fatal("MALL_STRESS_USERS must be 100..20000")
		}
		users = n
	}
	const stock = 30
	f := newFixture(t, users, stock)
	start := time.Now()
	results := make(chan Ticket, users)
	errs := make(chan error, users)
	slots := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i := 0; i < users; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			tk, err := f.submit(i)
			if err != nil {
				errs <- err
			} else {
				results <- tk
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	elapsed := time.Since(start)
	if len(results) != stock {
		t.Fatalf("winners=%d want=%d", len(results), stock)
	}
	for err := range errs {
		var fault *Fault
		if !errors.As(err, &fault) || fault.Code != "sold_out" {
			t.Fatalf("unexpected failure: %v", err)
		}
	}
	accepted := []Ticket{}
	for tk := range results {
		accepted = append(accepted, tk)
		must(t, f.d.FinalizeTicket(testCtx, tk.ID))
		must(t, f.d.FinalizeTicket(testCtx, tk.ID))
	}
	f.drain(t)
	f.check(t)
	var count int
	must(t, f.d.SQL.QueryRow("SELECT COUNT(*) FROM mall_orders").Scan(&count))
	if count != stock {
		t.Fatalf("orders=%d", count)
	}
	if f.cacheStock(t) != 0 {
		t.Fatal("Redis stock must be zero")
	}
	// 两次取消、重复释放不应增加两次库存。
	tk := accepted[0]
	o, err := f.d.Ticket(testCtx, tk.UserID, tk.ID)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, tk.UserID, o.OrderID, "cancel", "", false)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, tk.UserID, o.OrderID, "cancel", "", false)
	must(t, err)
	f.drain(t)
	must(t, f.c.Release(testCtx, Release{f.a.ID, tk.ID, f.a.Epoch}))
	f.check(t)
	if f.cacheStock(t) != 1 {
		t.Fatal("duplicate release inflated stock")
	}
	t.Logf("dialect=%s users=%d concurrency=64 accepted=%d sold_out=%d orders=%d admission_duration=%s", f.d.Dialect, users, stock, users-stock, count, elapsed)
}

func TestCheckoutPaymentAndOwnership(t *testing.T) {
	f := newFixture(t, 2, 3)
	uid, addr := f.users[0].ID, f.addresses[0].ID
	lines := []Line{{f.p.ID, 2}}
	o, err := f.d.Checkout(testCtx, uid, addr, "checkout_AA", lines)
	must(t, err)
	replay, err := f.d.Checkout(testCtx, uid, addr, "checkout_AA", lines)
	must(t, err)
	if replay.ID != o.ID || !replay.Replayed {
		t.Fatal("checkout not idempotent")
	}
	_, err = f.d.Checkout(testCtx, uid, addr, "checkout_AA", []Line{{f.p.ID, 1}})
	if err == nil {
		t.Fatal("changed payload accepted")
	}
	lower, err := f.d.Checkout(testCtx, uid, addr, "checkout_aa", lines)
	must(t, err)
	if lower.ID == o.ID {
		t.Fatal("request keys must be case-sensitive")
	}
	_, err = f.d.Checkout(testCtx, f.users[1].ID, addr, "foreign_addr", lines)
	if err != notFound {
		t.Fatalf("foreign address: %v", err)
	}
	_, err = f.d.ChangeOrder(testCtx, f.users[1].ID, o.ID, "pay", "", false)
	if err != notFound {
		t.Fatalf("foreign order: %v", err)
	}
	for i := 0; i < 2; i++ {
		_, err = f.d.ChangeOrder(testCtx, uid, o.ID, "pay", "", false)
		must(t, err)
	}
	var payments int
	must(t, f.d.SQL.QueryRow("SELECT COUNT(*) FROM mall_payments").Scan(&payments))
	if payments != 1 {
		t.Fatal("duplicate payment")
	}
	_, err = f.d.ChangeOrder(testCtx, uid, o.ID, "refund", "", false)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, uid, o.ID, "refund", "", false)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, uid, lower.ID, "pay", "", false)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, 0, lower.ID, "ship", "DEMO-12345", true)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, uid, lower.ID, "refund", "", false)
	if err == nil {
		t.Fatal("shipped order refunded")
	}
	received, err := f.d.ChangeOrder(testCtx, uid, lower.ID, "receive", "", false)
	must(t, err)
	if received.Status != "completed" {
		t.Fatal(received.Status)
	}
	p, err := f.d.Product(testCtx, f.p.ID, true)
	must(t, err)
	if p.Stock != 98 {
		t.Fatalf("stock=%d want98", p.Stock)
	}
}

func TestTransactionRollbacks(t *testing.T) {
	f := newFixture(t, 1, 3)
	trigger := "CREATE TRIGGER fail_order BEFORE INSERT ON mall_orders BEGIN SELECT RAISE(ABORT,'injected failure'); END"
	if f.d.Dialect == "mysql" {
		trigger = "CREATE TRIGGER fail_order BEFORE INSERT ON mall_orders FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected failure'"
	}
	_, err := f.d.SQL.Exec(trigger)
	must(t, err)
	_, err = f.d.Checkout(testCtx, f.users[0].ID, f.addresses[0].ID, "rollback_normal", []Line{{f.p.ID, 2}})
	if err == nil {
		t.Fatal("injected failure ignored")
	}
	p, err := f.d.Product(testCtx, f.p.ID, true)
	must(t, err)
	if p.Stock != 100 {
		t.Fatal("partial stock deduction")
	}
	tk, err := f.submit(0)
	must(t, err)
	if f.d.FinalizeTicket(testCtx, tk.ID) == nil {
		t.Fatal("injected consumer failure ignored")
	}
	f.check(t)
	a, err := f.d.Activity(testCtx, f.a.ID)
	must(t, err)
	if a.Stock != 3 {
		t.Fatal("partial flash deduction")
	}
	_, err = f.d.SQL.Exec("DROP TRIGGER fail_order")
	must(t, err)
	must(t, f.d.FinalizeTicket(testCtx, tk.ID))
	f.check(t)
}

func TestOrphanReservationAndLateRequest(t *testing.T) {
	f := newFixture(t, 2, 2)
	h := Hold{ID: ticketID(f.users[0].ID, f.a.ID, "orphan_key"), UserID: f.users[0].ID, ActivityID: f.a.ID, RequestKey: "orphan_key", CreatedAt: nowMS()}
	epoch, _, err := f.c.Reserve(testCtx, h)
	must(t, err)
	h.Epoch = epoch
	// 模拟 Lua 成功后 API 崩溃。补偿先写墓碑，迟到请求只能读到拒绝终态。
	must(t, f.d.FenceHold(testCtx, h))
	f.drain(t)
	late, err := f.e.Submit(testCtx, h.UserID, h.ActivityID, f.addresses[0].ID, h.RequestKey)
	must(t, err)
	if late.Status != "rejected" {
		t.Fatal("late request escaped tombstone")
	}
	must(t, f.c.Release(testCtx, Release{f.a.ID, h.ID, epoch}))
	if f.cacheStock(t) != 2 {
		t.Fatal("orphan compensation duplicated")
	}
	f.check(t)
}

func TestRebuildEpochAndMissingCache(t *testing.T) {
	f := newFixture(t, 3, 3)
	tk, err := f.submit(0)
	must(t, err)
	must(t, f.d.FinalizeTicket(testCtx, tk.ID))
	f.check(t)
	queued, err := f.submit(1)
	must(t, err)
	must(t, f.c.Client.Del(testCtx, flashKeys(f.a.ID)[0]).Err())
	_, err = f.submit(2)
	var fault *Fault
	if !errors.As(err, &fault) || fault.Code != "flash_not_ready" {
		t.Fatalf("cache miss should fail closed: %v", err)
	}
	must(t, f.d.RebuildActivity(testCtx, f.c, 0, f.a.ID))
	must(t, f.d.FinalizeTicket(testCtx, queued.ID))
	f.drain(t)
	if f.cacheStock(t) != 2 {
		t.Fatal("old release inflated new epoch")
	}
	q, err := f.d.Ticket(testCtx, queued.UserID, queued.ID)
	must(t, err)
	if q.Status != "rejected" {
		t.Fatal("old queue admitted after rebuild")
	}
	existing, err := f.d.Ticket(testCtx, tk.UserID, tk.ID)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, tk.UserID, existing.OrderID, "cancel", "", false)
	must(t, err)
	f.drain(t)
	if f.cacheStock(t) != 3 {
		t.Fatal("new-epoch cancellation not restored")
	}
	f.check(t)
}

func TestExpiryAndArchive(t *testing.T) {
	f := newFixture(t, 2, 4)
	tk, err := f.submit(0)
	must(t, err)
	must(t, f.d.FinalizeTicket(testCtx, tk.ID))
	tk, err = f.d.Ticket(testCtx, tk.UserID, tk.ID)
	must(t, err)
	_, err = f.d.SQL.Exec("UPDATE mall_orders SET expires_at=? WHERE id=?", nowMS()-1, tk.OrderID)
	must(t, err)
	must(t, f.d.ExpireOrders(testCtx))
	must(t, f.d.ExpireOrders(testCtx))
	f.drain(t)
	if f.cacheStock(t) != 4 {
		t.Fatal("timeout stock")
	}
	second, err := f.submit(1)
	must(t, err)
	must(t, f.d.FinalizeTicket(testCtx, second.ID))
	second, err = f.d.Ticket(testCtx, second.UserID, second.ID)
	must(t, err)
	_, err = f.d.SQL.Exec("UPDATE mall_activities SET starts_at=?,ends_at=? WHERE id=?", nowMS()-300000, nowMS()-140000, f.a.ID)
	must(t, err)
	must(t, f.d.ArchiveExpired(testCtx, f.c))
	must(t, f.d.ArchiveExpired(testCtx, f.c))
	f.check(t)
	_, err = f.d.ChangeOrder(testCtx, second.UserID, second.OrderID, "cancel", "", false)
	must(t, err)
	f.check(t)
	p, err := f.d.Product(testCtx, f.p.ID, true)
	must(t, err)
	if p.Stock != 104 {
		t.Fatalf("archive stock=%d", p.Stock)
	}
}

func TestOutboxLeaseRetryAndWorker(t *testing.T) {
	f := newFixture(t, 1, 1)
	tk, err := f.submit(0)
	must(t, err)
	ev, err := f.d.claimEvent(testCtx)
	must(t, err)
	_, err = f.d.claimEvent(testCtx)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("lease did not exclude duplicate claim")
	}
	must(t, f.d.finishEvent(testCtx, ev, errors.New("injected broker outage")))
	_, err = f.d.claimEvent(testCtx)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("retry did not back off")
	}
	_, err = f.d.SQL.Exec("UPDATE mall_outbox SET available_at=0,lease_until=0")
	must(t, err)
	ctx, cancel := context.WithCancel(testCtx)
	f.e.Start(ctx)
	defer func() { cancel(); f.e.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current, err := f.d.Ticket(testCtx, tk.UserID, tk.ID)
		must(t, err)
		if current.Status == "ordered" {
			f.check(t)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("outbox recovery did not finish order")
}

func TestHTTPSecurityAndPassword(t *testing.T) {
	f := newFixture(t, 1, 1)
	hash, err := HashPassword("Password2026!")
	must(t, err)
	if !CheckPassword(hash, "Password2026!") || CheckPassword(hash, "wrong") {
		t.Fatal("password verification")
	}
	_, err = f.d.SQL.Exec("UPDATE mall_users SET password_hash=? WHERE id=?", hash, f.users[0].ID)
	must(t, err)
	h := NewAPI(f.e, []string{"http://mall.test"}, true, false, http.NotFoundHandler()).Handler
	req := func(method, path, body, token, csrf, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "pulse_session", Value: token})
		}
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := req("POST", "/api/auth/login", `{"email":"u0@test.local","password":"Password2026!"}`, "", "", "http://mall.test")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	var session Session
	must(t, json.Unmarshal(login.Body.Bytes(), &session))
	token := cookies[0].Value
	cases := []struct {
		method, path, body, token, csrf, origin string
		want                                    int
	}{
		{"GET", "/api/orders", "", "", "", "", 401},
		{"GET", "/api/admin/stats", "", token, "", "", 403},
		{"POST", "/api/auth/logout", "{}", token, "", "http://mall.test", 403},
		{"POST", "/api/auth/logout", "{}", token, session.CSRF, "https://evil.test", 403},
		{"POST", "/api/auth/register", `{"email":"a@b.test","name":"测试","password":"Password2026!","role":"admin"}`, "", "", "http://mall.test", 400},
		{"POST", "/api/orders", `{"address_id":1,"items":[],"total_cents":1}`, token, session.CSRF, "http://mall.test", 400},
		{"POST", "/api/auth/logout", "{}", token, session.CSRF, "http://mall.test", 200},
		{"GET", "/api/orders", "", token, "", "", 401},
	}
	for _, c := range cases {
		w := req(c.method, c.path, c.body, c.token, c.csrf, c.origin)
		if w.Code != c.want {
			t.Errorf("%s %s: status=%d want=%d body=%s", c.method, c.path, w.Code, c.want, w.Body.String())
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Error("CSP missing")
		}
	}
	allowed, err := f.c.Allow(testCtx, "test-limit", 1, time.Second)
	must(t, err)
	if !allowed {
		t.Fatal("first rate request rejected")
	}
	allowed, err = f.c.Allow(testCtx, "test-limit", 1, time.Second)
	must(t, err)
	if allowed {
		t.Fatal("rate limit bypassed")
	}
	f.mr.FastForward(time.Second)
	allowed, err = f.c.Allow(testCtx, "test-limit", 1, time.Second)
	must(t, err)
	if !allowed {
		t.Fatal("rate TTL failed")
	}
}

func TestConcurrentReplayAndPayCancel(t *testing.T) {
	f := newFixture(t, 1, 2)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.submit(0); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	tickets, err := f.d.Tickets(testCtx, f.users[0].ID)
	must(t, err)
	if len(tickets) != 1 {
		t.Fatal("duplicate tickets")
	}
	other, err := f.d.SaveAddress(testCtx, f.users[0].ID, Address{Recipient: "另一个地址", Phone: "00000000000", Region: "测试地区", Detail: "并发重试参数校验地址"})
	must(t, err)
	// 模拟重试在最初查询时未看到票据，进入持久化层后才看到另一个请求的提交。
	_, err = f.d.AcceptTicket(testCtx, tickets[0], other.ID)
	var mismatch *Fault
	if !errors.As(err, &mismatch) || mismatch.Code != "idempotency_conflict" {
		t.Fatalf("concurrent request changed address: %v", err)
	}
	f.drain(t)
	tk, err := f.d.Ticket(testCtx, f.users[0].ID, tickets[0].ID)
	must(t, err)
	results := make(chan error, 2)
	for _, action := range []string{"pay", "cancel"} {
		wg.Add(1)
		go func(action string) {
			defer wg.Done()
			_, err := f.d.ChangeOrder(testCtx, tk.UserID, tk.OrderID, action, "", false)
			results <- err
		}(action)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			var fault *Fault
			if !errors.As(err, &fault) || fault.Code != "invalid_order_state" {
				t.Fatal(err)
			}
		}
	}
	if success != 1 {
		t.Fatal("payment and cancellation both succeeded")
	}
	o, err := f.d.Order(testCtx, tk.UserID, tk.OrderID, false)
	must(t, err)
	if o.Status == "paid" {
		_, err = f.d.ChangeOrder(testCtx, tk.UserID, tk.OrderID, "refund", "", false)
		must(t, err)
	}
	f.drain(t)
	f.check(t)
	if f.cacheStock(t) != 2 {
		t.Fatal("concurrent actions restored wrong stock")
	}
}

func TestDatabaseGuardAndQueueTimeout(t *testing.T) {
	f := newFixture(t, 3, 1)
	// 模拟 Redis 恢复旧快照，准入放多了名额，SQL 仍必须守住最后库存。
	must(t, f.c.Client.HSet(testCtx, flashKeys(f.a.ID)[0], "stock", 3).Err())
	first, err := f.submit(0)
	must(t, err)
	second, err := f.submit(1)
	must(t, err)
	third, err := f.submit(2)
	must(t, err)
	must(t, f.d.FinalizeTicket(testCtx, first.ID))
	must(t, f.d.FinalizeTicket(testCtx, second.ID))
	second, err = f.d.Ticket(testCtx, second.UserID, second.ID)
	must(t, err)
	if second.Status != "rejected" {
		t.Fatal("SQL allowed oversell")
	}
	_, err = f.d.SQL.Exec("UPDATE mall_tickets SET created_at=? WHERE id=?", nowMS()-121000, third.ID)
	must(t, err)
	must(t, f.d.RejectStaleTickets(testCtx))
	must(t, f.d.FinalizeTicket(testCtx, third.ID))
	third, err = f.d.Ticket(testCtx, third.UserID, third.ID)
	must(t, err)
	if third.Status != "rejected" {
		t.Fatal("timed-out ticket created an order")
	}
	f.check(t)
	var count int
	must(t, f.d.SQL.QueryRow("SELECT COUNT(*) FROM mall_orders").Scan(&count))
	if count != 1 {
		t.Fatal("incorrect order count")
	}
}
