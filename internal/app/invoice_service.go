package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// InvoiceService handles invoice generation and retrieval.
// An invoice is generated 1:1 per paid payment order (idempotent).
type InvoiceService struct {
	invoices invoiceStore
	wallets  walletStore
}

// NewInvoiceService creates a new InvoiceService.
func NewInvoiceService(invoices invoiceStore, wallets walletStore) *InvoiceService {
	return &InvoiceService{invoices: invoices, wallets: wallets}
}

// Generate creates an invoice for a paid order.
// Idempotent: if an invoice already exists for the order, it is returned unchanged.
// Returns an error if the order is not paid or does not belong to accountID.
func (s *InvoiceService) Generate(ctx context.Context, accountID int64, orderNo string) (*entity.Invoice, error) {
	// Check for an existing invoice first (idempotent path).
	existing, err := s.invoices.GetByOrderNo(ctx, orderNo)
	if err != nil {
		return nil, fmt.Errorf("lookup invoice by order: %w", err)
	}
	if existing != nil {
		// IDOR: the invoice must belong to the caller.
		if existing.AccountID != accountID {
			return nil, errors.New("invoice not found")
		}
		return existing, nil
	}

	// Fetch the payment order (walletStore enforces account ownership).
	order, err := s.wallets.GetPaymentOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	if order == nil {
		return nil, errors.New("order not found")
	}
	// IDOR guard — the order must belong to the calling account.
	if order.AccountID != accountID {
		return nil, errors.New("order not found")
	}
	if order.Status != entity.OrderStatusPaid {
		return nil, errors.New("invoice can only be generated for paid orders")
	}

	// Build line items from the order.
	items := []entity.InvoiceLineItem{
		{
			Description: fmt.Sprintf("%s subscription", order.ProductID),
			Quantity:    1,
			UnitPrice:   order.AmountCNY,
			Amount:      order.AmountCNY,
		},
	}
	lineJSON, _ := json.Marshal(items)

	now := time.Now().UTC()
	inv := &entity.Invoice{
		InvoiceNo:   generateInvoiceNo(),
		AccountID:   accountID,
		OrderNo:     orderNo,
		IssueDate:   now,
		LineItems:   lineJSON,
		SubtotalCNY: order.AmountCNY,
		TotalCNY:    order.AmountCNY,
		Currency:    "CNY",
		Status:      entity.InvoiceStatusIssued,
	}
	if err := s.invoices.Create(ctx, inv); err != nil {
		return nil, fmt.Errorf("create invoice: %w", err)
	}
	return inv, nil
}

// GetByNo returns an invoice by its invoice number, enforcing IDOR.
func (s *InvoiceService) GetByNo(ctx context.Context, accountID int64, invoiceNo string) (*entity.Invoice, error) {
	inv, err := s.invoices.GetByInvoiceNo(ctx, invoiceNo)
	if err != nil {
		return nil, fmt.Errorf("get invoice: %w", err)
	}
	if inv == nil || inv.AccountID != accountID {
		return nil, errors.New("invoice not found")
	}
	return inv, nil
}

// ListByAccount returns paginated invoices for an account.
func (s *InvoiceService) ListByAccount(ctx context.Context, accountID int64, page, pageSize int) ([]entity.Invoice, int64, error) {
	return s.invoices.ListByAccount(ctx, accountID, page, pageSize)
}

// AdminList returns paginated invoices for all accounts, optionally filtered by accountID.
// Pass filterAccountID=0 to list all accounts.
func (s *InvoiceService) AdminList(ctx context.Context, filterAccountID int64, page, pageSize int) ([]entity.Invoice, int64, error) {
	return s.invoices.AdminList(ctx, filterAccountID, page, pageSize)
}

// generateInvoiceNo returns a unique invoice number with prefix "LI" and nanosecond timestamp.
func generateInvoiceNo() string {
	return fmt.Sprintf("LI%X", time.Now().UnixNano())
}
