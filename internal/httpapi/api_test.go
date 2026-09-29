package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"seckill-lab/internal/auth"
	"seckill-lab/internal/domain"
	"seckill-lab/internal/service"
	"seckill-lab/internal/store"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureHandler(l Limiter) (http.Handler, *auth.Signer) {
	s := auth.New(strings.Repeat("a", 32))
	ps := domain.DemoProducts(time.Now())
	ps[0].Stock = 10
	ps[0].InitialStock = 10
	return New(service.New(store.NewMemory(ps)), s, l, 64, false, http.NotFoundHandler()), s
}
func request(h http.Handler, s *auth.Signer, user int64, method, path, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user > 0 {
		r.Header.Set("Authorization", "Bearer "+s.Mint(user, time.Now().Add(time.Hour)))
	}
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHTTPContract(t *testing.T) {
	h, s := fixtureHandler(nil)
	for _, tc := range []struct {
		user      int64
		body, key string
		status    int
	}{{0, `{"product_id":1}`, "request-0001", 401}, {1, `{"product_id":1,"user_id":2}`, "request-0001", 400}, {1, `{"product_id":1} {}`, "request-0001", 400}, {1, `{"product_id":-1}`, "request-0001", 400}, {1, `{"product_id":1}`, "short", 400}, {1, `{"product_id":1}`, "request-0001", 201}, {1, `{"product_id":1}`, "request-0001", 200}, {1, `{"product_id":1}`, "request-0002", 409}, {1, `{"product_id":2}`, "request-0001", 409}} {
		w := request(h, s, tc.user, "POST", "/api/orders", tc.key, tc.body)
		if w.Code != tc.status {
			t.Fatalf("want %d got %d %s", tc.status, w.Code, w.Body)
		}
	}
	if w := request(h, s, 2, "GET", "/api/orders/1", "", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request(h, s, 1, "GET", "/api/orders/1", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(h, s, 0, "POST", "/api/demo/token", "", `{"user_id":1}`); w.Code != 404 {
		t.Fatal("demo auth enabled in non-demo handler")
	}
	w := request(h, s, 0, "GET", "/metrics", "", "")
	if !strings.Contains(w.Body.String(), "seckill_orders_created_total 1") || !strings.Contains(w.Body.String(), "seckill_orders_replayed_total 1") {
		t.Fatal(w.Body)
	}
}
func TestConcurrentHTTP(t *testing.T) {
	h, s := fixtureHandler(nil)
	var wg sync.WaitGroup
	var created atomic.Int64
	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := request(h, s, int64(i), "POST", "/api/orders", fmt.Sprintf("request-%08d", i), `{"product_id":1}`)
			if w.Code == 201 {
				created.Add(1)
			} else if w.Code != 409 && w.Code != 429 {
				t.Errorf("%d %s", w.Code, w.Body)
			}
		}(i)
	}
	wg.Wait()
	w := request(h, s, 0, "GET", "/api/products/1", "", "")
	var p domain.Product
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if created.Load() != 10 || p.Stock != 0 {
		t.Fatal(created.Load(), p.Stock)
	}
}

type fakeLimiter struct {
	allow bool
	err   error
}

func (f fakeLimiter) Allow(context.Context) (bool, error) { return f.allow, f.err }
func (f fakeLimiter) Ping(context.Context) error          { return f.err }
func TestLimiterFailurePolicy(t *testing.T) {
	for _, tc := range []struct {
		lim    fakeLimiter
		status int
	}{{fakeLimiter{}, 429}, {fakeLimiter{err: errors.New("offline")}, 503}} {
		h, s := fixtureHandler(tc.lim)
		w := request(h, s, 1, "POST", "/api/orders", "request-0001", `{"product_id":1}`)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body)
		}
		w = request(h, s, 0, "GET", "/api/products/1", "", "")
		var p domain.Product
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if p.Stock != 10 {
			t.Fatal("limiter failure consumed stock")
		}
	}
}
