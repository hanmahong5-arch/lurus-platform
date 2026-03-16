package app

import (
	"context"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ProductService manages product catalog and plan operations.
type ProductService struct {
	products planStore
}

func NewProductService(products planStore) *ProductService {
	return &ProductService{products: products}
}

func (s *ProductService) GetByID(ctx context.Context, id string) (*entity.Product, error) {
	return s.products.GetByID(ctx, id)
}

func (s *ProductService) ListActive(ctx context.Context) ([]entity.Product, error) {
	return s.products.ListActive(ctx)
}

func (s *ProductService) CreateProduct(ctx context.Context, p *entity.Product) error {
	return s.products.Create(ctx, p)
}

func (s *ProductService) UpdateProduct(ctx context.Context, p *entity.Product) error {
	return s.products.Update(ctx, p)
}

func (s *ProductService) GetPlanByID(ctx context.Context, id int64) (*entity.ProductPlan, error) {
	return s.products.GetPlanByID(ctx, id)
}

func (s *ProductService) ListPlans(ctx context.Context, productID string) ([]entity.ProductPlan, error) {
	return s.products.ListPlans(ctx, productID)
}

func (s *ProductService) CreatePlan(ctx context.Context, p *entity.ProductPlan) error {
	return s.products.CreatePlan(ctx, p)
}

func (s *ProductService) UpdatePlan(ctx context.Context, p *entity.ProductPlan) error {
	return s.products.UpdatePlan(ctx, p)
}
