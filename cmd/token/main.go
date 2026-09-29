package main

import (
	"flag"
	"fmt"
	"os"
	"seckill-lab/internal/auth"
	"time"
)

func main() {
	user := flag.Int64("user", 1, "用户 ID")
	ttl := flag.Duration("ttl", time.Hour, "令牌有效期")
	flag.Parse()
	secret := os.Getenv("AUTH_SECRET")
	if len(secret) < 32 || *user <= 0 || *ttl <= 0 {
		fmt.Fprintln(os.Stderr, "需要至少 32 字节的 AUTH_SECRET、正数 user/ttl")
		os.Exit(1)
	}
	fmt.Println(auth.New(secret).Mint(*user, time.Now().Add(*ttl)))
}
