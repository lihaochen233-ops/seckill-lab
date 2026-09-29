package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"seckill-lab/internal/domain"
	"seckill-lab/internal/service"
	"seckill-lab/migrations"
)

type fixture struct {
	repo  service.Store
	audit func() (int64, int64)
	db    *sql.DB
}
type factory func(*testing.T, []domain.Product) fixture

func TestMemoryContract(t *testing.T) {
	contract(t, func(t *testing.T, ps []domain.Product) fixture {
		m := NewMemory(ps)
		return fixture{repo: m, audit: func() (int64, int64) {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.products[1].Stock, int64(len(m.orders))
		}}
	})
}
func TestMySQLContract(t *testing.T) {
	if os.Getenv("TEST_MYSQL_DSN") == "" {
		t.Skip("设置 TEST_MYSQL_DSN 运行真实 MySQL 测试；测试创建并清理自己命名的独立库")
	}
	contract(t, mysqlFixture)
}
func products(stock int64) []domain.Product {
	ps := domain.DemoProducts(time.Now().UTC())
	ps[0].Stock = stock
	ps[0].InitialStock = stock
	return ps
}
func contract(t *testing.T, newFixture factory) {
	t.Run("200 buyers compete for 20 items", func(t *testing.T) {
		f := newFixture(t, products(20))
		s := service.New(f.repo)
		var created atomic.Int64
		parallel(200, func(i int) {
			_, err := s.Buy(context.Background(), domain.BuyInput{UserID: int64(i + 1), ProductID: 1, RequestKey: fmt.Sprintf("request-%08d", i)})
			if err == nil {
				created.Add(1)
			} else if !errors.Is(err, domain.ErrSoldOut) {
				t.Errorf("unexpected: %v", err)
			}
		})
		stock, orders := f.audit()
		if created.Load() != 20 || stock != 0 || orders != 20 {
			t.Fatalf("created=%d stock=%d orders=%d", created.Load(), stock, orders)
		}
	})
	t.Run("same request replays even after sold out", func(t *testing.T) {
		f := newFixture(t, products(1))
		var created atomic.Int64
		var id atomic.Int64
		parallel(40, func(i int) {
			r, err := f.repo.Buy(context.Background(), domain.BuyInput{UserID: 1, ProductID: 1, RequestKey: "same-request"})
			if err != nil {
				t.Error(err)
				return
			}
			if !r.Replayed {
				created.Add(1)
			}
			id.CompareAndSwap(0, r.Order.ID)
			if id.Load() != r.Order.ID {
				t.Error("different order IDs")
			}
		})
		stock, orders := f.audit()
		if created.Load() != 1 || orders != 1 || stock != 0 {
			t.Fatalf("created=%d stock=%d orders=%d", created.Load(), stock, orders)
		}
	})
	t.Run("different keys still cannot bypass one per user", func(t *testing.T) {
		f := newFixture(t, products(20))
		var created atomic.Int64
		parallel(40, func(i int) {
			_, err := f.repo.Buy(context.Background(), domain.BuyInput{UserID: 1, ProductID: 1, RequestKey: fmt.Sprintf("request-%08d", i)})
			if err == nil {
				created.Add(1)
			} else if !errors.Is(err, domain.ErrPurchased) {
				t.Error(err)
			}
		})
		stock, orders := f.audit()
		if created.Load() != 1 || orders != 1 || stock != 19 {
			t.Fatalf("created=%d stock=%d orders=%d", created.Load(), stock, orders)
		}
	})
	t.Run("conflicting key and private order", func(t *testing.T) {
		f := newFixture(t, products(20))
		ctx := context.Background()
		r, err := f.repo.Buy(ctx, domain.BuyInput{UserID: 1, ProductID: 1, RequestKey: "request-0001"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.repo.Buy(ctx, domain.BuyInput{UserID: 1, ProductID: 2, RequestKey: "request-0001"})
		if !errors.Is(err, domain.ErrKeyConflict) {
			t.Fatalf("got %v", err)
		}
		p, _ := f.repo.Product(ctx, 2)
		if p.Stock != 25 {
			t.Fatal("conflict consumed stock")
		}
		_, err = f.repo.Order(ctx, 2, r.Order.ID)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("order privacy broken")
		}
		_, err = f.repo.Buy(ctx, domain.BuyInput{UserID: 1, ProductID: 999, RequestKey: "request-0002"})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("invalid or canceled request does not consume stock", func(t *testing.T) {
		f := newFixture(t, products(20))
		s := service.New(f.repo)
		for _, in := range []domain.BuyInput{{UserID: 0, ProductID: 1, RequestKey: "valid-key"}, {UserID: 1, ProductID: -1, RequestKey: "valid-key"}, {UserID: 1, ProductID: 1, RequestKey: "bad"}} {
			if _, err := s.Buy(context.Background(), in); !errors.Is(err, domain.ErrInvalid) {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.Buy(ctx, domain.BuyInput{UserID: 1, ProductID: 1, RequestKey: "valid-key"}); err == nil {
			t.Fatal("canceled request succeeded")
		}
		stock, orders := f.audit()
		if stock != 20 || orders != 0 {
			t.Fatalf("stock=%d orders=%d", stock, orders)
		}
	})
	for _, tc := range []struct {
		name       string
		start, end time.Duration
		want       error
	}{{"future", time.Hour, 2 * time.Hour, domain.ErrNotStarted}, {"past", -2 * time.Hour, -time.Hour, domain.ErrEnded}} {
		t.Run(tc.name, func(t *testing.T) {
			ps := products(1)
			ps[0].StartsAt = time.Now().Add(tc.start)
			ps[0].EndsAt = time.Now().Add(tc.end)
			f := newFixture(t, ps)
			_, err := f.repo.Buy(context.Background(), domain.BuyInput{UserID: 1, ProductID: 1, RequestKey: "valid-key"})
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			stock, orders := f.audit()
			if stock != 1 || orders != 0 {
				t.Fatal("inactive sale consumed stock")
			}
		})
	}
}
func parallel(n int, fn func(int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; fn(i) }(i)
	}
	close(start)
	wg.Wait()
}

var dbCounter atomic.Int64

func mysqlFixture(t *testing.T, ps []domain.Product) fixture {
	t.Helper()
	raw := os.Getenv("TEST_MYSQL_DSN")
	if raw == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := OpenMySQL(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("seckill_lab_test_%d_%d", time.Now().UnixNano(), dbCounter.Add(1))
	if _, err = admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	cfg.DBName = name
	db, err := OpenMySQL(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := migrations.Files.ReadFile("001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(b), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, p := range ps {
		if _, err := db.Exec("INSERT INTO products (id,name,price_cents,initial_stock,stock,starts_at,ends_at) VALUES (?,?,?,?,?,?,?)", p.ID, p.Name, p.PriceCents, p.InitialStock, p.Stock, p.StartsAt, p.EndsAt); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{db: db, repo: NewMySQL(db), audit: func() (int64, int64) {
		var stock, orders int64
		if err := db.QueryRow("SELECT stock FROM products WHERE id=1").Scan(&stock); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orders); err != nil {
			t.Fatal(err)
		}
		return stock, orders
	}}
}

func TestMySQLRollbackOnInsertFailure(t *testing.T) {
	f := mysqlFixture(t, products(2))
	_, err := f.db.Exec("CREATE TRIGGER fail_order BEFORE INSERT ON orders FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected insert failure'")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.repo.Buy(context.Background(), domain.BuyInput{UserID: 1, ProductID: 1, RequestKey: "rollback-test"})
	if err == nil {
		t.Fatal("expected insert failure")
	}
	stock, orders := f.audit()
	if stock != 2 || orders != 0 {
		t.Fatalf("rollback failed: stock=%d orders=%d", stock, orders)
	}
}

func TestMySQLTwoPools(t *testing.T) {
	f := mysqlFixture(t, products(10))
	var name string
	if err := f.db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	db, err := OpenMySQL(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	other := NewMySQL(db)
	parallel(50, func(i int) {
		repo := f.repo
		if i%2 == 0 {
			repo = other
		}
		_, err := repo.Buy(context.Background(), domain.BuyInput{UserID: int64(i + 1), ProductID: 1, RequestKey: fmt.Sprintf("two-pools-%d", i)})
		if err != nil && !errors.Is(err, domain.ErrSoldOut) {
			t.Error(err)
		}
	})
	stock, orders := f.audit()
	if stock != 0 || orders != 10 {
		t.Fatalf("stock=%d orders=%d", stock, orders)
	}
}

func TestMySQLConcurrentKeyConflict(t *testing.T) {
	f := mysqlFixture(t, products(1))
	var success, conflict atomic.Int64
	parallel(2, func(i int) {
		_, err := f.repo.Buy(context.Background(), domain.BuyInput{UserID: 1, ProductID: int64(i + 1), RequestKey: "shared-request"})
		if err == nil {
			success.Add(1)
		} else if errors.Is(err, domain.ErrKeyConflict) {
			conflict.Add(1)
		} else {
			t.Error(err)
		}
	})
	p1, _ := f.repo.Product(context.Background(), 1)
	p2, _ := f.repo.Product(context.Background(), 2)
	if success.Load() != 1 || conflict.Load() != 1 || p1.Stock+p2.Stock != 25 {
		t.Fatalf("success=%d conflict=%d remaining=%d", success.Load(), conflict.Load(), p1.Stock+p2.Stock)
	}
}
