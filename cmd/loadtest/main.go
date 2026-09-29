// loadtest 是正确性实验工具，统计 HTTP 延迟和库存守恒；不是生产容量结论。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"seckill-lab/internal/auth"
	"seckill-lab/internal/domain"
)

type result struct {
	user    int64
	status  int
	body    domain.BuyResult
	code    string
	elapsed time.Duration
	err     error
}

var client = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 256}}

func call(method, url, token, key string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	return res.StatusCode, err
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}
func run() error {
	base := flag.String("url", "http://127.0.0.1:8080", "目标地址，仅压测你拥有的本地实验服务")
	n := flag.Int("n", 500, "不同用户数")
	c := flag.Int("c", 32, "并发数")
	repeat := flag.Int("repeat", 2, "每用户以同一 key 提交次数")
	product := flag.Int64("product", 1, "活动商品 ID")
	startUser := flag.Int64("start-user", 10000, "首个用户 ID；重复实验需换用户段或全新数据库")
	flag.Parse()
	if *n <= 0 || *n > 100000 || *c <= 0 || *c > 1000 || *repeat < 1 || *repeat > 10 || *product <= 0 || *startUser <= 0 || *startUser > 1000000-int64(*n) {
		return fmt.Errorf("参数越界：n 1..100000，c 1..1000，repeat 1..10，用户 ID 总范围 1..1000000")
	}
	*base = strings.TrimRight(*base, "/")
	var before domain.Product
	status, err := call("GET", fmt.Sprintf("%s/api/products/%d", *base, *product), "", "", nil, &before)
	if err != nil || status != 200 {
		return fmt.Errorf("读取商品失败 status=%d error=%v", status, err)
	}
	if before.Stock <= 0 {
		return fmt.Errorf("活动已经售罄；请使用有库存的新活动，避免把全是售罄响应的空实验当作成功")
	}
	var config struct {
		Demo bool `json:"demo_auth"`
	}
	status, err = call("GET", *base+"/api/config", "", "", nil, &config)
	if err != nil || status != 200 {
		return fmt.Errorf("读取运行模式失败")
	}
	tokens := make([]string, *n)
	secret := os.Getenv("AUTH_SECRET")
	if !config.Demo && len(secret) < 32 {
		return fmt.Errorf("MySQL 模式压测需要与服务端一致的 AUTH_SECRET")
	}
	for i := range tokens {
		user := *startUser + int64(i)
		if config.Demo {
			var response struct {
				Token string `json:"token"`
			}
			status, err := call("POST", *base+"/api/demo/token", "", "", map[string]int64{"user_id": user}, &response)
			if err != nil || status != 200 {
				return fmt.Errorf("演示令牌申请失败")
			}
			tokens[i] = response.Token
		} else {
			tokens[i] = auth.New(secret).Mint(user, time.Now().Add(time.Hour))
		}
	}
	totalRequests := (*n) * (*repeat)
	jobs := make(chan int)
	results := make(chan result, totalRequests)
	var wg sync.WaitGroup
	runID := time.Now().UnixNano()
	started := time.Now()
	for worker := 0; worker < *c; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				user := *startUser + int64(i)
				key := fmt.Sprintf("load-%d-%d", runID, user)
				var raw struct {
					domain.BuyResult
					Code string `json:"code"`
				}
				start := time.Now()
				status, err := call("POST", *base+"/api/orders", tokens[i], key, map[string]int64{"product_id": *product}, &raw)
				results <- result{user: user, status: status, body: raw.BuyResult, code: raw.Code, elapsed: time.Since(start), err: err}
			}
		}()
	}
	for i := 0; i < *n; i++ {
		for j := 0; j < *repeat; j++ {
			jobs <- i
		}
	}
	close(jobs)
	wg.Wait()
	close(results)
	elapsed := time.Since(started)
	counts := map[string]int{}
	orders := map[int64]domain.Order{}
	users := map[int64]int64{}
	durations := make([]time.Duration, 0, totalRequests)
	created, replayed, unexpected := 0, 0, 0
	for r := range results {
		durations = append(durations, r.elapsed)
		label := fmt.Sprintf("%d:%s", r.status, r.code)
		counts[label]++
		if r.err != nil {
			unexpected++
			continue
		}
		if r.status == 200 || r.status == 201 {
			o := r.body.Order
			if o.ID <= 0 || o.UserID != r.user || o.ProductID != *product || o.RequestKey != fmt.Sprintf("load-%d-%d", runID, r.user) {
				unexpected++
				continue // 错误响应不能用于随后索引用户令牌。
			}
			if old, ok := users[r.user]; ok && old != o.ID {
				unexpected++
			}
			users[r.user] = o.ID
			if old, ok := orders[o.ID]; ok && old.UserID != r.user {
				unexpected++
			}
			orders[o.ID] = o
			if r.status == 201 {
				created++
				if r.body.Replayed {
					unexpected++
				}
			} else {
				replayed++
				if !r.body.Replayed {
					unexpected++
				}
			}
		} else if r.status != 409 || r.code != "sold_out" {
			unexpected++
		}
	}
	var after domain.Product
	status, err = call("GET", fmt.Sprintf("%s/api/products/%d", *base, *product), "", "", nil, &after)
	if err != nil || status != 200 {
		return fmt.Errorf("无法读取最终库存")
	}
	for _, o := range orders {
		var persisted domain.Order
		status, err := call("GET", fmt.Sprintf("%s/api/orders/%d", *base, o.ID), tokens[int(o.UserID-*startUser)], "", nil, &persisted)
		if err != nil || status != 200 || persisted.ID != o.ID || persisted.UserID != o.UserID || persisted.ProductID != o.ProductID {
			unexpected++
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	target := min(before.Stock, int64(*n))
	report := map[string]any{
		"users": *n, "concurrency": *c, "requests": totalRequests,
		"elapsed_ms": elapsed.Milliseconds(), "http_requests_per_second": float64(totalRequests) / elapsed.Seconds(),
		"p50_ms":   float64(durations[(len(durations)-1)*50/100].Microseconds()) / 1000,
		"p95_ms":   float64(durations[(len(durations)-1)*95/100].Microseconds()) / 1000,
		"statuses": counts, "created": created, "replayed": replayed, "unique_orders": len(orders),
		"stock_before": before.Stock, "stock_after": after.Stock, "unexpected": unexpected,
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	if unexpected != 0 || after.Stock < 0 || int64(created) != target || int64(len(orders)) != target || before.Stock-after.Stock != target || int64(replayed) != target*int64(*repeat-1) {
		return fmt.Errorf("正确性校验未通过。检查限流/超时/用户重复/其他流量；此实验要求独立活动和未购买过的用户")
	}
	fmt.Println("PASS: 本轮响应、库存守恒与订单归属校验通过")
	return nil
}
