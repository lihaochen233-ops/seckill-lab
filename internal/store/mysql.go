package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"seckill-lab/internal/domain"
)

type MySQL struct{ db *sql.DB }

func NewMySQL(db *sql.DB) *MySQL { return &MySQL{db: db} }

func OpenMySQL(ctx context.Context, dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL DSN")
	}
	cfg.ParseTime, cfg.Loc = true, time.UTC
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(3 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

const productColumns = `id, name, price_cents, initial_stock, stock, starts_at, ends_at`
const orderColumns = `id, user_id, product_id, request_key, price_cents, created_at`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (p domain.Product, err error) {
	err = row.Scan(&p.ID, &p.Name, &p.PriceCents, &p.InitialStock, &p.Stock, &p.StartsAt, &p.EndsAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return
}
func scanOrder(row scanner) (o domain.Order, err error) {
	err = row.Scan(&o.ID, &o.UserID, &o.ProductID, &o.RequestKey, &o.PriceCents, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return
}
func (m *MySQL) Ping(ctx context.Context) error { return m.db.PingContext(ctx) }
func (m *MySQL) Product(ctx context.Context, id int64) (domain.Product, error) {
	return scanProduct(m.db.QueryRowContext(ctx, "SELECT "+productColumns+" FROM products WHERE id = ?", id))
}
func (m *MySQL) List(ctx context.Context) ([]domain.Product, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT "+productColumns+" FROM products ORDER BY id LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ps := make([]domain.Product, 0)
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}
func (m *MySQL) Order(ctx context.Context, user, id int64) (domain.Order, error) {
	return scanOrder(m.db.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM orders WHERE id = ? AND user_id = ?", id, user))
}
func (m *MySQL) byKey(ctx context.Context, in domain.BuyInput) (domain.Order, error) {
	return scanOrder(m.db.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM orders WHERE user_id = ? AND request_key = ?", in.UserID, in.RequestKey))
}
func replay(o domain.Order, in domain.BuyInput) (domain.BuyResult, error) {
	if o.ProductID != in.ProductID {
		return domain.BuyResult{}, domain.ErrKeyConflict
	}
	return domain.BuyResult{Order: o, Replayed: true}, nil
}

func (m *MySQL) Buy(ctx context.Context, in domain.BuyInput) (domain.BuyResult, error) {
	if o, err := m.byKey(ctx, in); err == nil {
		return replay(o, in)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.BuyResult{}, err
	}
	result, err := m.buyTx(ctx, in)
	// 两个不同商品也可能同时争用同一个请求编号。唯一约束是最终防线。
	// buyTx 返回前已经回滚，必须在事务外重新读取已提交订单。
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		o, lookupErr := m.byKey(ctx, in)
		if lookupErr == nil {
			return replay(o, in)
		}
		if !errors.Is(lookupErr, domain.ErrNotFound) {
			return domain.BuyResult{}, lookupErr
		}
		return domain.BuyResult{}, domain.ErrPurchased
	}
	return result, err
}

func (m *MySQL) buyTx(ctx context.Context, in domain.BuyInput) (domain.BuyResult, error) {
	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.BuyResult{}, err
	}
	defer tx.Rollback() // 任意失败路径回滚；Commit 成功后此调用无副作用。

	// 同一商品上的事务串行进入临界区；这个锁在 MySQL 内，跨进程也生效。
	p, err := scanProduct(tx.QueryRowContext(ctx, "SELECT "+productColumns+" FROM products WHERE id = ? FOR UPDATE", in.ProductID))
	if err != nil {
		return domain.BuyResult{}, err
	}
	o, err := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM orders WHERE user_id = ? AND request_key = ?", in.UserID, in.RequestKey))
	if err == nil {
		return replay(o, in)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.BuyResult{}, err
	}
	var existing int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM orders WHERE user_id = ? AND product_id = ?", in.UserID, in.ProductID).Scan(&existing)
	if err == nil {
		return domain.BuyResult{}, domain.ErrPurchased
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.BuyResult{}, err
	}
	// 使用数据库时钟，避免多个应用实例时钟偏差。锁等待结束后再取时间。
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return domain.BuyResult{}, err
	}
	if err = domain.CheckSale(p, now); err != nil {
		return domain.BuyResult{}, err
	}

	// 即使日后调整读取逻辑，也不能去掉 stock > 0 和 RowsAffected 检查。
	r, err := tx.ExecContext(ctx, "UPDATE products SET stock = stock - 1 WHERE id = ? AND stock > 0", p.ID)
	if err != nil {
		return domain.BuyResult{}, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return domain.BuyResult{}, err
	}
	if n != 1 {
		return domain.BuyResult{}, domain.ErrSoldOut
	}
	r, err = tx.ExecContext(ctx, "INSERT INTO orders (user_id, product_id, request_key, price_cents, created_at) VALUES (?, ?, ?, ?, ?)", in.UserID, p.ID, in.RequestKey, p.PriceCents, now)
	if err != nil {
		return domain.BuyResult{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return domain.BuyResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.BuyResult{}, err
	}
	return domain.BuyResult{Order: domain.Order{ID: id, UserID: in.UserID, ProductID: p.ID, RequestKey: in.RequestKey, PriceCents: p.PriceCents, CreatedAt: now}}, nil
}
