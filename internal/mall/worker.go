package mall

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	DB     *DB
	Cache  *Cache
	Broker Broker
	wg     sync.WaitGroup
}

// Start 启动 Outbox 转发、消息消费和定时恢复三类后台任务。
func (e *Engine) Start(ctx context.Context) {
	for i := 0; i < 4; i++ {
		e.wg.Add(1)
		go func() { defer e.wg.Done(); e.relay(ctx) }()
	}
	for i := 0; i < 4; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			for ctx.Err() == nil {
				if err := e.Broker.Consume(ctx, e.handle); err != nil && ctx.Err() == nil {
					slog.Error("consumer reconnecting", "error", err)
				}
				if !pause(ctx, time.Second) {
					return
				}
			}
		}()
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for ctx.Err() == nil {
			work, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := e.sweep(work)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Error("recovery sweep failed", "error", err)
			}
			if !pause(ctx, time.Second) {
				return
			}
		}
	}()
}
func (e *Engine) Wait() { e.wg.Wait() }
func (e *Engine) handle(ctx context.Context, event Event) error {
	switch event.Kind {
	case "flash.order":
		var p struct {
			TicketID string `json:"ticket_id"`
		}
		if err := json.Unmarshal([]byte(event.Payload), &p); err != nil {
			return err
		}
		if p.TicketID == "" {
			return fmt.Errorf("missing ticket_id")
		}
		return e.DB.FinalizeTicket(ctx, p.TicketID)
	case "flash.release":
		var r Release
		if err := json.Unmarshal([]byte(event.Payload), &r); err != nil {
			return err
		}
		return e.Cache.Release(ctx, r)
	default:
		return fmt.Errorf("unknown event kind: %s", event.Kind)
	}
}
// claimEvent 给事件加短租约；进程在发布途中退出后，租约到期即可由其他 worker 接手。
func (d *DB) claimEvent(ctx context.Context) (Event, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	lock := d.Lock()
	if lock != "" {
		// MySQL 跳过其他 worker 已锁住的事件，减少多实例之间的等待。
		lock += " SKIP LOCKED"
	}
	var e Event
	err = tx.QueryRowContext(ctx, "SELECT id,kind,payload,attempts FROM mall_outbox WHERE status<>'published' AND available_at<=? AND lease_until<=? ORDER BY created_at,id LIMIT 1"+lock, nowMS(), nowMS()).Scan(&e.ID, &e.Kind, &e.Payload, &e.Attempts)
	if err != nil {
		return e, err
	}
	e.Attempts++
	if _, err = tx.ExecContext(ctx, "UPDATE mall_outbox SET status='processing',lease_until=?,attempts=attempts+1 WHERE id=?", nowMS()+30000, e.ID); err != nil {
		return e, err
	}
	return e, tx.Commit()
}
func (d *DB) finishEvent(ctx context.Context, event Event, publishErr error) error {
	if publishErr == nil {
		_, err := d.SQL.ExecContext(ctx, "UPDATE mall_outbox SET status='published',lease_until=0,last_error='' WHERE id=?", event.ID)
		return err
	}
	delay := time.Second * time.Duration(min(60, 1<<min(event.Attempts, 6)))
	msg := publishErr.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	_, err := d.SQL.ExecContext(ctx, "UPDATE mall_outbox SET status='pending',available_at=?,lease_until=0,last_error=? WHERE id=?", nowMS()+delay.Milliseconds(), msg, event.ID)
	return err
}
// relay 只在 Broker 确认接收后标记已发布；确认丢失可能重发，消费端必须幂等。
func (e *Engine) relay(ctx context.Context) {
	for ctx.Err() == nil {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		event, err := e.DB.claimEvent(work)
		if err == nil {
			err = e.Broker.Publish(work, event)
			if err != nil {
				slog.Warn("outbox will retry", "event", event.ID, "error", err)
			}
			finishErr := e.DB.finishEvent(work, event, err)
			if finishErr != nil && ctx.Err() == nil {
				slog.Error("outbox finish failed", "event", event.ID, "error", finishErr)
			}
		}
		cancel()
		if err != nil {
			if err != sql.ErrNoRows && ctx.Err() == nil {
				slog.Warn("outbox relay delayed", "error", err)
			}
			if !pause(ctx, 250*time.Millisecond) {
				return
			}
		}
	}
}
// sweep 处理超时订单、久未完成的排队票据和 Redis 中过期的预扣租约。
func (e *Engine) sweep(ctx context.Context) error {
	if err := e.DB.ExpireOrders(ctx); err != nil {
		return err
	}
	if err := e.DB.RejectStaleTickets(ctx); err != nil {
		return err
	}
	activities, err := e.DB.Activities(ctx, false)
	if err != nil {
		return err
	}
	for _, a := range activities {
		holds, err := e.Cache.ExpiredHolds(ctx, a.ID)
		if err != nil {
			return err
		}
		for _, h := range holds {
			// 先在数据库写入拒绝终态，再清租约；迟到的 Submit 只能读到这条终态。
			if err = e.DB.FenceHold(ctx, h); err != nil {
				return err
			}
			if err = e.Cache.ForgetLease(ctx, a.ID, h.ID); err != nil {
				return err
			}
		}
	}
	return e.DB.ArchiveExpired(ctx, e.Cache)
}
// Submit 先在 Redis 原子预扣，再在 MySQL 保存 ticket 和 Outbox 事件。
// Redis 与 MySQL 无法共用事务，所以失败时保留租约供 sweep 查证和补偿。
func (e *Engine) Submit(ctx context.Context, user, activity, addressID int64, key string) (Ticket, error) {
	if user <= 0 || activity <= 0 || addressID <= 0 || !validKey(key) {
		return Ticket{}, bad("请选择地址并提供有效的活动和请求编号")
	}
	h := Hold{ID: ticketID(user, activity, key), UserID: user, ActivityID: activity, RequestKey: key, CreatedAt: nowMS()}
	epoch, replayed, err := e.Cache.Reserve(ctx, h)
	if err != nil {
		return Ticket{}, err
	}
	if replayed {
		t, lookupErr := e.DB.Ticket(ctx, user, h.ID)
		if lookupErr == nil {
			if t.Address.ID != 0 && t.Address.ID != addressID {
				return t, conflict("idempotency_conflict", "同一请求编号不能更换收货地址")
			}
			return t, nil
		}
		if lookupErr != notFound {
			return Ticket{}, lookupErr
		}
		if epoch == "" {
			return Ticket{}, conflict("stale_request", "原请求已失效，请关注下一场活动")
		}
	}
	t := Ticket{ID: h.ID, UserID: user, ActivityID: activity, Epoch: epoch, RequestKey: key}
	t, err = e.DB.AcceptTicket(ctx, t, addressID)
	if err != nil {
		return t, err
	} // 结果不确定时不直接返还；租约恢复先查数据库并写墓碑。
	if err = e.Cache.ForgetLease(ctx, activity, t.ID); err != nil {
		slog.Warn("lease cleanup deferred", "ticket", t.ID, "error", err)
	}
	return t, nil
}
func (d *DB) Tickets(ctx context.Context, user int64) ([]Ticket, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE user_id=? ORDER BY created_at DESC LIMIT 50", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}
func (d *DB) Outbox(ctx context.Context) ([]map[string]any, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,kind,status,attempts,last_error,created_at FROM mall_outbox WHERE status<>'published' ORDER BY created_at LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, kind, status, message string
		var attempts, created int64
		if err = rows.Scan(&id, &kind, &status, &attempts, &message, &created); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "kind": kind, "status": status, "attempts": attempts, "last_error": strings.TrimSpace(message), "created_at": created})
	}
	return items, rows.Err()
}
