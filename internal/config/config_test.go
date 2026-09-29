package config

import (
	"strings"
	"testing"
)

func TestConfig(t *testing.T) {
	for _, key := range []string{"ADDR", "STORE_MODE", "MYSQL_DSN", "AUTH_SECRET", "MAX_INFLIGHT", "RATE_LIMIT", "REDIS_ADDR", "REDIS_PASSWORD"} {
		t.Setenv(key, "")
	}
	c, err := Load()
	if err != nil || !c.DemoAuth || len(c.Secret) < 32 {
		t.Fatal(c, err)
	}
	t.Setenv("ADDR", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("public demo accepted")
	}
	t.Setenv("STORE_MODE", "mysql")
	t.Setenv("AUTH_SECRET", strings.Repeat("x", 32))
	t.Setenv("MYSQL_DSN", "example")
	c, err = Load()
	if err != nil || c.DemoAuth {
		t.Fatal(c, err)
	}
	t.Setenv("MAX_INFLIGHT", "0")
	if _, err := Load(); err == nil {
		t.Fatal("invalid capacity accepted")
	}
}
