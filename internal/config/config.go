package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
)

type Config struct {
	Addr, Mode, DSN, Secret, RedisAddr, RedisPassword string
	MaxInFlight, RateLimit                            int
	DemoAuth                                          bool
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func positive(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s 必须为正整数", key)
	}
	return n, nil
}
func Load() (c Config, err error) {
	c = Config{Addr: env("ADDR", "127.0.0.1:8080"), Mode: env("STORE_MODE", "memory"), DSN: os.Getenv("MYSQL_DSN"), Secret: os.Getenv("AUTH_SECRET"), RedisAddr: os.Getenv("REDIS_ADDR"), RedisPassword: os.Getenv("REDIS_PASSWORD")}
	if c.Mode != "memory" && c.Mode != "mysql" {
		return c, fmt.Errorf("STORE_MODE 只能是 memory 或 mysql")
	}
	c.DemoAuth = c.Mode == "memory"
	if c.DemoAuth {
		host, _, e := net.SplitHostPort(c.Addr)
		ip := net.ParseIP(host)
		if e != nil || ip == nil || !ip.IsLoopback() {
			return c, fmt.Errorf("memory 演示模式含令牌发放接口，ADDR 必须使用回环 IP，例如 127.0.0.1:8080")
		}
		if c.Secret == "" {
			b := make([]byte, 32)
			if _, err = rand.Read(b); err != nil {
				return c, err
			}
			c.Secret = hex.EncodeToString(b)
		}
	}
	if len(c.Secret) < 32 {
		return c, fmt.Errorf("AUTH_SECRET 至少 32 字节")
	}
	if c.Mode == "mysql" && c.DSN == "" {
		return c, fmt.Errorf("mysql 模式必须配置 MYSQL_DSN")
	}
	if c.MaxInFlight, err = positive("MAX_INFLIGHT", 64); err != nil {
		return c, err
	}
	c.RateLimit, err = positive("RATE_LIMIT", 200)
	return c, err
}
