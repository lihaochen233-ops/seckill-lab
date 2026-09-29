package mall

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct{ Client *redis.Client }

func NewCache(addr, password string) *Cache {
	return &Cache{redis.NewClient(&redis.Options{Addr: addr, Password: password, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxRetries: -1, ContextTimeoutEnabled: true})}
}
// 同一活动的所有 key 使用相同哈希标签，Redis Cluster 才能在一个 Lua 脚本中操作它们。
func flashKeys(id int64) []string {
	prefix := fmt.Sprintf("pulse:flash:{%d}:", id)
	return []string{prefix + "state", prefix + "users", prefix + "leases", prefix + "holds", prefix + "released"}
}

// Lua 将库存门票、用户限购和可恢复租约一起写入，消除应用中途崩溃产生的未知预扣。
var reserveScript = redis.NewScript(`
-- 相同用户和相同 ticket 是重试；相同用户换 ticket 则违反一人一场一次。
local old=redis.call('HGET',KEYS[2],ARGV[1])
if old then
 if old==ARGV[2] then
  local hold=redis.call('HGET',KEYS[4],ARGV[2])
  if hold then return {2,cjson.decode(hold).epoch} end
  return {2,''}
 end
 return {3,old}
end
if redis.call('HGET',KEYS[1],'ready')~='1' or redis.call('HGET',KEYS[1],'enabled')~='1' then return {-4,''} end
local tm=redis.call('TIME'); local now=tonumber(tm[1])*1000+math.floor(tonumber(tm[2])/1000)
if now<tonumber(redis.call('HGET',KEYS[1],'starts')) then return {-2,''} end
if now>=tonumber(redis.call('HGET',KEYS[1],'ends')) then return {-3,''} end
if tonumber(redis.call('HGET',KEYS[1],'stock'))<=0 then return {0,''} end
local epoch=redis.call('HGET',KEYS[1],'epoch')
local hold=cjson.decode(ARGV[3]);hold.epoch=epoch;hold.created_at=now
-- 扣名额、记录参与者与 30 秒租约必须原子完成，恢复任务才能找到孤儿预扣。
redis.call('HINCRBY',KEYS[1],'stock',-1)
redis.call('HSET',KEYS[2],ARGV[1],ARGV[2])
redis.call('ZADD',KEYS[3],now+30000,ARGV[2])
redis.call('HSET',KEYS[4],ARGV[2],cjson.encode(hold))
return {1,epoch}
`)

// 只有同一代库存才能被旧事件修改。released 是永久去重标记，不能随意设置短 TTL。
var releaseScript = redis.NewScript(`
if redis.call('HGET',KEYS[1],'ready')~='1' then return -1 end
if redis.call('HGET',KEYS[1],'epoch')~=ARGV[2] then return 2 end
if redis.call('HSETNX',KEYS[5],ARGV[1],'1')==1 then redis.call('HINCRBY',KEYS[1],'stock',1) end
redis.call('ZREM',KEYS[3],ARGV[1]);redis.call('HDEL',KEYS[4],ARGV[1]);return 1
`)

type Hold struct {
	ID         string `json:"id"`
	UserID     int64  `json:"user_id"`
	ActivityID int64  `json:"activity_id"`
	RequestKey string `json:"request_key"`
	Epoch      string `json:"epoch"`
	CreatedAt  int64  `json:"created_at"`
}

// Reserve 只决定能否进入队列；最终能否生成订单仍由 MySQL 事务确认。
func (c *Cache) Reserve(ctx context.Context, h Hold) (string, bool, error) {
	v, err := reserveScript.Run(ctx, c.Client, flashKeys(h.ActivityID), h.UserID, h.ID, jsonText(h)).Slice()
	if err != nil {
		return "", false, err
	}
	n, ok := v[0].(int64)
	if !ok {
		return "", false, unavailable
	}
	extra, _ := v[1].(string)
	switch n {
	case 1:
		return extra, false, nil
	case 2:
		return extra, true, nil
	case 3:
		return "", false, conflict("already_joined", "本场活动每人仅可参与一次，请在订单或抢购记录中查看结果")
	case 0:
		return "", false, conflict("sold_out", "本轮名额已抢完，请关注释放的库存")
	case -2:
		return "", false, conflict("not_started", "活动尚未开始")
	case -3:
		return "", false, conflict("ended", "活动已结束")
	default:
		return "", false, &Fault{"flash_not_ready", "活动暂停或库存尚未预热", 503}
	}
}
func (c *Cache) Release(ctx context.Context, r Release) error {
	n, err := releaseScript.Run(ctx, c.Client, flashKeys(r.ActivityID), r.TicketID, r.Epoch).Int()
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("flash cache missing: %d", r.ActivityID)
	}
	return nil
}
func (c *Cache) ForgetLease(ctx context.Context, activity int64, ticket string) error {
	// Ticket 与 Outbox 已落库后才清理租约；清理失败可由恢复任务再次核对数据库。
	keys := flashKeys(activity)
	p := c.Client.TxPipeline()
	p.ZRem(ctx, keys[2], ticket)
	p.HDel(ctx, keys[3], ticket)
	_, err := p.Exec(ctx)
	return err
}
func (c *Cache) Pause(ctx context.Context, id int64) error {
	return c.Client.HSet(ctx, flashKeys(id)[0], "enabled", 0).Err()
}

// InitDisabled 必须在暂停准入、清理 queued 请求、持有数据库活动行锁时调用。
// 用户参与记录保留，重建后也不能绕过一人一次。
func (c *Cache) InitDisabled(ctx context.Context, a Activity) error {
	p := c.Client.TxPipeline()
	keys := flashKeys(a.ID)
	p.Del(ctx, keys[0], keys[2], keys[3], keys[4])
	p.HSet(ctx, keys[0], "ready", 1, "enabled", 0, "stock", a.Stock, "starts", a.StartsAt, "ends", a.EndsAt, "epoch", a.Epoch)
	_, err := p.Exec(ctx)
	return err
}
func (c *Cache) Enable(ctx context.Context, id int64) error {
	return c.Client.HSet(ctx, flashKeys(id)[0], "enabled", 1).Err()
}
func (c *Cache) ExpiredHolds(ctx context.Context, id int64) ([]Hold, error) {
	keys := flashKeys(id)
	ids, err := c.Client.ZRangeByScore(ctx, keys[2], &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(nowMS(), 10), Offset: 0, Count: 100}).Result()
	if err != nil {
		return nil, err
	}
	holds := []Hold{}
	for _, ticket := range ids {
		raw, err := c.Client.HGet(ctx, keys[3], ticket).Result()
		if err != nil {
			return nil, err
		}
		var h Hold
		if err = json.Unmarshal([]byte(raw), &h); err != nil {
			return nil, err
		}
		holds = append(holds, h)
	}
	return holds, nil
}
func (c *Cache) Decorate(ctx context.Context, a *Activity) {
	fields, err := c.Client.HMGet(ctx, flashKeys(a.ID)[0], "stock", "ready", "enabled").Result()
	if err != nil || len(fields) != 3 || fields[0] == nil {
		a.Available = 0
		a.Ready = false
		return
	}
	a.Available, _ = strconv.ParseInt(fmt.Sprint(fields[0]), 10, 64)
	a.Ready = fmt.Sprint(fields[1]) == "1" && fmt.Sprint(fields[2]) == "1"
}

// 计数和首次设置过期时间放进同一个脚本，避免 INCR 成功而 EXPIRE 未执行的永久限流键。
var rateScript = redis.NewScript(`local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[2]) end; if n>tonumber(ARGV[1]) then return 0 end; return 1`)

func (c *Cache) Allow(ctx context.Context, key string, max int, window time.Duration) (bool, error) {
	n, err := rateScript.Run(ctx, c.Client, []string{"pulse:rate:" + key}, max, window.Milliseconds()).Int()
	return n == 1, err
}
func (c *Cache) NewSession(ctx context.Context, u User) (string, Session, error) {
	token := randomID()
	s := Session{User: u, CSRF: randomID(), ExpiresAt: nowMS() + int64((12*time.Hour)/time.Millisecond)}
	err := c.Client.Set(ctx, "pulse:session:"+digest(token), jsonText(s), 12*time.Hour).Err()
	return token, s, err
}
func (c *Cache) Session(ctx context.Context, token string) (Session, error) {
	var s Session
	if len(token) != 48 {
		return s, &Fault{"unauthorized", "请先登录", 401}
	}
	raw, err := c.Client.Get(ctx, "pulse:session:"+digest(token)).Result()
	if err == redis.Nil {
		return s, &Fault{"unauthorized", "登录已过期，请重新登录", 401}
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(raw), &s)
	if err == nil && s.ExpiresAt <= nowMS() {
		err = &Fault{"unauthorized", "登录已过期，请重新登录", 401}
	}
	return s, err
}
func (c *Cache) Logout(ctx context.Context, token string) error {
	return c.Client.Del(ctx, "pulse:session:"+digest(token)).Err()
}
