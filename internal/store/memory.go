package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"seckill-lab/internal/domain"
)

type userKey struct {
	user int64
	key  string
}
type purchaseKey struct{ user, product int64 }

// Memory 仅用于单进程教学：重启丢数据，多个实例不能共享库存。
type Memory struct {
	mu        sync.Mutex
	products  map[int64]domain.Product
	orders    map[int64]domain.Order
	requests  map[userKey]int64
	purchases map[purchaseKey]int64
	nextID    int64
	now       func() time.Time
}

func NewMemory(products []domain.Product) *Memory {
	m := &Memory{products: make(map[int64]domain.Product), orders: make(map[int64]domain.Order), requests: make(map[userKey]int64), purchases: make(map[purchaseKey]int64), now: time.Now}
	for _, p := range products {
		m.products[p.ID] = p
	}
	return m
}
func (m *Memory) Ping(ctx context.Context) error { return ctx.Err() }
func (m *Memory) List(ctx context.Context) ([]domain.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ps := make([]domain.Product, 0, len(m.products))
	for _, p := range m.products {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	return ps, nil
}
func (m *Memory) Product(ctx context.Context, id int64) (domain.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Product{}, err
	}
	p, ok := m.products[id]
	if !ok {
		return p, domain.ErrNotFound
	}
	return p, nil
}
func (m *Memory) Order(ctx context.Context, user, id int64) (domain.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Order{}, err
	}
	o, ok := m.orders[id]
	if !ok || o.UserID != user {
		return domain.Order{}, domain.ErrNotFound
	}
	return o, nil
}
func (m *Memory) Buy(ctx context.Context, in domain.BuyInput) (domain.BuyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.BuyResult{}, err
	}
	// 重试判断先于库存/活动时间判断：抢到最后一件后，重试也应返回原订单。
	rk := userKey{in.UserID, in.RequestKey}
	if id, ok := m.requests[rk]; ok {
		o := m.orders[id]
		if o.ProductID != in.ProductID {
			return domain.BuyResult{}, domain.ErrKeyConflict
		}
		return domain.BuyResult{Order: o, Replayed: true}, nil
	}
	p, ok := m.products[in.ProductID]
	if !ok {
		return domain.BuyResult{}, domain.ErrNotFound
	}
	pk := purchaseKey{in.UserID, in.ProductID}
	if _, ok := m.purchases[pk]; ok {
		return domain.BuyResult{}, domain.ErrPurchased
	}
	if err := domain.CheckSale(p, m.now()); err != nil {
		return domain.BuyResult{}, err
	}
	m.nextID++
	o := domain.Order{ID: m.nextID, UserID: in.UserID, ProductID: in.ProductID, RequestKey: in.RequestKey, PriceCents: p.PriceCents, CreatedAt: m.now().UTC()}
	p.Stock--
	m.products[p.ID], m.orders[o.ID], m.requests[rk], m.purchases[pk] = p, o, o.ID, o.ID
	return domain.BuyResult{Order: o}, nil
}
