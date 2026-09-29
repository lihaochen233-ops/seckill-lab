# 部署与开发指南

## Docker Compose

在项目根目录使用 PowerShell 生成配置并启动：

```powershell
./scripts/setup.ps1
docker compose up --build -d
docker compose ps -a
docker compose logs --tail 80 api worker init
```

其他平台可复制 `.env.example` 为 `.env` 并替换所有密码占位符。建议使用随机十六进制密码，避免数据库 DSN 和 AMQP URL 的转义问题。

MySQL、Redis、RabbitMQ 健康后运行 `init`，创建数据库表与管理员。初始化成功后 API 和 Worker 启动；`init` 显示 `Exited (0)` 表示正常完成。管理员凭据来自 `.env`，商品与活动通过后台创建。重复初始化不会覆盖已有管理员、商品或库存。

访问 [商城首页](http://127.0.0.1:8088)。停止服务并保留数据：

```powershell
docker compose down
```

中间件使用命名数据卷持久化。`down -v` 会删除数据卷，仅在需要重置环境时使用。修改配置中的初始中间件密码不会自动更新已有数据卷内的账号密码。

服务编排使用 MySQL 8.4、Redis 7.4、RabbitMQ 4.2。应用采用非 root 用户、只读容器文件系统和独立 Worker；中间件不开放宿主机端口。

## 配置

| 配置项 | 用途 |
|---|---|
| MALL_MODE | 默认 stack；demo 用于本地预览 |
| MALL_ROLE | all、api、worker；默认 all |
| MALL_ADDR | 默认 127.0.0.1:8088；容器内 0.0.0.0:8088 |
| MYSQL_DSN | stack 模式 MySQL 主库连接串 |
| REDIS_ADDR / REDIS_PASSWORD | Redis 地址和密码 |
| AMQP_URL | RabbitMQ 连接 URL |
| APP_ORIGINS | 允许的精确 Origin，逗号分隔 |
| COOKIE_SECURE | stack 默认 true；本地 HTTP Compose 配置为 false |
| ADMIN_EMAIL / ADMIN_PASSWORD | 首次初始化管理员，密码至少 12 字节 |
| ORDER_TTL | 默认 15m，范围 1s～24h |
| DEMO_DB | 预览模式 SQLite 文件，默认 .local/mall.db |

Compose 自动读取根目录 `.env`。直接运行 Go 时需设置进程环境变量，应用不会自动加载 `.env`。

单独初始化：`go run ./cmd/mall -mode stack -init`。拆分运行：`-role api` 与 `-role worker`。实例连接同一 MySQL 主库、Redis 库和 RabbitMQ vhost。

## HTTPS 与运维

默认商城端口仅绑定 `127.0.0.1:8088`。对外服务时由可信反向代理终止 TLS，并设置 `COOKIE_SECURE=true` 和精确的 `APP_ORIGINS=https://商城域名`。

应用不会直接信任任意 `X-Forwarded-For`。反向代理后，登录 IP 限流会共享代理 IP，应在网关配置客户端限流，并按实际部署管理可信代理链。

备份数据库和配置，定期验证恢复流程；监控 `/api/health`、Outbox 积压、死信与库存核对差额。当前 Compose 为单节点中间件配置，多副本高可用、历史数据归档和集中告警需要结合部署环境配置。

## 本地开发与预览

需要 Go 1.25+ 和 Node.js 22.12+（推荐 24）。在项目根目录执行：

```powershell
cd frontend
npm ci
npm run build
cd ..
go run ./cmd/mall -mode demo
```

也可执行 `./scripts/start.ps1`。预览模式使用 SQLite、miniredis 和进程内队列，并创建示例账号、商品及活动。示例活动有效期为首次创建后的 48 小时，到期后可在后台新建。重启会使已有会话失效，并关闭未完成的排队请求。

端口占用时停止对应服务，或同时修改 `MALL_ADDR` 与 `APP_ORIGINS`，例如分别设为 `127.0.0.1:8090` 与 `http://127.0.0.1:8090`。预览模式仅允许监听回环 IP。

### VS Code 调试

安装 Go 扩展与 Delve，先在 `frontend` 执行 `npm ci`，选择“PULSE 商城：本地预览”后按 F5。配置先构建前端，再启动应用。

秒杀断点停留过久可能触发请求超时、30 秒预扣租约或 120 秒排队超时，需结合订单状态和库存核对判断。

### 前端热更新

终端 A，在项目根目录执行：

```powershell
$env:APP_ORIGINS='http://localhost:5173,http://127.0.0.1:5173,http://127.0.0.1:8088'
go run ./cmd/mall -mode demo
```

终端 B：

```powershell
cd frontend
npm run dev
```

访问 Vite 的 5173 地址，`/api` 代理到 8088。直接访问 Go 的 8088 端口时，修改前端后需要重新构建并重启 Go，静态资源在编译时嵌入二进制。

## 故障恢复

在独立测试环境暂停 Worker，提交秒杀请求后恢复：

```powershell
docker compose stop worker
docker compose start worker
```

恢复后继续消费；排队超过 120 秒的请求关闭并补偿。RabbitMQ 不可用时 Outbox 保留任务并重试；Redis 不可用时关闭秒杀准入。

缓存缺失时，通过后台“重建库存”按数据库余额恢复新代次。不要将 Redis 库存直接重置为初始值。恢复后核对订单数、数据库库存、差额与积压任务，恢复边界见 [架构文档](architecture.md)。
