# 第三课：把正确性放进 MySQL 事务

阅读顺序：`migrations/001_init.sql` → `internal/store/mysql.go` 中 `Buy` → `buyTx`。

需要维护三个不变量：库存不负数；每用户每活动至多一单；成功订单数量 + 剩余库存 = 初始库存。前提是没有绕过服务直接篡改表、没有退款/补货，本项目确实采用这个边界。

## 1. 三种机制分别解决什么

| 机制 | 解决的问题 |
|---|---|
| 行锁 / 条件 UPDATE | 多个请求争抢有限库存 |
| 事务 | 扣库存成功、写订单失败时不能留下半成品 |
| UNIQUE 约束 | 并发场景最终拒绝重复请求和重复购买 |

仅 `BEGIN` 后普通 SELECT，并不自动解决两个事务读到相同库存的问题。本项目使用 `SELECT ... FOR UPDATE` 锁定商品行，再做条件更新。显式行锁便于按序检查规则、读取成交价格；条件更新又明确表达库存前提。代价是热商品上的写事务串行，吞吐有上限。

## 2. 一笔事务的执行顺序

```sql
BEGIN;
SELECT ... FROM products WHERE id = ? FOR UPDATE;
-- 检查同用户同 key 的订单，存在则复用。
-- 检查同用户同商品的订单，存在则限购拒绝。
SELECT UTC_TIMESTAMP(6);
-- 检查活动时间与库存。
UPDATE products SET stock = stock - 1 WHERE id = ? AND stock > 0;
INSERT INTO orders (...);
COMMIT;
```

所有 SQL 使用 `tx` 执行，不能中途用 `m.db.Exec` 替代，否则可能拿到另一条连接、跳出事务。参数通过 `?` 绑定，不拼接客户端传入的 SQL。

`BeginTx(..., sql.LevelReadCommitted)` 明确使用已提交读。事务等待商品行锁后再检查已有订单，能读取之前买家刚提交的结果；数据库时钟也在等锁结束后读取，避免等锁时间跨过活动结束点却继续用旧时间判断。

活动有效区间为 `[starts_at, ends_at)`。这里以获得锁后校验的时刻为准，不保证网络响应或事务提交一定发生在截止时间之前。

条件更新的 `RowsAffected()` 必须等于 1。0 行说明没有真正扣成功，不能继续创建订单。MySQL CHECK 是额外边界约束，不代替业务逻辑。

## 3. defer Rollback 与 Commit

```go
tx, err := m.db.BeginTx(...)
if err != nil { return ..., err }
defer tx.Rollback()
// 多个可能失败的操作
if err = tx.Commit(); err != nil { return ..., err }
```

`defer tx.Rollback()` 是兜底：任何中间错误返回都会撤销事务。成功提交后再次 Rollback 返回“事务已结束”，不会撤销已经提交的数据。

把 `INSERT` 故意变成失败后，先前的库存 UPDATE 也必须回滚。`TestMySQLRollbackOnInsertFailure` 会创建只属于测试库的触发器，让订单插入必然失败，再检查库存和订单数。

`Commit` 返回错误或 HTTP 响应丢失时，客户端可能不知道是否成功，不应换 key 再创建一个“新请求”。保留原 key 重试，由已提交订单判断结果。

## 4. 两个唯一约束不是一回事

```sql
UNIQUE (user_id, request_key)
UNIQUE (user_id, product_id)
```

第一条表示“一次请求的唯一身份”；第二条表示“每人每场只能购买一次”。同用户同 key 同商品复用原订单；同 key 换商品是 key 冲突；换 key 买同商品仍受限购约束。不同用户可使用相同 key。

请求 key 用 `ascii_bin` 比较大小写，和 Go map 中字符串行为一致。价格来自锁定的商品记录，不接受客户端报价。订单里的价格是成交时的快照。

## 5. 为什么事务内外都检查一次 key

事务外检查让常见重试可以直接返回。两个首次并发请求仍可能同时发现 key 不存在，所以拿到商品锁后必须再次检查。

当同一用户用同一 key 并发购买两个不同商品时，商品锁不同，两个事务都可能开始。最终由唯一约束裁决；失败事务先回滚，再到事务外查已提交订单，返回重放或冲突。

读不到商品、数据库断连、锁等待超时不是同一种错误。代码把已知业务错误映射为相应 HTTP 状态；其他存储错误返回 503，日志保留诊断信息，不把驱动错误直接交给客户端。

## 练习与资料

能回答这四句再进入下一阶段：为什么单靠事务不够？为什么不能先扣库存再独立写订单？为什么 Go mutex 不能保护两个服务器？为什么幂等查询要在售罄判断前？

原理参考：[Go 官方事务说明](https://go.dev/doc/database/execute-transactions)、[MySQL 官方隔离级别说明](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html)。注意数据库内部行为要结合具体隔离级别和 SQL 判断，不把“事务”简单等同于“所有操作自动串行”。
