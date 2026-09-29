package mall

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
)

const orderCols = "id,user_id,kind,status,total_cents,items_json,address_json,activity_id,ticket_id,created_at,expires_at,paid_at,tracking"

func scanOrder(row rowScanner) (o Order, err error) {
	var items, addr string
	err = row.Scan(&o.ID, &o.UserID, &o.Kind, &o.Status, &o.Total, &items, &addr, &o.ActivityID, &o.TicketID, &o.CreatedAt, &o.ExpiresAt, &o.PaidAt, &o.Tracking)
	if err != nil {
		return o, missing(err)
	}
	if err = json.Unmarshal([]byte(items), &o.Items); err != nil {
		return o, err
	}
	err = json.Unmarshal([]byte(addr), &o.Address)
	return
}
func (d *DB) Order(ctx context.Context, user int64, id string, admin bool) (Order, error) {
	q := "SELECT " + orderCols + " FROM mall_orders WHERE id=?"
	args := []any{id}
	if !admin {
		q += " AND user_id=?"
		args = append(args, user)
	}
	return scanOrder(d.SQL.QueryRowContext(ctx, q, args...))
}
func (d *DB) Orders(ctx context.Context, user int64, status string, page, size int, admin bool) (Page[Order], error) {
	result := Page[Order]{Items: []Order{}, Page: page, Size: size}
	where := " WHERE 1=1"
	args := []any{}
	if !admin {
		where += " AND user_id=?"
		args = append(args, user)
	}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}
	if err := d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_orders"+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := d.SQL.QueryContext(ctx, "SELECT "+orderCols+" FROM mall_orders"+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, o)
	}
	return result, rows.Err()
}
func insertOrder(ctx context.Context, tx *sql.Tx, o Order, key, fingerprint string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO mall_orders(id,user_id,request_key,fingerprint,kind,status,total_cents,items_json,address_json,activity_id,ticket_id,created_at,expires_at,paid_at,tracking) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", o.ID, o.UserID, key, fingerprint, o.Kind, o.Status, o.Total, jsonText(o.Items), jsonText(o.Address), o.ActivityID, o.TicketID, o.CreatedAt, o.ExpiresAt, o.PaidAt, o.Tracking)
	return err
}
// Checkout 在一个事务内完成幂等检查、扣库存和写订单；任一步失败都会回滚。
// 前端只提供商品编号与数量，成交价必须以数据库中的当前价格为准。
func (d *DB) Checkout(ctx context.Context, user, addressID int64, key string, lines []Line) (Order, error) {
	if !validKey(key) || addressID <= 0 || len(lines) < 1 || len(lines) > 30 {
		return Order{}, bad("请提供有效请求编号、地址和 1～30 种商品")
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].ProductID < lines[j].ProductID })
	for i, l := range lines {
		if l.ProductID <= 0 || l.Quantity < 1 || l.Quantity > 99 || (i > 0 && l.ProductID == lines[i-1].ProductID) {
			return Order{}, bad("商品不能重复，数量需为 1～99")
		}
	}
	// 同一请求编号只允许重试相同内容，防止网络超时后意外创建另一笔订单。
	fingerprint := digest(jsonText(struct {
		Address int64
		Lines   []Line
	}{addressID, lines}))
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback()
	// 同一用户的结算序列化，跨用户按商品 ID 升序加锁，降低多商品死锁概率。
	var uid int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM mall_users WHERE id=?"+d.Lock(), user).Scan(&uid); err != nil {
		return Order{}, err
	}
	var previousID, previousFingerprint string
	err = tx.QueryRowContext(ctx, "SELECT id,fingerprint FROM mall_orders WHERE user_id=? AND request_key=?", user, key).Scan(&previousID, &previousFingerprint)
	if err == nil {
		if previousFingerprint != fingerprint {
			return Order{}, conflict("idempotency_conflict", "该请求编号已用于不同的结算内容")
		}
		o, e := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderCols+" FROM mall_orders WHERE id=?", previousID))
		o.Replayed = true
		return o, e
	}
	if err != sql.ErrNoRows {
		return Order{}, err
	}
	a, err := address(ctx, tx, user, addressID)
	if err != nil {
		return Order{}, err
	}
	o := Order{ID: randomID(), UserID: user, Kind: "normal", Status: "pending", Address: a, CreatedAt: nowMS(), Items: []OrderItem{}}
	o.ExpiresAt = o.CreatedAt + d.OrderTTL.Milliseconds()
	for _, l := range lines {
		p, err := scanProduct(tx.QueryRowContext(ctx, "SELECT "+productCols+" FROM mall_products WHERE id=?"+d.Lock(), l.ProductID))
		if err != nil {
			return Order{}, err
		}
		if p.Status != "active" || p.Stock < l.Quantity {
			return Order{}, conflict("stock_shortage", p.Name+" 库存不足或已下架")
		}
		// SQL 中再次限定 stock>=quantity：即使前面的检查过期，也不能扣成负数。
		r, err := tx.ExecContext(ctx, "UPDATE mall_products SET stock=stock-? WHERE id=? AND stock>=?", l.Quantity, p.ID, l.Quantity)
		if err != nil {
			return Order{}, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return Order{}, err
		}
		if n != 1 {
			return Order{}, conflict("stock_shortage", "库存不足")
		}
		o.Items = append(o.Items, OrderItem{p.ID, p.Name, p.Image, p.Price, l.Quantity})
		o.Total += p.Price * l.Quantity
		// 不删除另一标签页后来额外加入的数量。
		if _, err = tx.ExecContext(ctx, "DELETE FROM mall_cart WHERE user_id=? AND product_id=? AND quantity<=?", user, p.ID, l.Quantity); err != nil {
			return Order{}, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE mall_cart SET quantity=quantity-? WHERE user_id=? AND product_id=?", l.Quantity, user, p.ID); err != nil {
			return Order{}, err
		}
	}
	if err = insertOrder(ctx, tx, o, key, fingerprint); err != nil {
		return Order{}, err
	}
	return o, tx.Commit()
}
// ChangeOrder 用数据库行锁串行处理支付、取消、超时和退款，避免重复返还库存。
func (d *DB) ChangeOrder(ctx context.Context, user int64, id, action, tracking string, admin bool) (Order, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback()
	o, err := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderCols+" FROM mall_orders WHERE id=?"+d.Lock(), id))
	if err != nil {
		return o, err
	}
	if !admin && o.UserID != user {
		return Order{}, notFound
	}
	before := o.Status
	restore := false
	switch action {
	case "pay":
		if before == "paid" || before == "shipped" || before == "completed" {
			o.Replayed = true
			return o, nil
		}
		if before != "pending" {
			return o, conflict("invalid_order_state", "此订单不能支付")
		}
		if nowMS() >= o.ExpiresAt {
			return o, conflict("payment_expired", "支付时间已结束，订单即将自动关闭")
		}
		o.Status = "paid"
		o.PaidAt = nowMS()
		if _, err = tx.ExecContext(ctx, "INSERT INTO mall_payments(order_id,amount_cents,provider,status,created_at) VALUES(?,?,'simulation','paid',?)", o.ID, o.Total, nowMS()); err != nil {
			return o, err
		}
	case "cancel":
		if before == "cancelled" {
			o.Replayed = true
			return o, nil
		}
		if before != "pending" {
			return o, conflict("invalid_order_state", "只能取消待支付订单")
		}
		o.Status = "cancelled"
		restore = true
	case "expire":
		if !admin {
			return o, notFound
		}
		if before != "pending" || nowMS() < o.ExpiresAt {
			return o, nil
		}
		o.Status = "expired"
		restore = true
	case "refund":
		if before == "refunded" {
			o.Replayed = true
			return o, nil
		}
		if before != "paid" {
			return o, conflict("invalid_order_state", "模拟退款仅支持已支付且未发货的订单")
		}
		o.Status = "refunded"
		restore = true
		if _, err = tx.ExecContext(ctx, "UPDATE mall_payments SET status='refunded' WHERE order_id=? AND status='paid'", o.ID); err != nil {
			return o, err
		}
	case "ship":
		if !admin {
			return o, notFound
		}
		if before == "shipped" && tracking == o.Tracking {
			o.Replayed = true
			return o, nil
		}
		if before != "paid" || len(tracking) < 5 || len(tracking) > 100 {
			return o, conflict("invalid_order_state", "仅可对已付款订单发货，请填写 5～100 字节物流单号")
		}
		o.Status = "shipped"
		o.Tracking = tracking
	case "receive":
		if before == "completed" {
			o.Replayed = true
			return o, nil
		}
		if before != "shipped" {
			return o, conflict("invalid_order_state", "订单尚未发货")
		}
		o.Status = "completed"
	default:
		return o, bad("不支持的订单操作")
	}
	if restore {
		if err = d.restoreStock(ctx, tx, o); err != nil {
			return o, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_orders SET status=?,paid_at=?,tracking=? WHERE id=?", o.Status, o.PaidAt, o.Tracking, o.ID); err != nil {
		return o, err
	}
	if err = audit(ctx, tx, user, "order."+action, o.ID); err != nil {
		return o, err
	}
	return o, tx.Commit()
}
func (d *DB) restoreStock(ctx context.Context, tx *sql.Tx, o Order) error {
	if o.Kind == "normal" {
		for _, item := range o.Items {
			if _, err := tx.ExecContext(ctx, "UPDATE mall_products SET stock=stock+? WHERE id=?", item.Quantity, item.ProductID); err != nil {
				return err
			}
		}
		return nil
	}
	a, err := scanActivity(tx.QueryRowContext(ctx, activitySelect+" WHERE a.id=?"+d.Lock(), o.ActivityID))
	if err != nil {
		return err
	}
	// 活动已归档时名额回到商品库存；仍在进行时回到活动库存。
	if a.Status == "archived" {
		if _, err = tx.ExecContext(ctx, "UPDATE mall_products SET stock=stock+1 WHERE id=?", a.ProductID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE mall_activities SET returned=returned+1 WHERE id=?", a.ID)
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mall_activities SET stock=stock+1 WHERE id=?", a.ID); err != nil {
		return err
	}
	// 数据库返库与释放事件同事务提交，随后由 Outbox 将 Redis 的预扣也归还。
	return addEvent(ctx, tx, "release:"+o.TicketID, "flash.release", Release{a.ID, o.TicketID, a.Epoch})
}
func (d *DB) ExpireOrders(ctx context.Context) error {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id FROM mall_orders WHERE status='pending' AND expires_at<=? ORDER BY expires_at LIMIT 100", nowMS())
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
		if _, err = d.ChangeOrder(ctx, 0, id, "expire", "", true); err != nil {
			return err
		}
	}
	return nil
}
func (d *DB) Stats(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	queries := map[string]string{"users": "SELECT COUNT(*) FROM mall_users WHERE role='customer'", "products": "SELECT COUNT(*) FROM mall_products", "orders": "SELECT COUNT(*) FROM mall_orders", "revenue_cents": "SELECT COALESCE(SUM(total_cents),0) FROM mall_orders WHERE status IN ('paid','shipped','completed')", "pending_orders": "SELECT COUNT(*) FROM mall_orders WHERE status='pending'", "queued_tickets": "SELECT COUNT(*) FROM mall_tickets WHERE status='queued'", "rejected_tickets": "SELECT COUNT(*) FROM mall_tickets WHERE status='rejected'", "outbox_pending": "SELECT COUNT(*) FROM mall_outbox WHERE status<>'published'", "flash_orders": "SELECT COUNT(*) FROM mall_orders WHERE kind='flash'", "outbox_retries": "SELECT COALESCE(SUM(attempts),0) FROM mall_outbox"}
	for k, q := range queries {
		var n int64
		if err := d.SQL.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, nil
}
func (d *DB) AuditLog(ctx context.Context) ([]map[string]any, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,actor_id,action,target,created_at FROM mall_audit ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, actor, at int64
		var action, target string
		if err = rows.Scan(&id, &actor, &action, &target, &at); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "actor_id": actor, "action": action, "target": target, "created_at": at})
	}
	return items, rows.Err()
}
func (d *DB) CheckInvariant(ctx context.Context, id int64) (map[string]int64, error) {
	var a Activity
	var committed int64
	// 单条 SQL 的一致性快照，避免并发返库恰好发生在两次查询之间产生假告警。
	if err := d.SQL.QueryRowContext(ctx, "SELECT initial_stock,stock,returned,(SELECT COUNT(*) FROM mall_orders WHERE activity_id=a.id AND status NOT IN ('cancelled','expired','refunded')) FROM mall_activities a WHERE id=?", id).Scan(&a.InitialStock, &a.Stock, &a.Returned, &committed); err != nil {
		return nil, err
	}
	return map[string]int64{"initial_stock": a.InitialStock, "remaining_stock": a.Stock, "active_orders": committed, "returned": a.Returned, "difference": a.InitialStock - a.Stock - committed - a.Returned}, nil
}
