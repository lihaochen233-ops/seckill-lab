# 运行与 VS Code 调试

## 本地运行

VS Code 打开 `E:\STUDY\项目\seckill-lab`。按 README 运行 `npm ci → npm run build → go run ./cmd/mall -mode demo`，访问 http://127.0.0.1:8088。Go 同时提供页面与 API，无需 CORS 配置。

也可以执行 `./scripts/start-mall.ps1`；PowerShell 禁止运行脚本时，用 README 逐条命令即可，不必修改全局执行策略。首次 Go/npm 下载失败时先处理网络或代理，不要删除业务代码。

8088 被占用时先停止你启动的另一个商城实例。更换端口需同时修改 `MALL_ADDR` 和 `APP_ORIGINS`，例如 `127.0.0.1:8090` 与 `http://127.0.0.1:8090`。

如果是本次开发验收留下的后台预览，先运行 `./scripts/stop-preview.ps1` 再自行启动。脚本核对进程路径与启动时间，只停止记录中的本项目预览进程。

## F5 断点

安装 VS Code 官方 Go 扩展和它提示的 Delve，先在 `frontend` 执行 `npm ci`。选择调试配置“PULSE 商城：本地演示”，按 F5。配置先构建前端，再运行 `cmd/mall`。

建议断点依次设置在 `api.go/checkout`、`orders.go/Checkout`、`worker.go/Submit`、`flash.go/FinalizeTicket`。秒杀断点停留过久会触发请求超时、30 秒预扣租约或 120 秒排队超时，初学时先调试普通下单。

## 热更新

根目录终端 A：

```powershell
$env:APP_ORIGINS='http://localhost:5173,http://127.0.0.1:5173,http://127.0.0.1:8088'
go run ./cmd/mall -mode demo
```

终端 B：

```powershell
cd frontend
npm run dev
```

打开 Vite 的 5173 地址，`/api` 代理到 8088。允许的 Origin 必须和浏览器地址一致。直接用 Go 的 8088 访问时，修改 Vue 后须重新构建并重启 Go，因为前端资源在编译时嵌入二进制。

## 完整环境

启动 Docker Desktop Linux containers，停止本地 8088 服务：

```powershell
./scripts/setup-mall.ps1
docker compose --env-file .env.mall -f compose.mall.yaml up --build -d
docker compose --env-file .env.mall -f compose.mall.yaml ps -a
docker compose --env-file .env.mall -f compose.mall.yaml logs --tail 80 api worker init
```

MySQL、Redis、RabbitMQ 健康后运行 init，初始化成功后 API 和 Worker 启动。管理员密码在 `.env.mall`。停止但保留数据：

```powershell
docker compose --env-file .env.mall -f compose.mall.yaml down
```

不要加 `-v`，除非明确要销毁全部商城数据。修改配置中的初始数据库/RabbitMQ 密码不会自动改变已有数据卷中的密码。

使用 MySQL 8.4、Redis 7.4、RabbitMQ 4.2。补丁版本会随镜像更新变化；严格复现时验证后锁定镜像 digest。三个服务没有宿主机开放端口，商城仅绑定回环地址。

本机 HTTP 配置 `COOKIE_SECURE=false`。HTTPS 部署必须设置 `COOKIE_SECURE=true` 与精确的 `APP_ORIGINS=https://你的域名`，由可信网关终止 TLS。demo 禁止监听公网 IP。应用不信任任意 X-Forwarded-For，反向代理后登录 IP 限流会共享代理 IP，公开部署前须明确可信代理链或在网关执行客户端限流。

| 配置 | 用途 |
|---|---|
| MALL_MODE | demo 或 stack |
| MALL_ROLE | all、api、worker |
| MALL_ADDR | 默认 127.0.0.1:8088；容器内 0.0.0.0:8088 |
| MYSQL_DSN | stack MySQL 主库连接串 |
| REDIS_ADDR / REDIS_PASSWORD | stack Redis 配置 |
| AMQP_URL | RabbitMQ URL；生成脚本使用 hex 密码避免 URL 转义 |
| APP_ORIGINS | 逗号分隔的精确来源 |
| COOKIE_SECURE | stack 默认 true；本机 Compose 显式 false |
| ADMIN_EMAIL / ADMIN_PASSWORD | 初始化管理员，密码至少 12 字节 |
| ORDER_TTL | 默认 15m；调试可用 10s，范围 1s～24h |
| DEMO_DB | SQLite 文件，默认 .local/mall.db |

单独初始化：`go run ./cmd/mall -mode stack -init`。拆分启动：`-role api`、`-role worker`。所有实例连接同一主库、同一 Redis 库和 RabbitMQ vhost。

## 业务演示顺序

1. 用户登录，搜索 AIR，加入购物袋，选择地址，结算。
2. 执行模拟支付后为待发货；可在发货前模拟退款。
3. 管理员订单履约中填写模拟物流编号，用户确认收货。
4. 秒杀专区抢一件耳机，观察排队到生成订单的变化。
5. 管理员查看秒杀监控，库存差额应为 0。
6. 新建活动会从普通库存划拨名额；预热并发布后才能参与。

购物车不占库存，结算才扣减。初始两场活动立即开始，一场一小时后开始。所有账户资料均建议使用虚构测试信息。

## 故障演练

仅在自己的本地测试环境中执行：

```powershell
docker compose --env-file .env.mall -f compose.mall.yaml stop worker
# 浏览器提交测试秒杀，看到已受理/排队
docker compose --env-file .env.mall -f compose.mall.yaml start worker
```

恢复后继续消费；排队超过 120 秒会关闭并补偿。RabbitMQ 暂停时 Outbox 保留发布任务，恢复后重试。Redis 不可用时关闭准入，不回退到无保护的数据库下单。

缓存丢失后通过后台“重建库存”，按数据库剩余库存恢复新代次。不要手动把 Redis 库存设回初始值。最终检查订单数、数据库库存、差额和积压任务，而不只是页面提示。
