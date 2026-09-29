# API 与安全边界

API 根路径 `/api`，JSON 字段为 snake_case，金额整数单位“分”，时间为 Unix 毫秒。前端完整调用示例在 `frontend/src/api.ts` 与页面组件中；路由定义集中在 `internal/mall/api.go/NewAPI`。

## 认证方式

注册/登录响应返回 `{user, csrf_token, expires_at}`，同时设置 `pulse_session` Cookie。会话 token 是 24 个随机字节的 hex，Redis 使用 token 的 SHA-256 摘要作为 key；前端不把登录凭证保存在 localStorage。

所有写请求必须携带精确允许的 `Origin`；登录后的写请求还必须有 Cookie 和 `X-CSRF-Token`。浏览器自动发送 Origin，手动使用 Postman/HTTP 工具时需要填写。登录失败统一返回邮箱或密码不正确。

普通下单与秒杀请求还需 `Idempotency-Key`，8～64 位英文字母、数字、下划线或连字符。网络重试沿用原键。普通结算会校验请求指纹，禁止同键更换地址或商品数量。

## 接口地图

| 方法与路径 | 内容 | 权限 |
|---|---|---|
| GET /config | 模式、模拟支付标识、服务端时间 | 公共 |
| GET /health | DB/Redis/MQ 连通性 | 公共 |
| POST /auth/register | email、name、password | 公共，限流 |
| POST /auth/login | email、password | 公共，IP/账号限流 |
| GET /auth/session | 当前用户和 CSRF | 登录 |
| POST /auth/logout | 撤销当前会话 | 登录 |
| GET /products | q、category、sort、page、size | 公共 |
| GET /products/{id} | 上架商品详情 | 公共 |
| GET /activities | 活动、时间、可抢名额和 ready | 公共 |
| GET/POST /addresses | 地址列表/新增 | 本人 |
| PUT/DELETE /addresses/{id} | 编辑/删除地址 | 本人 |
| GET /cart | 购物车 | 本人 |
| PUT /cart/{productId} | quantity 设定数量；0 删除 | 本人 |
| POST /orders | address_id、items，幂等请求头 | 本人 |
| GET /orders | status、page、size | 本人 |
| GET /orders/{id} | 订单快照 | 本人 |
| POST /orders/{id}/pay | 模拟支付，不接收客户端金额 | 本人 |
| POST /orders/{id}/cancel | 取消待支付订单 | 本人 |
| POST /orders/{id}/refund | 已支付未发货模拟退款 | 本人 |
| POST /orders/{id}/receive | 确认收货 | 本人 |
| POST /activities/{id}/join | address_id，幂等请求头 | 本人 |
| GET /tickets、/tickets/{id} | 抢购受理结果 | 本人 |
| GET /admin/stats | 业务统计、死信数和当前 API 进程计数 | 管理员 |
| GET/POST /admin/products | 商品列表/创建 | 管理员 |
| PUT /admin/products/{id} | 编辑资料，不覆盖实时库存 | 管理员 |
| POST /admin/products/{id}/restock | quantity 增量补货 | 管理员 |
| GET /admin/orders | 全部订单与状态筛选 | 管理员 |
| POST /admin/orders/{id}/ship | tracking 模拟物流单号 | 管理员 |
| GET/POST /admin/activities | 活动列表/划拨库存创建活动 | 管理员 |
| POST /admin/activities/{id}/publish | 重建并开放准入 | 管理员 |
| POST /admin/activities/{id}/pause | 暂停新准入 | 管理员 |
| POST /admin/activities/{id}/rebuild | 显式恢复缓存 | 管理员 |
| GET /admin/ops | Outbox 积压与活动账本核对 | 管理员 |
| POST /admin/ops/retry-dead | 最多重投 100 条死信 | 管理员 |
| GET /admin/audit | 最近 100 条审计 | 管理员 |

产品排序只接受 `price_asc / price_desc / newest`，其他为精选排序；分类固定为桌面数码、通勤随行、品质生活。size 最大 50。管理商品 JSON 参见 `Product`，管理员修改不可信 ID 也由后端路由中的资源 ID 覆盖。

## 下单与受理示例

```http
POST /api/orders
Origin: http://127.0.0.1:8088
Content-Type: application/json
Cookie: pulse_session=<登录所得>
X-CSRF-Token: <session 响应所得>
Idempotency-Key: checkout-example-001

{"address_id":1,"items":[{"product_id":1,"quantity":2}]}
```

首次成功 201，重放原订单 200。秒杀接口请求体只保留 address_id，成功受理 202 返回 ticket。ticket.status 为 queued、ordered 或 rejected，ordered 时才有 order_id。

错误响应：`{code,message,request_id}`。常见状态：400 参数问题、401 未登录、403 来源/CSRF/权限失败、404 不存在或不属于本人、409 售罄/重复参与/状态冲突、429 限流、503 依赖失败或活动未就绪。

## 已实现的防护

- 密码为 PBKDF2-HMAC-SHA256，随机 16 字节盐、600000 轮、32 字节结果，常量时间比较。登录不会把密码写日志；注册接口不接受 role。
- Cookie 为 HttpOnly + SameSite=Strict；stack 默认 Secure。会话 12 小时，有 Redis TTL 和显式 expires_at 双重检查。登录轮换旧会话，退出立即撤销。
- 客户端不能指定用户 ID、角色、订单金额或支付金额。订单、地址、ticket 在 SQL 查询和操作中检查归属，管理路由检查服务端会话角色。
- 参数化 SQL，排序白名单，JSON 不允许未知字段；表单接口最多 32 KiB，请求头、连接、读取和处理均有超时。
- 注册、登录、结算、订单操作、秒杀分别限流；单进程 API 并发有上限；缓存故障不绕过安全校验。
- Vue 文本插值转义，不使用 v-html 渲染用户输入；图片只使用本地固定插画。CSP 禁止外部脚本、对象和页面嵌套，设置 nosniff 与 Referrer-Policy。
- 管理操作记录审计。页面在按钮之外仍执行后端鉴权，隐藏按钮本身不被当成权限控制。

## 不能由本项目替代的保护

没有邮件验证、找回密码、MFA、验证码、设备风控、真实支付签名、第三方渗透测试和生产告警。限流无法阻止所有批量注册或分布式攻击。角色变化目前没有管理入口；如后续增加，必须撤销该用户旧会话，不能仅更新数据库角色后继续使用旧会话权限。

仅有页面安全头并不等于无 XSS；后续引入富文本、用户上传或第三方脚本时要重新评估。当前应用不提供任意文件上传。公开部署需 HTTPS、隔离网络、最小权限账号、补丁管理、备份恢复与真实安全测试。

参考：[OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) 与 [Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)。
