package mall

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// 使用专用测试 Redis / RabbitMQ。不要把 TEST_* 指向已有业务环境。
func TestRealMiddlewareLifecycle(t *testing.T) {
	url, addr := os.Getenv("TEST_AMQP_URL"), os.Getenv("TEST_REDIS_ADDR")
	if url == "" || addr == "" {
		t.Skip("requires dedicated TEST_AMQP_URL and TEST_REDIS_ADDR")
	}
	f := newFixture(t, 1, 2)
	c := NewCache(addr, os.Getenv("TEST_REDIS_PASSWORD"))
	defer c.Client.Close()
	f.c = c
	f.e.Cache = c
	// 该测试独占测试中间件；只使用 mall 测试活动及 pulse.flash 队列。
	must(t, c.Client.Del(testCtx, flashKeys(f.a.ID)...).Err())
	defer c.Client.Del(testCtx, flashKeys(f.a.ID)...)
	must(t, f.d.RebuildActivity(testCtx, c, 0, f.a.ID))
	broker := NewRabbit(url)
	defer broker.Close()
	must(t, broker.Ping(testCtx))
	f.e.Broker = broker
	ctx, cancel := context.WithCancel(testCtx)
	f.e.Start(ctx)
	defer func() { cancel(); f.e.Wait() }()
	tk, err := f.submit(0)
	must(t, err)
	waitUntil(t, 30*time.Second, func() bool { x, e := f.d.Ticket(testCtx, tk.UserID, tk.ID); return e == nil && x.Status == "ordered" })
	current, err := f.d.Ticket(testCtx, tk.UserID, tk.ID)
	must(t, err)
	_, err = f.d.ChangeOrder(testCtx, tk.UserID, current.OrderID, "cancel", "", false)
	must(t, err)
	waitUntil(t, 20*time.Second, func() bool {
		n, e := c.Client.HGet(testCtx, flashKeys(f.a.ID)[0], "stock").Int64()
		return e == nil && n == 2
	})
	f.check(t)
	// 未知类型会失败重试，超过队列限制后进入死信。
	must(t, broker.Publish(testCtx, Event{ID: randomID(), Kind: "test.poison", Payload: "{}"}))
	waitUntil(t, 30*time.Second, func() bool { n, e := broker.DeadCount(testCtx); return e == nil && n > 0 })
	cancel()
	f.e.Wait()
	retryCtx, stop := context.WithCancel(testCtx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- broker.Consume(retryCtx, func(context.Context, Event) error { return nil }) }()
	n, err := broker.RetryDead(testCtx)
	must(t, err)
	if n != 1 {
		t.Fatal(fmt.Sprintf("redrive count=%d", n))
	}
	waitUntil(t, 10*time.Second, func() bool { n, e := broker.DeadCount(testCtx); return e == nil && n == 0 })
	stop()
	<-done
}
func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}
