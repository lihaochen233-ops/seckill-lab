// Package domain 定义业务语言，不依赖 HTTP、MySQL 或 Redis。
package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("参数不合法")
	ErrNotFound    = errors.New("资源不存在")
	ErrSoldOut     = errors.New("库存不足")
	ErrNotStarted  = errors.New("活动未开始")
	ErrEnded       = errors.New("活动已结束")
	ErrPurchased   = errors.New("每人每场限购一件")
	ErrKeyConflict = errors.New("此请求编号已经用于另一件商品")
)

// 一条 Product 就是一场独立秒杀；下一场活动应创建新的 Product。
type Product struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	PriceCents   int64     `json:"price_cents"` // 金额用分，避免浮点误差。
	InitialStock int64     `json:"initial_stock"`
	Stock        int64     `json:"stock"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
}

type Order struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	ProductID  int64     `json:"product_id"`
	RequestKey string    `json:"request_key"`
	PriceCents int64     `json:"price_cents"` // 成交价格快照，不随商品改价变化。
	CreatedAt  time.Time `json:"created_at"`
}

type BuyInput struct {
	UserID     int64
	ProductID  int64
	RequestKey string
}

type BuyResult struct {
	Order    Order `json:"order"`
	Replayed bool  `json:"replayed"`
}

func CheckSale(p Product, now time.Time) error {
	if now.Before(p.StartsAt) {
		return ErrNotStarted
	}
	if !now.Before(p.EndsAt) {
		return ErrEnded
	}
	if p.Stock <= 0 {
		return ErrSoldOut
	}
	return nil
}

func DemoProducts(now time.Time) []Product {
	return []Product{
		{ID: 1, Name: "Go 工程师机械键盘", PriceCents: 9900, InitialStock: 100, Stock: 100, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(24 * time.Hour)},
		{ID: 2, Name: "程序员桌面台灯", PriceCents: 3900, InitialStock: 25, Stock: 25, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(24 * time.Hour)},
	}
}
