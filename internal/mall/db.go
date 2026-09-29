package mall

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type DB struct {
	SQL      *sql.DB
	Dialect  string
	OrderTTL time.Duration
}

func OpenDB(ctx context.Context, dialect, dsn string) (*DB, error) {
	if dialect == "mysql" {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("MYSQL_DSN 格式错误")
		}
		cfg.ParseTime = true
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 10 * time.Second
		cfg.WriteTimeout = 10 * time.Second
		dsn = cfg.FormatDSN()
	}
	db, err := sql.Open(dialect, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(3 * time.Minute)
	if dialect == "sqlite" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{SQL: db, Dialect: dialect, OrderTTL: 15 * time.Minute}, nil
}
func (d *DB) Lock() string {
	if d.Dialect == "mysql" {
		return " FOR UPDATE"
	}
	return ""
}

func (d *DB) lockUser(ctx context.Context, tx *sql.Tx, id int64) error {
	var found int64
	return tx.QueryRowContext(ctx, "SELECT id FROM mall_users WHERE id=?"+d.Lock(), id).Scan(&found)
}
func (d *DB) Migrate(ctx context.Context) error {
	s := schema
	if d.Dialect == "mysql" {
		// 幂等键区分大小写，与 Go / Redis / SQLite 的语义一致。
		s = strings.ReplaceAll(s, "request_key VARCHAR(80)", "request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin")
	}
	if d.Dialect == "sqlite" {
		s = strings.ReplaceAll(s, "BIGINT PRIMARY KEY AUTO_INCREMENT", "INTEGER PRIMARY KEY AUTOINCREMENT")
		if _, err := d.SQL.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
			return err
		}
	}
	for _, q := range strings.Split(s, ";") {
		if strings.TrimSpace(q) == "" {
			continue
		}
		if _, err := d.SQL.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// 独立索引按元数据存在性创建，MySQL/SQLite 共用业务 schema。
	for _, ix := range []struct{ name, table, cols string }{{"ix_mall_orders_user", "mall_orders", "user_id,created_at"}, {"ix_mall_orders_expiry", "mall_orders", "status,expires_at"}, {"ix_mall_tickets_status", "mall_tickets", "status,created_at"}, {"ix_mall_outbox_due", "mall_outbox", "status,available_at,lease_until"}, {"ix_mall_products_category", "mall_products", "status,category"}, {"ix_mall_addresses_user", "mall_addresses", "user_id"}} {
		var count int
		var err error
		if d.Dialect == "mysql" {
			err = d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?", ix.table, ix.name).Scan(&count)
		} else {
			err = d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", ix.name).Scan(&count)
		}
		if err != nil {
			return err
		}
		if count == 0 {
			if _, err = d.SQL.ExecContext(ctx, "CREATE INDEX "+ix.name+" ON "+ix.table+" ("+ix.cols+")"); err != nil {
				return err
			}
		}
	}
	return nil
}
func duplicate(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notFound
	}
	return err
}

type rowScanner interface{ Scan(...any) error }
type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func audit(ctx context.Context, tx *sql.Tx, user int64, action, target string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO mall_audit(actor_id,action,target,created_at) VALUES(?,?,?,?)", user, action, target, nowMS())
	return err
}
func addEvent(ctx context.Context, tx *sql.Tx, id, kind string, payload any) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO mall_outbox(id,kind,payload,available_at,created_at) VALUES(?,?,?,?,?)", id, kind, jsonText(payload), nowMS(), nowMS())
	return err
}
