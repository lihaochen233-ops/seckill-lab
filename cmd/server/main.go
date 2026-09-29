package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"seckill-lab/internal/auth"
	"seckill-lab/internal/config"
	"seckill-lab/internal/domain"
	"seckill-lab/internal/httpapi"
	"seckill-lab/internal/limit"
	"seckill-lab/internal/service"
	"seckill-lab/internal/store"
	"seckill-lab/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var repo service.Store
	if cfg.Mode == "memory" {
		repo = store.NewMemory(domain.DemoProducts(time.Now()))
	} else {
		db, err := store.OpenMySQL(ctx, cfg.DSN)
		if err != nil {
			return err
		}
		defer db.Close()
		repo = store.NewMySQL(db)
	}
	var limiter httpapi.Limiter
	if cfg.RedisAddr != "" {
		r := limit.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RateLimit)
		defer r.Close()
		if err := r.Ping(ctx); err != nil {
			return fmt.Errorf("redis unavailable: %w", err)
		}
		limiter = r
	}
	h := httpapi.New(service.New(repo), auth.New(cfg.Secret), limiter, cfg.MaxInFlight, cfg.DemoAuth, web.Handler())
	srv := &http.Server{Addr: cfg.Addr, Handler: h, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stop, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	slog.Info("server starting", "address", cfg.Addr, "mode", cfg.Mode, "redis", cfg.RedisAddr != "", "demo_auth", cfg.DemoAuth)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stop.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
			return err
		}
		return nil
	}
}
