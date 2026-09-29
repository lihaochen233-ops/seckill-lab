package service

import (
	"context"
	"regexp"

	"seckill-lab/internal/domain"
)

// Store 是业务层对存储的需求。内存版和 MySQL 版实现同一契约。
// Buy 必须原子完成：检查幂等和限购、扣库存、写订单。
type Store interface {
	List(context.Context) ([]domain.Product, error)
	Product(context.Context, int64) (domain.Product, error)
	Order(context.Context, int64, int64) (domain.Order, error)
	Buy(context.Context, domain.BuyInput) (domain.BuyResult, error)
	Ping(context.Context) error
}

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

var validKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

func (s *Service) Buy(ctx context.Context, in domain.BuyInput) (domain.BuyResult, error) {
	if in.UserID <= 0 || in.ProductID <= 0 || !validKey.MatchString(in.RequestKey) {
		return domain.BuyResult{}, domain.ErrInvalid
	}
	return s.store.Buy(ctx, in)
}
func (s *Service) Products(ctx context.Context) ([]domain.Product, error) { return s.store.List(ctx) }
func (s *Service) Product(ctx context.Context, id int64) (domain.Product, error) {
	return s.store.Product(ctx, id)
}
func (s *Service) Order(ctx context.Context, user, id int64) (domain.Order, error) {
	return s.store.Order(ctx, user, id)
}
func (s *Service) Ping(ctx context.Context) error { return s.store.Ping(ctx) }
