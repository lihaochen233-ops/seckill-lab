package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alicebob/miniredis/v2"
	"seckill-lab/internal/mall"
	mallui "seckill-lab/web/mall"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("mall stopped", "error", err)
		os.Exit(1)
	}
}
// run 负责装配依赖：demo 使用本地 SQLite/模拟 Redis/内存队列；stack 连接完整中间件。
func run() error {
	mode := flag.String("mode", env("MALL_MODE", "demo"), "demo 或 stack")
	role := flag.String("role", env("MALL_ROLE", "all"), "all、api 或 worker")
	initOnly := flag.Bool("init", false, "初始化商城表和种子数据后退出")
	flag.Parse()
	if *mode != "demo" && *mode != "stack" {
		return fmt.Errorf("mode 必须为 demo 或 stack")
	}
	if *role != "all" && *role != "api" && *role != "worker" {
		return fmt.Errorf("role 必须为 all/api/worker")
	}
	demo := *mode == "demo"
	addr := env("MALL_ADDR", "127.0.0.1:8088")
	origins := strings.Split(env("APP_ORIGINS", "http://127.0.0.1:8088,http://localhost:8088"), ",")
	dialect, dsn := "mysql", os.Getenv("MYSQL_DSN")
	redisAddr := os.Getenv("REDIS_ADDR")
	rabbitURL := os.Getenv("AMQP_URL")
	adminEmail, adminPassword := os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD")
	secure := env("COOKIE_SECURE", "true") == "true"
	if demo {
		if *role != "all" {
			return fmt.Errorf("demo 必须使用 all 角色")
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return fmt.Errorf("demo 只能监听回环 IP")
		}
		secure = false
		adminEmail = "admin@pulse.local"
		adminPassword = "PulseAdmin2026!"
		if err := os.MkdirAll(".local", 0700); err != nil {
			return err
		}
		file := env("DEMO_DB", filepath.Join(".local", "mall.db"))
		dsn = "file:" + filepath.ToSlash(file) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
		dialect = "sqlite"
		mr := miniredis.NewMiniRedis()
		if err := mr.Start(); err != nil {
			return err
		}
		defer mr.Close()
		clockDone := make(chan struct{})
		defer close(clockDone)
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			last := time.Now()
			for {
				select {
				case now := <-ticker.C:
					mr.FastForward(now.Sub(last))
					last = now
				case <-clockDone:
					return
				}
			}
		}()
		redisAddr = mr.Addr()
	} else if dsn == "" || redisAddr == "" || rabbitURL == "" {
		return fmt.Errorf("stack 需要 MYSQL_DSN、REDIS_ADDR、AMQP_URL")
	}
	boot, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := mall.OpenDB(boot, dialect, dsn)
	if err != nil {
		return err
	}
	defer db.SQL.Close()
	if ttl := os.Getenv("ORDER_TTL"); ttl != "" {
		d, err := time.ParseDuration(ttl)
		if err != nil || d < time.Second || d > 24*time.Hour {
			return fmt.Errorf("ORDER_TTL 必须介于 1s 与 24h")
		}
		db.OrderTTL = d
	}
	cache := mall.NewCache(redisAddr, os.Getenv("REDIS_PASSWORD"))
	defer cache.Client.Close()
	if err = cache.Client.Ping(boot).Err(); err != nil {
		return err
	}
	if *initOnly || demo {
		// Compose 的 init 容器先迁移和造初始数据，再让 API 与 worker 启动。
		if err = db.Migrate(boot); err != nil {
			return err
		}
		seeded, err := db.Seed(boot, adminEmail, adminPassword, demo)
		if err != nil {
			return err
		}
		if demo {
			activities, err := db.Activities(boot, false)
			if err != nil {
				return err
			}
			for _, a := range activities {
				should := a.Status == "active"
				for _, id := range seeded {
					if id == a.ID {
						should = true
					}
				}
				if should && a.EndsAt > time.Now().UnixMilli() {
					if err = db.RebuildActivity(boot, cache, 0, a.ID); err != nil {
						return err
					}
				}
			}
		} else {
			for _, id := range seeded {
				if err = db.RebuildActivity(boot, cache, 0, id); err != nil {
					return err
				}
			}
		}
		if *initOnly {
			slog.Info("mall initialization complete")
			return nil
		}
	}
	var broker mall.Broker
	if demo {
		broker = mall.NewLocalBroker()
	} else {
		broker = mall.NewRabbit(rabbitURL)
	}
	defer broker.Close()
	if err = broker.Ping(boot); err != nil {
		return err
	}
	engine := &mall.Engine{DB: db, Cache: cache, Broker: broker}
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workers, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	if *role != "api" {
		// stack 将 API 与 worker 分成独立容器；demo 则由一个进程同时运行两者。
		engine.Start(workers)
		defer engine.Wait()
	}
	if *role == "worker" {
		slog.Info("mall worker running")
		<-signals.Done()
		stopWorkers()
		return nil
	}
	srv := mall.NewAPI(engine, origins, secure, demo, mallui.Handler())
	srv.Addr = addr
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	slog.Info("PULSE mall starting", "address", addr, "mode", *mode, "role", *role, "payment", "simulation")
	select {
	case err = <-done:
		stopWorkers()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err = srv.Shutdown(shutdown)
		if err != nil {
			_ = srv.Close()
		}
		stopWorkers()
		return err
	}
}
