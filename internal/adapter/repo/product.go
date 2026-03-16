package repo

import (
	"context"
	"errors"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// ProductRepo manages products and plans.
type ProductRepo struct {
	db *gorm.DB
}

func NewProductRepo(db *gorm.DB) *ProductRepo { return &ProductRepo{db: db} }

func (r *ProductRepo) GetByID(ctx context.Context, id string) (*entity.Product, error) {
	var p entity.Product
	err := r.db.WithContext(ctx).First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

func (r *ProductRepo) ListActive(ctx context.Context) ([]entity.Product, error) {
	var list []entity.Product
	err := r.db.WithContext(ctx).Where("status = 1").Order("sort_order ASC").Find(&list).Error
	return list, err
}

func (r *ProductRepo) Create(ctx context.Context, p *entity.Product) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *ProductRepo) Update(ctx context.Context, p *entity.Product) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *ProductRepo) GetPlanByID(ctx context.Context, id int64) (*entity.ProductPlan, error) {
	var p entity.ProductPlan
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

func (r *ProductRepo) GetPlanByCode(ctx context.Context, productID, code string) (*entity.ProductPlan, error) {
	var p entity.ProductPlan
	err := r.db.WithContext(ctx).
		Where("product_id = ? AND code = ?", productID, code).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

func (r *ProductRepo) ListPlans(ctx context.Context, productID string) ([]entity.ProductPlan, error) {
	var list []entity.ProductPlan
	err := r.db.WithContext(ctx).
		Where("product_id = ? AND status = 1", productID).
		Order("sort_order ASC").Find(&list).Error
	return list, err
}

func (r *ProductRepo) CreatePlan(ctx context.Context, p *entity.ProductPlan) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *ProductRepo) UpdatePlan(ctx context.Context, p *entity.ProductPlan) error {
	return r.db.WithContext(ctx).Save(p).Error
}
