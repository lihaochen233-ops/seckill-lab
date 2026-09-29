此文档保留基础版设计；新版入口是 [商城 README](../README.md)。当前模块要求 Go 1.25+，下文历史运行版本信息仅供对照。

# 秒杀实验室 · Seckill Lab

适合学完 Go 基础后的第一个后端项目：做一个能运行、能解释、能验证的**防超卖秒杀下单系统**。

不是电商全站。本项目完成商品活动展示 → 身份校验 → 抢购 → 原子扣库存与创建订单 → 幂等重试 → 查询本人订单的闭环。每个商品代表一场活动，每个用户每场只能买一件；订单表示抢购成功，不表示已支付。

原目录 `../seckill` 保留为第一阶段错误实现对照，新代码全部位于本目录。为了看清 HTTP 与 SQL 的工作过程，新版使用 Go 标准库 `net/http`、`database/sql`，没有继续叠加 Gin/GORM。以后换框架不影响业务原理。

## 先运行，再读代码

需要 Go 1.24 或更新版本（本机验证版本为 1.26.5）。**内存模式不需要安装 MySQL、Redis 或 Docker。**

```powershell
cd E:\STUDY\项目\seckill-lab
go run ./cmd/server
```

打开 <http://127.0.0.1:8080>，点击“立即抢购 / 重试”：第一次创建订单，再次点击返回原订单。切换演示用户可以模拟不同买家。内存模式重启后数据清空，活动时间重新生成，只允许监听回环 IP。

另开一个终端，观察错误实现为什么会超卖：

```powershell
go run ./cmd/oversell
# 初始库存=1，剩余库存=0，订单数=2
```

然后运行正确性测试：

```powershell
go test ./...
go vet ./...
```

在**全新启动、没有手动购买过**的演示服务上：

```powershell
go run ./cmd/loadtest -n 500 -c 32 -repeat 2
```

预期 100 次创建、100 次原订单重放、800 次售罄，最终库存 0。压测工具还会查询成功订单的归属；任何库存不守恒、重复生成订单、429、503 或其他异常都会让完整实验失败。它不会自动重试、掩盖异常。重复实验请重新启动内存服务；持久化模式请使用新活动和新用户段。

## 有哪些可以讲清楚的特点

| 能力 | 实现与保证 | 重要程度 |
|---|---|---|
| 防止超卖 | MySQL 行锁、`stock > 0` 条件更新、事务内写订单 | 必须掌握 |
| 失败不丢库存 | 写订单失败会回滚库存扣减 | 必须掌握 |
| 每人一件 | `(user_id, product_id)` 唯一约束 | 必须掌握 |
| 幂等重试 | `(user_id, request_key)` 唯一约束，返回原订单 | 必须掌握 |
| 可信身份与归属 | 签名令牌取 user ID，查订单同时限制所属用户 | 必须掌握 |
| 并发实验 | 内存/MySQL 共用契约测试，双连接池测试，故障注入 | 简历亮点 |
| 流量保护 | 单实例有界并发，可选 Redis Lua 全局固定窗口限流 | 第二轮学习 |
| 工程配套 | Go 内嵌页面、优雅退出、连接池、超时、健康检查、指标、Docker/CI | 第二轮学习 |

Redis 不存库存。MySQL 是订单与库存的唯一权威存储，因此本版本不需要维护 Redis 库存与数据库之间的一致性。跨实例正确性依赖所有实例连接**同一个 MySQL 主库**；内存模式没有这个能力。

## 代码地图

```text
cmd/
  server/       组装配置、数据库、业务和 HTTP，启动与关闭
  migrate/      显式初始化表与演示数据
  token/        本地签发测试用户令牌
  loadtest/     并发正确性与 HTTP 延迟实验
  oversell/     可稳定复现超卖的错误对照
internal/
  domain/       商品、订单、输入输出、业务错误
  service/      业务入口与 Store 接口
  store/        内存实现、MySQL 事务实现及契约测试
  auth/         教学用 HMAC 签名令牌
  httpapi/      请求解析、响应、身份校验与并发保护
  limit/        Redis Lua 限流
  config/       环境变量读取与校验
migrations/     显式 SQL 建表、初始化数据
web/            嵌入二进制的单页演示
docs/           学习路线、逐阶段讲解、实验记录与简历建议
```

建议从 [学习路线](00-roadmap.md) 和 [第一课：跟着一次请求读代码](01-first-request.md) 开始。

## MySQL 持久化模式

使用独立数据库 `seckill_lab`，**不要复用旧 `seckill` 项目的表**。现有旧版 `products/orders` 表结构不兼容，新版迁移不会自动修改旧表。

如果已有 MySQL 8.0.16+，手动创建空库：

```sql
CREATE DATABASE seckill_lab CHARACTER SET utf8mb4;
```

配置连接（替换成自己的账号密码）：

```powershell
$env:STORE_MODE='mysql'
$env:MYSQL_DSN='你的用户:你的密码@tcp(127.0.0.1:3306)/seckill_lab'
$env:AUTH_SECRET=([guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N'))
go run ./cmd/migrate
go run ./cmd/server
```

另一个终端使用**相同** `AUTH_SECRET` 签发测试用户令牌：

```powershell
go run ./cmd/token -user 1 -ttl 1h
```

将输出粘贴到页面的“访问令牌”。真实模式不开放演示令牌接口。这个命令代表可信服务端签发测试身份；真实业务需要接入注册登录，客户端绝不能持有 `AUTH_SECRET`。

`migrate` 只创建缺失表并补缺失的演示商品，重复运行不重置库存，也不延长活动。MySQL 的活动初次创建后 24 小时结束；后续练习应创建新的活动记录，不要靠重置有订单的库存“开新场”。DDL 不保证整个文件事务回滚；这不是通用版本迁移框架。

Linux/macOS 将 `$env:NAME='value'` 换成 `export NAME='value'`，其他 Go 命令相同。

## Docker 与可选 Redis

本机没有 Docker，此路径提供配置和操作说明，尚未在本机实际运行。已有 Docker 时：

```powershell
# Compose 解析整个文件时也需要 AUTH_SECRET，先设置再执行。
$env:AUTH_SECRET=([guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N'))
docker compose up -d mysql redis
$env:STORE_MODE='mysql'
$env:MYSQL_DSN='seckill:local-seckill-only@tcp(127.0.0.1:13306)/seckill_lab'
$env:REDIS_ADDR='127.0.0.1:16379'
go run ./cmd/migrate
go run ./cmd/server
```

MySQL 初始化完成后再运行迁移；用 `docker compose ps` 看健康状态。也可直接 `docker compose --profile app up -d --build` 启动完整容器版。初始化 SQL 仅在 MySQL 数据卷首次创建时执行。Compose 中固定密码仅用于本地开发，发布部署时需更换并限制访问。

`REDIS_ADDR` 为空时关闭 Redis 限流；设置后 Redis 不可用则拒绝下单（503），避免故障时无控制地放行。`RATE_LIMIT` 默认每秒 200 次，是所有实例共享的一秒计数窗口，窗口边界可能短时放过双倍流量。正确性实验可先不配置 Redis；测试限流时预期看到 429，完整抢购实验会如实报告未通过。

## 配置速查

Go 进程只读取环境变量，**不会自动加载 `.env`**。`.env.example` 是说明模板，Docker Compose 可读取复制后的 `.env`。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `STORE_MODE` | `memory` | `mysql` 时持久化 |
| `ADDR` | `127.0.0.1:8080` | 演示模式只能回环监听 |
| `MYSQL_DSN` | 空 | MySQL 模式必填；日期按 UTC 解析 |
| `AUTH_SECRET` | 内存模式随机生成 | MySQL 模式必填，至少 32 字节 |
| `MAX_INFLIGHT` | `64` | 单实例同时执行下单的上限 |
| `REDIS_ADDR` | 空 | 为空关闭 Redis 限流 |
| `REDIS_PASSWORD` | 空 | Redis 认证密码 |
| `RATE_LIMIT` | `200` | Redis 每窗口允许下单尝试次数 |

## 接口

所有下单数量固定为 1，请求体只接受 `product_id`。金额单位为分。

| 方法与路径 | 身份 | 用途 |
|---|---|---|
| `GET /api/products` | 无 | 展示前 100 个活动商品 |
| `GET /api/products/{id}` | 无 | 查活动和当前库存 |
| `POST /api/orders` | Bearer 令牌 | 下单，必须提供 `Idempotency-Key` |
| `GET /api/orders/{id}` | Bearer 令牌 | 只返回本人订单 |
| `POST /api/demo/token` | 仅本地内存模式 | `{"user_id":1}` 换测试令牌 |
| `GET /api/config` | 无 | 页面判断是否展示演示登录 |
| `GET /healthz` | 无 | 进程存活 |
| `GET /readyz` | 无 | 数据库、启用的 Redis 是否可用 |
| `GET /metrics` | 无 | 本实例下单创建/重放/拒绝/故障计数 |

```powershell
$base='http://127.0.0.1:8080'
$token=(Invoke-RestMethod "$base/api/demo/token" -Method Post -ContentType 'application/json' -Body '{"user_id":1}').token
$headers=@{Authorization="Bearer $token"; 'Idempotency-Key'='learn-request-0001'}
Invoke-RestMethod "$base/api/orders" -Method Post -Headers $headers -ContentType 'application/json' -Body '{"product_id":1}'
```

首次成功为 201；同 key 同商品重放为 200；同 key 不同商品、限购、未开始/结束/售罄为 409；非法参数 400；认证失败 401；不存在/他人订单 404；限流 429；存储或 Redis 异常 503。错误响应统一为 `{"code":"...","message":"..."}`。key 必须为 8～64 位字母、数字、下划线或连字符。

key 按用户隔离；只有成功订单占用 key，校验失败/售罄不会永久缓存响应。503 或断网后使用**原 key 原商品**重试，不能直接认定订单失败。幂等重放也经过流量保护，所以过载时仍可能收到 429。

## 深入测试与实际记录

真实数据库集成测试需能创建测试库的专用开发实例账号。测试仅创建、清理 `seckill_lab_test_时间戳_序号` 库；不会清理传入 DSN 指定的业务库。

```powershell
$env:TEST_MYSQL_DSN='测试账号:测试密码@tcp(127.0.0.1:3306)/'
go test -count=1 -v ./internal/store
```

未设置 `TEST_MYSQL_DSN` 时 MySQL 测试会明确跳过，不等于已验证数据库。`go test -race ./...` 需要平台对应 C 工具链；当前 Windows 环境没有 gcc，所以本地未完成 race 检查，CI 配置会在 Linux 执行，但 CI 尚未实际触发。

参见 [实际验证记录](validation.md)、[事务讲解](03-mysql.md)、[简历与面试](06-resume.md)。

## 当前边界

这是同步秒杀下单服务，单个热商品行会成为吞吐瓶颈。没有支付/退款、订单过期释放库存、后台管理、MQ 异步下单、分库分表或线上运维验证。没有宣称百万 QPS。内存实验结果不能代替数据库容量；短时间 HTTP 请求速率也不等于成功订单吞吐。

HMAC 令牌仅演示身份签名，没有账户管理、令牌吊销、密钥轮换。商品列表没有分页管理能力；指标是进程内计数，重启归零。外网部署还需要 HTTPS、指标端点访问控制、账户系统等配套。扩展任务见 [下一步实验](07-next-steps.md)。
