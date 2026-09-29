package mall

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const activitySelect = "SELECT a.id,a.product_id,p.name,p.image,a.price_cents,p.price_cents,a.initial_stock,a.stock,a.returned,a.starts_at,a.ends_at,a.status,a.epoch FROM mall_activities a JOIN mall_products p ON p.id=a.product_id"

func scanActivity(row rowScanner) (a Activity, err error) {
	err = row.Scan(&a.ID, &a.ProductID, &a.Name, &a.Image, &a.Price, &a.OriginalPrice, &a.InitialStock, &a.Stock, &a.Returned, &a.StartsAt, &a.EndsAt, &a.Status, &a.Epoch)
	a.Available = a.Stock
	return a, missing(err)
}
func (d *DB) Activity(ctx context.Context, id int64) (Activity, error) {
	return scanActivity(d.SQL.QueryRowContext(ctx, activitySelect+" WHERE a.id=?", id))
}
func (d *DB) Activities(ctx context.Context, admin bool) ([]Activity, error) {
	q := activitySelect
	if !admin {
		q += " WHERE a.status<>'archived'"
	}
	q += " ORDER BY a.starts_at DESC,a.id DESC"
	rows, err := d.SQL.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Activity{}
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func (d *DB) CreateActivity(ctx context.Context, actor int64, a Activity) (Activity, error) {
	if a.ProductID <= 0 || a.Price <= 0 || a.Stock <= 0 || a.Stock > 100000 || a.EndsAt <= a.StartsAt || a.EndsAt <= nowMS() {
		return a, bad("活动商品、价格、库存或时间无效")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	p, err := scanProduct(tx.QueryRowContext(ctx, "SELECT "+productCols+" FROM mall_products WHERE id=?"+d.Lock(), a.ProductID))
	if err != nil {
		return a, err
	}
	if p.Status != "active" || p.Stock < a.Stock || a.Price > p.Price {
		return a, bad("活动库存不能超过普通库存，秒杀价不能高于商品价")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_products SET stock=stock-? WHERE id=?", a.Stock, p.ID); err != nil {
		return a, err
	}
	a.Epoch = randomID()
	a.InitialStock = a.Stock
	a.Status = "paused"
	a.Name = p.Name
	a.Image = p.Image
	a.OriginalPrice = p.Price
	r, err := tx.ExecContext(ctx, "INSERT INTO mall_activities(product_id,price_cents,initial_stock,stock,starts_at,ends_at,status,epoch) VALUES(?,?,?,?,?,?,?,?)", p.ID, a.Price, a.Stock, a.Stock, a.StartsAt, a.EndsAt, a.Status, a.Epoch)
	if err != nil {
		return a, err
	}
	a.ID, err = r.LastInsertId()
	if err != nil {
		return a, err
	}
	if err = audit(ctx, tx, actor, "activity.create", fmt.Sprint(a.ID)); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

const ticketCols = "id,user_id,activity_id,epoch,address_json,request_key,status,reason,order_id,created_at"

func scanTicket(row rowScanner) (t Ticket, err error) {
	var addr string
	err = row.Scan(&t.ID, &t.UserID, &t.ActivityID, &t.Epoch, &addr, &t.RequestKey, &t.Status, &t.Reason, &t.OrderID, &t.CreatedAt)
	if err != nil {
		return t, missing(err)
	}
	err = json.Unmarshal([]byte(addr), &t.Address)
	return
}
func (d *DB) Ticket(ctx context.Context, user int64, id string) (Ticket, error) {
	return scanTicket(d.SQL.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE id=? AND user_id=?", id, user))
}
func insertTicket(ctx context.Context, tx *sql.Tx, t Ticket) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO mall_tickets(id,user_id,activity_id,epoch,address_json,request_key,status,reason,order_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", t.ID, t.UserID, t.ActivityID, t.Epoch, jsonText(t.Address), t.RequestKey, t.Status, t.Reason, t.OrderID, t.CreatedAt)
	return err
}
func rejectTicket(ctx context.Context, tx *sql.Tx, t Ticket, reason string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE mall_tickets SET status='rejected',reason=? WHERE id=?", reason, t.ID); err != nil {
		return err
	}
	return addEvent(ctx, tx, "release:"+t.ID, "flash.release", Release{t.ActivityID, t.ID, t.Epoch})
}
// AcceptTicket 把秒杀准入结果和待发布事件写进同一个 MySQL 事务。
// 即使 RabbitMQ 暂时不可用，已受理的请求也能从 Outbox 恢复。
func (d *DB) AcceptTicket(ctx context.Context, t Ticket, addressID int64) (Ticket, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	// 活动锁同时为“暂停/重建”提供屏障：暂停提交后旧准入不能悄悄写入 queued。
	if err = d.lockUser(ctx, tx, t.UserID); err != nil {
		return t, err
	}
	a, err := scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.id=?"+d.Lock(), t.ActivityID))
	if err != nil {
		return t, err
	}
	previous, err := scanTicket(tx.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE user_id=? AND activity_id=?"+d.Lock(), t.UserID, t.ActivityID))
	if err == nil {
		if previous.ID != t.ID {
			return t, conflict("already_joined", "每人每场只能参与一次")
		}
		// 并发重试可能在 Submit 查询原请求前尚未提交，必须在行锁内再校验参数。
		if previous.Address.ID != 0 && previous.Address.ID != addressID {
			return t, conflict("idempotency_conflict", "同一请求编号不能更换收货地址")
		}
		return previous, nil
	}
	if err != notFound {
		return t, err
	}
	t.Status = "queued"
	t.CreatedAt = nowMS()
	t.Address, err = address(ctx, tx, t.UserID, addressID)
	if err != nil {
		if err != notFound {
			return t, err
		}
		t.Status = "rejected"
		t.Reason = "收货地址不存在"
	}
	if a.Status != "active" || a.Epoch != t.Epoch || t.CreatedAt < a.StartsAt || t.CreatedAt >= a.EndsAt {
		t.Status = "rejected"
		t.Reason = "活动已暂停、结束或准入已失效"
	}
	if err = insertTicket(ctx, tx, t); err != nil {
		return t, err
	}
	if t.Status == "queued" {
		// 排队票据和下单事件一起提交，避免“已排队但没有消费任务”。
		err = addEvent(ctx, tx, "order:"+t.ID, "flash.order", map[string]string{"ticket_id": t.ID})
	} else {
		err = addEvent(ctx, tx, "release:"+t.ID, "flash.release", Release{t.ActivityID, t.ID, t.Epoch})
	}
	if err != nil {
		return t, err
	}
	return t, tx.Commit()
}

// FinalizeTicket 是 RabbitMQ 消费端的业务入口：重复投递只读取同一终态。
func (d *DB) FinalizeTicket(ctx context.Context, id string) error {
	// 先读不可变的归属，再按 user → activity → ticket 加锁，与准入保持一致。
	owner, err := scanTicket(d.SQL.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE id=?", id))
	if err != nil {
		return err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = d.lockUser(ctx, tx, owner.UserID); err != nil {
		return err
	}
	a, err := scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.id=?"+d.Lock(), owner.ActivityID))
	if err != nil {
		return err
	}
	t, err := scanTicket(tx.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE id=?"+d.Lock(), id))
	if err != nil {
		return err
	}
	if t.Status != "queued" {
		// RabbitMQ 可能重复投递，已处理的票据不应再次扣库存或创建订单。
		return nil
	}
	if a.Stock <= 0 || a.Status == "archived" || a.Epoch != t.Epoch {
		if err = rejectTicket(ctx, tx, t, "活动名额已失效，未生成订单"); err != nil {
			return err
		}
		return tx.Commit()
	}
	// Redis 负责拦住大部分请求，MySQL 的条件扣减是最终的防超卖边界。
	r, err := tx.ExecContext(ctx, "UPDATE mall_activities SET stock=stock-1 WHERE id=? AND stock>0", a.ID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("activity stock invariant")
	}
	o := Order{ID: randomID(), UserID: t.UserID, Kind: "flash", Status: "pending", Total: a.Price, Address: t.Address, Items: []OrderItem{{a.ProductID, a.Name, a.Image, a.Price, 1}}, ActivityID: a.ID, TicketID: t.ID, CreatedAt: nowMS()}
	o.ExpiresAt = o.CreatedAt + d.OrderTTL.Milliseconds()
	if err = insertOrder(ctx, tx, o, "flash:"+t.ID, digest(t.ID)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_tickets SET status='ordered',order_id=? WHERE id=?", o.ID, t.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// FenceHold 在补偿前先写入数据库终态。迟到的 API 无法越过这块“墓碑”创建新任务。
func (d *DB) FenceHold(ctx context.Context, h Hold) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = d.lockUser(ctx, tx, h.UserID); err != nil {
		return err
	}
	var aid int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM mall_activities WHERE id=?"+d.Lock(), h.ActivityID).Scan(&aid); err != nil {
		return err
	}
	t, err := scanTicket(tx.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE user_id=? AND activity_id=?", h.UserID, h.ActivityID))
	if err == nil {
		return nil
	}
	if err != notFound {
		return err
	}
	t = Ticket{ID: h.ID, UserID: h.UserID, ActivityID: h.ActivityID, Epoch: h.Epoch, RequestKey: h.RequestKey, Status: "rejected", Reason: "请求未完成持久化，名额已回收", CreatedAt: h.CreatedAt}
	if err = insertTicket(ctx, tx, t); err != nil {
		return err
	}
	if err = addEvent(ctx, tx, "release:"+t.ID, "flash.release", Release{t.ActivityID, t.ID, t.Epoch}); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) RejectStaleTickets(ctx context.Context) error {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id FROM mall_tickets WHERE status='queued' AND created_at<? ORDER BY created_at LIMIT 100", nowMS()-120000)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = d.rejectOne(ctx, id, "排队超时，请关注下一场活动"); err != nil {
			return err
		}
	}
	return nil
}
func (d *DB) rejectOne(ctx context.Context, id, reason string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := scanTicket(tx.QueryRowContext(ctx, "SELECT "+ticketCols+" FROM mall_tickets WHERE id=?"+d.Lock(), id))
	if err != nil {
		return err
	}
	if t.Status != "queued" {
		return nil
	}
	if err = rejectTicket(ctx, tx, t, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// RebuildActivity 是显式管理操作，不会在缓存缺失时自动把库存补满。
func (d *DB) RebuildActivity(ctx context.Context, c *Cache, actor, id int64) error {
	if err := d.PauseActivity(ctx, c, actor, id); err != nil {
		return err
	}
	rows, err := d.SQL.QueryContext(ctx, "SELECT id FROM mall_tickets WHERE activity_id=? AND status='queued'", id)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var tid string
		if err = rows.Scan(&tid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, tid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, tid := range ids {
		if err = d.rejectOne(ctx, tid, "管理员恢复库存，未完成的排队请求已关闭"); err != nil {
			return err
		}
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.id=?"+d.Lock(), id))
	if err != nil {
		return err
	}
	if a.Status == "archived" || a.EndsAt <= nowMS() {
		return conflict("ended", "已结束的活动不能恢复准入")
	}
	// 新一代库存使用新 epoch；旧消息即使迟到，也不能增加新缓存中的库存。
	a.Epoch = randomID()
	a.Status = "active"
	if _, err = tx.ExecContext(ctx, "UPDATE mall_activities SET epoch=?,status='active' WHERE id=?", a.Epoch, id); err != nil {
		return err
	}
	// 持行锁取库存快照并写禁用的缓存。返库事务随后读到新 epoch，旧释放事件被忽略。
	if err = c.InitDisabled(ctx, a); err != nil {
		return err
	}
	users, err := tx.QueryContext(ctx, "SELECT user_id,id FROM mall_tickets WHERE activity_id=?", id)
	if err != nil {
		return err
	}
	pairs := []any{}
	for users.Next() {
		var user int64
		var tid string
		if err = users.Scan(&user, &tid); err != nil {
			users.Close()
			return err
		}
		pairs = append(pairs, user, tid)
	}
	err = users.Err()
	users.Close()
	if err != nil {
		return err
	}
	if len(pairs) > 0 {
		if err = c.Client.HSet(ctx, flashKeys(id)[1], pairs...).Err(); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, actor, "activity.rebuild", fmt.Sprint(id)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return c.Enable(ctx, id)
}
func (d *DB) PauseActivity(ctx context.Context, c *Cache, actor, id int64) error {
	if err := c.Pause(ctx, id); err != nil {
		return err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.id=?"+d.Lock(), id))
	if err != nil {
		return err
	}
	if a.Status == "archived" {
		return conflict("ended", "活动已归档")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_activities SET status='paused' WHERE id=?", id); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "activity.pause", fmt.Sprint(id)); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) ArchiveExpired(ctx context.Context, c *Cache) error {
	activities, err := d.Activities(ctx, false)
	if err != nil {
		return err
	}
	for _, a := range activities {
		if a.EndsAt+130000 > nowMS() {
			continue
		}
		if err = c.Pause(ctx, a.ID); err != nil {
			return err
		}
		if err = d.archiveOne(ctx, a.ID); err != nil {
			return err
		}
	}
	return nil
}
func (d *DB) archiveOne(ctx context.Context, id int64) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.id=?"+d.Lock(), id))
	if err != nil {
		return err
	}
	if a.Status == "archived" {
		return nil
	}
	var queued int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_tickets WHERE activity_id=? AND status='queued'", id).Scan(&queued); err != nil {
		return err
	}
	if queued > 0 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_products SET stock=stock+? WHERE id=?", a.Stock, a.ProductID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_activities SET status='archived',returned=returned+stock,stock=0 WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
