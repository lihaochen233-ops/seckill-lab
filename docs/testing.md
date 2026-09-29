# 测试与性能验证

## 常规检查

在项目根目录执行：

```powershell
go test ./...
go vet ./...
go build ./cmd/...
cd frontend
npm ci
npm run build
```

默认业务测试使用独立 SQLite 临时数据库和 miniredis。前端构建包含 TypeScript 检查，产物输出到 `web/mall/dist`，供 Go embed 打包。

## 测试覆盖

- 并发争抢、重复请求、重复消费、取消与释放后的库存守恒。
- 结算幂等、请求参数一致性、地址与订单归属。
- 支付、取消、退款、发货与收货状态机。
- 订单写入故障时库存与事务回滚。
- 孤儿预扣补偿、迟到请求隔离、缓存重建与排队超时。
- Outbox 租约、发布重试与后台任务恢复。
- 会话、CSRF/Origin、权限、限流和金额篡改。
- stack 初始化仅创建管理员；demo 初始化提供示例数据且可重复执行。

## MySQL 测试

配置 `TEST_MYSQL_DSN` 后，每个用例创建随机命名的 `pulse_test_*` 数据库，结束时删除。账号需要 CREATE/DROP DATABASE 权限，应连接独立测试服务。

```powershell
$env:TEST_MYSQL_DSN='root:测试密码@tcp(127.0.0.1:3306)/'
go test ./internal/mall -count=1 -v
Remove-Item Env:TEST_MYSQL_DSN
```

提高秒杀一致性测试用户数：

```powershell
$env:MALL_STRESS_USERS='1000'
go test ./internal/mall -run TestFlashConcurrencyAndDuplicateDelivery -count=1 -v
Remove-Item Env:MALL_STRESS_USERS
```

该用例直接调用业务引擎，验证订单与库存一致性，耗时不代表 HTTP/RabbitMQ 全链路吞吐。

## Redis 与 RabbitMQ 集成测试

配置 `TEST_REDIS_ADDR` 和 `TEST_AMQP_URL` 后启用 `TestRealMiddlewareLifecycle`；缺少配置时跳过。需使用专用空环境，测试会使用 `pulse.flash` 队列与活动缓存。

```powershell
$env:TEST_REDIS_ADDR='127.0.0.1:6379'
$env:TEST_AMQP_URL='amqp://test:测试密码@127.0.0.1:5672/'
go test ./internal/mall -run TestRealMiddlewareLifecycle -count=1 -v
Remove-Item Env:TEST_REDIS_ADDR,Env:TEST_AMQP_URL
```

验证内容包括发布确认、异步订单、取消返库、死信及重投。测试 MySQL 与中间件组合时，同时设置 `TEST_MYSQL_DSN`。

## 持续集成

`.github/workflows/ci.yml` 在 push 和 pull request 时配置 MySQL 8.4、Redis 7.4、RabbitMQ 4.2，构建前端并执行 Go vet、race 测试和命令构建。运行结果以对应提交的 GitHub Actions 状态为准。本地 race 检查需要平台支持及可用的 C/C++ 工具链。

## HTTP 全链路压测

`cmd/mallbench` 在测试环境生成用户、商品和活动，请求经过 HTTP 鉴权、CSRF、限流、Lua、Outbox 与 Worker，结束后核对订单、买家数、库存与队列余量。用户和会话在计时前准备，结束后撤销会话，业务数据保留供核查。

Compose 启动后先执行小规模检查：

```powershell
docker compose run --rm bench -allow-test-data -base http://api:8088 -origin http://127.0.0.1:8088 -n 100 -c 8 -stock 20
```

按环境逐级增加并发：

```powershell
docker compose run --rm bench -allow-test-data -base http://api:8088 -origin http://127.0.0.1:8088 -n 1000 -c 64 -stock 100
```

工具输出 JSON，包含响应分类、准入 RPS、P50/P95/P99、异步处理等待时间、订单数量与库存差额。429、503、网络异常、订单缺失、超卖或库存不一致均导致非零退出，限流应独立记录。

宿主机可运行 `go run ./cmd/mallbench`，需配置与 API 相同的 `MYSQL_DSN / REDIS_ADDR / REDIS_PASSWORD`。Compose 默认不开放中间件端口，建议使用容器命令。

性能报告需记录硬件、服务版本、连接池、Worker 数量、并发数、失败率及延迟分位数。结果仅适用于对应配置和场景。
