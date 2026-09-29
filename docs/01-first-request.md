# 第一课：跟着一次请求读代码

今天只理解一件事：**浏览器点击“抢购”，怎么变成 Go 代码里的一次方法调用？** 先不用研究数据库锁。

## 1. 我们到底在卖什么

打开 `internal/domain/model.go`。

`Product` 是一个秒杀场次的商品，包含价格、初始库存、当前库存、开始/结束时间。`Order` 是成功抢到一件商品的凭证。`BuyInput` 是准备下单的输入，`BuyResult` 是输出。

```go
type BuyInput struct {
    UserID     int64
    ProductID  int64
    RequestKey string
}
```

例如用户 7 抢商品 1，请求编号为 `learn-0007`。输入描述“谁，要什么，这次请求是谁”；订单 ID 是服务器创建后的结果，不能由客户端指定。

为什么没有购买数量？我们明确规定每人一场只买一件，让你集中解决并发核心问题。金额 `PriceCents=9900` 表示 99 元，计算和存储使用整数分。

`json:"product_id"` 是结构体标签，JSON 编码器用它决定外部字段名。业务输入不直接对外解析；HTTP 层只允许客户端传商品 ID，用户身份从签名令牌中取得。

## 2. main 是组装工厂

打开 `cmd/server/main.go` 的 `run`：

```go
repo = store.NewMemory(domain.DemoProducts(time.Now()))
// ...
h := httpapi.New(service.New(repo), auth.New(cfg.Secret), ...)
```

实际顺序是：读取配置 → 选择内存或数据库 → 创建业务服务 → 创建 HTTP 路由 → 启动端口监听。`main` 不负责下单规则，它把需要合作的对象交给彼此。

`service.New(repo)` 叫“依赖注入”：Service 需要一个仓库，调用者把仓库传进去。它不是什么特殊 Go 语法，只是普通函数参数。

## 3. 一个请求经过哪些层

```text
浏览器 POST /api/orders
    ↓
httpapi.API.buy       读取令牌、JSON 和 Idempotency-Key
    ↓
service.Service.Buy   校验正数 ID 和请求编号格式
    ↓
store.Memory.Buy     原子完成检查、扣库存、写订单
或 store.MySQL.Buy   用数据库事务完成同样的业务契约
    ↓
BuyResult + error    逐层返回
    ↓
HTTP 201 + JSON      浏览器展示订单
```

`httpapi.New` 中这句负责匹配 URL：

```go
mux.HandleFunc("POST /api/orders", a.buy)
```

这里传递的是函数 `a.buy`，不是立刻执行 `a.buy(...)`。服务器以后收到对应请求时才调用它。

`buy` 中 `decode` 把 JSON 转成 Go struct；`a.user` 验证令牌并取出用户；再构造 `domain.BuyInput` 交给 Service。HTTP 层只做协议相关工作，不直接修改数据库库存。

## 4. Store 接口有什么用

打开 `internal/service/service.go`：

```go
type Service struct { store Store }
func New(store Store) *Service { return &Service{store: store} }
```

字段里的 `Store` 是接口，约定“必须会做什么”。`*store.Memory` 和 `*store.MySQL` 只要实现接口列出的所有方法，就能传给 `New`，不需要 `implements` 关键字。

这不是为了炫接口：你现在没有数据库也能运行；之后换 MySQL，HTTP 代码不必跟着改；同一组并发规则也能对两种存储重复验证。

`*Service` 是指针；`&Service{...}` 创建结构体并取地址。Go 的参数始终是值传递，传指针时复制的是地址值，并不是把语言改成了“引用传递”。

## 5. error 怎样往回传

```go
result, err := a.svc.Buy(...)
if err != nil {
    a.businessError(w, err)
    return
}
respond(w, status, result)
```

返回值有两个：业务结果和错误。`err == nil` 表示没有报告错误。每层只处理自己理解的错误，其他错误交给上层。

`return` 必不可少：写过 HTTP 错误响应不代表 Go 会自动停止函数。继续执行可能重复写响应。

## 本课练习

1. 启动程序，点击商品 2 两次，记下两次状态码、订单号和库存。
2. 找到 `Product.PriceCents`，说出页面为什么显示 39.00 而不是 3900。
3. 找到 `service.New` 的调用，解释是谁创建了仓库、谁使用了仓库。
4. 用纸画出请求经过的四个位置，暂时把数据库当作黑盒。

验收：你能说出“HTTP 层解析，Service 校验，Store 原子落地，结果沿路返回”。下一课再打开这个黑盒。
