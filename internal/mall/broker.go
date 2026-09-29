package mall

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Broker interface {
	Publish(context.Context, Event) error
	Consume(context.Context, func(context.Context, Event) error) error
	Ping(context.Context) error
	DeadCount(context.Context) (int, error)
	RetryDead(context.Context) (int, error)
	Close() error
}

// LocalBroker 仅是单进程演示适配器；完整部署使用 RabbitMQ 的持久化队列。
type LocalBroker struct {
	queue chan Event
	mu    sync.Mutex
	dead  []Event
}

func NewLocalBroker() *LocalBroker { return &LocalBroker{queue: make(chan Event, 4096)} }
func (b *LocalBroker) Publish(ctx context.Context, e Event) error {
	select {
	case b.queue <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *LocalBroker) Consume(ctx context.Context, fn func(context.Context, Event) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-b.queue:
			for {
				err := fn(ctx, e)
				if err == nil {
					break
				}
				e.Attempts++
				if e.Attempts >= 5 {
					b.mu.Lock()
					b.dead = append(b.dead, e)
					b.mu.Unlock()
					slog.Error("demo message dead-lettered", "event", e.ID, "error", err)
					break
				} else {
					if !pause(ctx, 250*time.Millisecond) {
						return ctx.Err()
					}
				}
			}
		}
	}
}
func (b *LocalBroker) Ping(ctx context.Context) error { return ctx.Err() }
func (b *LocalBroker) DeadCount(context.Context) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.dead), nil
}
func (b *LocalBroker) RetryDead(ctx context.Context) (int, error) {
	b.mu.Lock()
	events := append([]Event(nil), b.dead...)
	b.dead = nil
	b.mu.Unlock()
	for i, e := range events {
		e.Attempts = 0
		if err := b.Publish(ctx, e); err != nil {
			b.mu.Lock()
			b.dead = append(b.dead, events[i:]...)
			b.mu.Unlock()
			return i, err
		}
	}
	return len(events), nil
}
func (b *LocalBroker) Close() error { return nil }

type Rabbit struct {
	url  string
	mu   sync.Mutex
	conn *amqp.Connection
}

func NewRabbit(url string) *Rabbit { return &Rabbit{url: url} }
func (b *Rabbit) connection() (*amqp.Connection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil && !b.conn.IsClosed() {
		return b.conn, nil
	}
	c, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: 10 * time.Second, Dial: func(network, addr string) (net.Conn, error) { return net.DialTimeout(network, addr, 3*time.Second) }})
	if err != nil {
		return nil, fmt.Errorf("rabbitmq connection unavailable")
	}
	ch, err := c.Channel()
	if err != nil {
		c.Close()
		return nil, err
	}
	defer ch.Close()
	for _, exchange := range []string{"pulse.events", "pulse.dead"} {
		if err = ch.ExchangeDeclare(exchange, "direct", true, false, false, false, nil); err != nil {
			c.Close()
			return nil, err
		}
	}
	if _, err = ch.QueueDeclare("pulse.flash", true, false, false, false, amqp.Table{"x-queue-type": "quorum", "x-delivery-limit": int32(5), "x-dead-letter-exchange": "pulse.dead", "x-dead-letter-routing-key": "flash", "x-overflow": "reject-publish", "x-dead-letter-strategy": "at-least-once"}); err != nil {
		c.Close()
		return nil, err
	}
	if _, err = ch.QueueDeclare("pulse.flash.dlq", true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		c.Close()
		return nil, err
	}
	if err = ch.QueueBind("pulse.flash", "flash", "pulse.events", false, nil); err != nil {
		c.Close()
		return nil, err
	}
	if err = ch.QueueBind("pulse.flash.dlq", "flash", "pulse.dead", false, nil); err != nil {
		c.Close()
		return nil, err
	}
	b.conn = c
	return c, nil
}
func (b *Rabbit) Publish(ctx context.Context, e Event) error {
	c, err := b.connection()
	if err != nil {
		return err
	}
	ch, err := c.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	// 持久化消息仍要等待发布确认，否则网络断开时无法知道 Broker 是否收到。
	if err = ch.Confirm(false); err != nil {
		return err
	}
	// mandatory=true 配合 Return 检查消息是否真的路由到队列。
	returned := ch.NotifyReturn(make(chan amqp.Return, 1))
	confirmation, err := ch.PublishWithDeferredConfirmWithContext(ctx, "pulse.events", "flash", true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: e.ID, Timestamp: time.Now(), Body: []byte(jsonText(e))})
	if err != nil {
		return err
	}
	if confirmation == nil {
		return fmt.Errorf("publisher confirms not enabled")
	}
	ok, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("broker rejected message")
	}
	select {
	case r := <-returned:
		return fmt.Errorf("message unroutable: %d", r.ReplyCode)
	default:
		return nil
	}
}
func (b *Rabbit) Consume(ctx context.Context, fn func(context.Context, Event) error) error {
	c, err := b.connection()
	if err != nil {
		return err
	}
	ch, err := c.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.Qos(8, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.ConsumeWithContext(ctx, "pulse.flash", "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("consumer channel closed")
			}
			var event Event
			if err = json.Unmarshal(delivery.Body, &event); err != nil {
				if err = delivery.Nack(false, false); err != nil {
					return err
				}
				continue
			}
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = fn(work, event)
			cancel()
			if err != nil {
				slog.Error("consumer retry", "event", event.ID, "error", err)
				if !pause(ctx, 300*time.Millisecond) {
					return ctx.Err()
				}
				// Reject 表示处理失败；RabbitMQ 4.3+ 的 Nack 重排不再计入 delivery-limit。
				if err = delivery.Reject(true); err != nil {
					return err
				}
			} else {
				// 业务事务提交成功后才 ACK；ACK 丢失导致重投时由 ticket 状态去重。
				if err = delivery.Ack(false); err != nil {
					return err
				}
			}
		}
	}
}
func (b *Rabbit) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := b.connection()
	return err
}
func (b *Rabbit) DeadCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c, err := b.connection()
	if err != nil {
		return 0, err
	}
	ch, err := c.Channel()
	if err != nil {
		return 0, err
	}
	defer ch.Close()
	q, err := ch.QueueInspect("pulse.flash.dlq")
	return q.Messages, err
}
func (b *Rabbit) RetryDead(ctx context.Context) (int, error) {
	c, err := b.connection()
	if err != nil {
		return 0, err
	}
	ch, err := c.Channel()
	if err != nil {
		return 0, err
	}
	defer ch.Close()
	count := 0
	for count < 100 {
		msg, ok, err := ch.Get("pulse.flash.dlq", false)
		if err != nil {
			return count, err
		}
		if !ok {
			break
		}
		var e Event
		if err = json.Unmarshal(msg.Body, &e); err != nil {
			_ = msg.Nack(false, true)
			return count, fmt.Errorf("死信格式错误，需要人工检查")
		}
		// 先确认重新发布，再 ACK 死信；中途失败最多重复投递，不会凭空丢消息。
		if err = b.Publish(ctx, e); err != nil {
			_ = msg.Nack(false, true)
			return count, err
		}
		if err = msg.Ack(false); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
func (b *Rabbit) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
