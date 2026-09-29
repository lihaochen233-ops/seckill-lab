package limit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// 计数与过期一起执行，避免 INCR 成功、EXPIRE 失败留下永不过期的 key。
// 一秒固定窗口（从首次请求开始），窗口边界仍可能出现短时双倍突发。
var script = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], 1000) end
if n <= tonumber(ARGV[1]) then return 1 end
return 0
`)

type Redis struct {
	client *redis.Client
	max    int
}

func NewRedis(addr, password string, max int) *Redis {
	return &Redis{client: redis.NewClient(&redis.Options{Addr: addr, Password: password, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: -1, ContextTimeoutEnabled: true}), max: max}
}
func (r *Redis) Allow(ctx context.Context) (bool, error) {
	n, err := script.Run(ctx, r.client, []string{"seckill-lab:orders:rate:v1"}, r.max).Int()
	return n == 1, err
}
func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }
func (r *Redis) Close() error                   { return r.client.Close() }
