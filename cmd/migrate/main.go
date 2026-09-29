package main

import (
	"context"
	"fmt"
	"os"
	"seckill-lab/internal/store"
	"seckill-lab/migrations"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return fmt.Errorf("请配置 MYSQL_DSN，且使用独立的 seckill_lab 数据库")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.OpenMySQL(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, name := range []string{"001_init.sql", "002_seed.sql"} {
		b, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		// 仅适用于本项目这两份不包含存储过程/字符串分号的固定 SQL 文件。
		for _, statement := range strings.Split(string(b), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		fmt.Println("applied", name)
	}
	return nil
}
