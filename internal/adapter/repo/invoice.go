package repo

import (
	"context"
	"errors"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// InvoiceRepo implements invoiceStore backed by PostgreSQL via GORM.
type InvoiceRepo struct {
	db *gorm.DB
}

// NewInvoiceRepo creates a new InvoiceRepo.
func NewInvoiceRepo(db *gorm.DB) *InvoiceRepo { return &InvoiceRepo{db: db} }

// Create inserts a new invoice record.
func (r *InvoiceRepo) Create(ctx context.Context, inv *entity.Invoice) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

// GetByOrderNo returns the invoice linked to a payment order, or nil if none exists.
func (r *InvoiceRepo) GetByOrderNo(ctx context.Context, orderNo string) (*entity.Invoice, error) {
	var inv entity.Invoice
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetByInvoiceNo returns an invoice by its unique invoice number, or nil if not found.
func (r *InvoiceRepo) GetByInvoiceNo(ctx context.Context, invoiceNo string) (*entity.Invoice, error) {
	var inv entity.Invoice
	err := r.db.WithContext(ctx).Where("invoice_no = ?", invoiceNo).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// ListByAccount returns paginated invoices for a given account, newest first.
func (r *InvoiceRepo) ListByAccount(ctx context.Context, accountID int64, page, pageSize int) ([]entity.Invoice, int64, error) {
	var list []entity.Invoice
	var total int64
	q := r.db.WithContext(ctx).Model(&entity.Invoice{}).Where("account_id = ?", accountID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := q.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&list).Error
	return list, total, err
}

// AdminList returns paginated invoices across all accounts, optionally filtered by accountID.
// Pass filterAccountID=0 to return all accounts.
func (r *InvoiceRepo) AdminList(ctx context.Context, filterAccountID int64, page, pageSize int) ([]entity.Invoice, int64, error) {
	var list []entity.Invoice
	var total int64
	q := r.db.WithContext(ctx).Model(&entity.Invoice{})
	if filterAccountID != 0 {
		q = q.Where("account_id = ?", filterAccountID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := q.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&list).Error
	return list, total, err
}
