// mallbench 创建专用测试活动和用户，对完整商城 API 发压，并等待异步订单落库。
// 必须连接独立测试环境；不会删除测试数据，也不会调用真实支付。
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"seckill-lab/internal/mall"
)

type buyer struct {
	token, csrf, key string
	address          int64
}
type result struct {
	code     string
	duration time.Duration
	ticket   mall.Ticket
}

func unique() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	base := flag.String("base", "http://127.0.0.1:8088", "商城地址")
	origin := flag.String("origin", "", "Origin，默认为 base 的 scheme://host")
	n := flag.Int("n", 1000, "独立买家数 1..10000")
	concurrency := flag.Int("c", 64, "HTTP 并发数 1..256")
	stock := flag.Int64("stock", 100, "本次测试活动库存")
	allow := flag.Bool("allow-test-data", false, "确认向当前专用测试环境写入用户、商品、活动和订单")
	flag.Parse()
	if !*allow {
		return fmt.Errorf("仅限专用测试环境；需要 -allow-test-data。测试记录会保留")
	}
	if *n < 1 || *n > 10000 || *concurrency < 1 || *concurrency > 256 || *stock < 1 || *stock > int64(*n) {
		return fmt.Errorf("invalid n/c/stock")
	}
	u, err := url.Parse(*base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid base URL")
	}
	*base = strings.TrimRight(*base, "/")
	if *origin == "" {
		*origin = u.Scheme + "://" + u.Host
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if os.Getenv("MYSQL_DSN") == "" || os.Getenv("REDIS_ADDR") == "" {
		return fmt.Errorf("requires MYSQL_DSN and REDIS_ADDR of the SAME test stack")
	}
	db, err := mall.OpenDB(ctx, "mysql", os.Getenv("MYSQL_DSN"))
	if err != nil {
		return err
	}
	defer db.SQL.Close()
	cache := mall.NewCache(os.Getenv("REDIS_ADDR"), os.Getenv("REDIS_PASSWORD"))
	defer cache.Client.Close()
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{MaxIdleConns: *concurrency, MaxIdleConnsPerHost: *concurrency}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", *base+"/api/health", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("health check returned %d", response.StatusCode)
	}
	batch := "bench_" + unique()
	// 只在准备阶段生成一次随机不可知密码哈希，不把昂贵的注册流程计入下单延迟。
	hash, err := mall.HashPassword(unique() + unique())
	if err != nil {
		return err
	}
	buyers := make([]buyer, *n)
	for i := range buyers {
		email := fmt.Sprintf("%s_%d@example.invalid", batch, i)
		r, e := db.SQL.ExecContext(ctx, "INSERT INTO mall_users(email,name,password_hash,role,created_at) VALUES(?,? ,?,'customer',?)", email, "压测买家", hash, time.Now().UnixMilli())
		if e != nil {
			return e
		}
		id, e := r.LastInsertId()
		if e != nil {
			return e
		}
		addr, e := db.SaveAddress(ctx, id, mall.Address{Recipient: "测试收件人", Phone: "00000000000", Region: "测试地区", Detail: "仅用于并发实验的虚构地址"})
		if e != nil {
			return e
		}
		token, session, e := cache.NewSession(ctx, mall.User{ID: id, Email: email, Name: "压测买家", Role: "customer"})
		if e != nil {
			return e
		}
		buyers[i] = buyer{token, session.CSRF, unique(), addr.ID}
	}
	// 测试会话结束后清除登录凭证，订单和测试用户保留供审计。
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, b := range buyers {
			_ = cache.Logout(cleanup, b.token)
		}
	}()
	p, err := db.SaveProduct(ctx, 0, mall.Product{Name: batch, Subtitle: "专用并发测试商品", Category: "桌面数码", Image: "keyboard", Price: 10000, OriginalPrice: 10000, Stock: *stock, Status: "active"})
	if err != nil {
		return err
	}
	a, err := db.CreateActivity(ctx, 0, mall.Activity{ProductID: p.ID, Price: 100, Stock: *stock, StartsAt: time.Now().Add(-time.Minute).UnixMilli(), EndsAt: time.Now().Add(10 * time.Minute).UnixMilli()})
	if err != nil {
		return err
	}
	if err = db.RebuildActivity(ctx, cache, 0, a.ID); err != nil {
		return err
	}
	jobs := make(chan buyer)
	results := make(chan result, *n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range jobs {
				began := time.Now()
				body, _ := json.Marshal(map[string]int64{"address_id": b.address})
				req, e := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/activities/%d/join", *base, a.ID), bytes.NewReader(body))
				if e != nil {
					results <- result{code: "request_error", duration: time.Since(began)}
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", *origin)
				req.Header.Set("X-CSRF-Token", b.csrf)
				req.Header.Set("Idempotency-Key", b.key)
				req.AddCookie(&http.Cookie{Name: "pulse_session", Value: b.token})
				res, e := client.Do(req)
				if e != nil {
					results <- result{code: "network_error", duration: time.Since(began)}
					continue
				}
				raw, e := io.ReadAll(io.LimitReader(res.Body, 65536))
				res.Body.Close()
				out := result{duration: time.Since(began), code: fmt.Sprintf("http_%d", res.StatusCode)}
				if e == nil && res.StatusCode == 202 {
					if json.Unmarshal(raw, &out.ticket) == nil && out.ticket.ID != "" {
						out.code = "accepted"
					}
				} else {
					var fault struct {
						Code string `json:"code"`
					}
					if json.Unmarshal(raw, &fault) == nil && fault.Code != "" {
						out.code = fault.Code
					}
				}
				results <- out
			}
		}()
	}
	for _, b := range buyers {
		jobs <- b
	}
	close(jobs)
	wg.Wait()
	close(results)
	admission := time.Since(start)
	counts := map[string]int{}
	durations := []float64{}
	for r := range results {
		counts[r.code]++
		durations = append(durations, float64(r.duration.Microseconds())/1000)
	}
	sort.Float64s(durations)
	queued := int64(1)
	deadline := time.Now().Add(130 * time.Second)
	for queued > 0 && time.Now().Before(deadline) {
		if err = db.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_tickets WHERE activity_id=? AND status='queued'", a.ID).Scan(&queued); err != nil {
			return err
		}
		if queued > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	invariant, err := db.CheckInvariant(ctx, a.ID)
	if err != nil {
		return err
	}
	var orders, users int64
	if err = db.SQL.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT user_id) FROM mall_orders WHERE activity_id=?", a.ID).Scan(&orders, &users); err != nil {
		return err
	}
	quantile := func(p float64) float64 { return durations[min(len(durations)-1, int(float64(len(durations)-1)*p))] }
	report := map[string]any{"batch": batch, "activity_id": a.ID, "users": *n, "concurrency": *concurrency, "stock": *stock, "responses": counts, "admission_seconds": admission.Seconds(), "admission_requests_per_second": float64(*n) / admission.Seconds(), "admission_latency_ms": map[string]float64{"p50": quantile(.5), "p95": quantile(.95), "p99": quantile(.99)}, "end_to_end_seconds": time.Since(start).Seconds(), "orders": orders, "unique_buyers": users, "queued": queued, "invariant": invariant}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
	unexpected := *n - counts["accepted"] - counts["sold_out"]
	if unexpected != 0 || int64(counts["accepted"]) != *stock || orders != *stock || users != orders || queued != 0 || invariant["difference"] != 0 || invariant["remaining_stock"] != 0 {
		return fmt.Errorf("experiment FAILED: unexpected HTTP failures, missing orders or inventory mismatch; inspect report")
	}
	return nil
}
