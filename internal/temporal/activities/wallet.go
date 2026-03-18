package activities

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// WalletActivities wraps WalletService for Temporal.
type WalletActivities struct {
	Wallets *app.WalletService
}

// DebitInput is the input for WalletDebit activity.
type DebitInput struct {
	AccountID int64
	Amount    float64
	TxType    string
	Desc      string
	RefType   string
	RefID     string
	ProductID string
}

// DebitOutput returns the transaction ID on success.
type DebitOutput struct {
	TransactionID int64
}

// Debit charges the wallet. Returns a non-retryable error on insufficient funds.
func (a *WalletActivities) Debit(ctx context.Context, in DebitInput) (*DebitOutput, error) {
	tx, err := a.Wallets.Debit(ctx, in.AccountID, in.Amount, in.TxType, in.Desc, in.RefType, in.RefID, in.ProductID)
	if err != nil {
		slog.Warn("activity/debit: failed", "account_id", in.AccountID, "amount", in.Amount, "tx_type", in.TxType, "ref_id", in.RefID, "err", err)
		return nil, fmt.Errorf("wallet debit: %w", err)
	}
	slog.Info("activity/debit", "account_id", in.AccountID, "amount", in.Amount, "tx_type", in.TxType, "tx_id", tx.ID)
	return &DebitOutput{TransactionID: tx.ID}, nil
}

// CreditInput is the input for WalletCredit (compensation/refund).
type CreditInput struct {
	AccountID int64
	Amount    float64
	TxType    string
	Desc      string
	RefType   string
	RefID     string
	ProductID string
}

// Credit adds funds back to the wallet (used for saga compensation).
func (a *WalletActivities) Credit(ctx context.Context, in CreditInput) error {
	_, err := a.Wallets.Credit(ctx, in.AccountID, in.Amount, in.TxType, in.Desc, in.RefType, in.RefID, in.ProductID)
	if err != nil {
		slog.Error("activity/credit: failed", "account_id", in.AccountID, "amount", in.Amount, "tx_type", in.TxType, "ref_id", in.RefID, "err", err)
		return fmt.Errorf("wallet credit: %w", err)
	}
	slog.Info("activity/credit", "account_id", in.AccountID, "amount", in.Amount, "tx_type", in.TxType)
	return nil
}

// MarkOrderPaidOutput contains the order details after marking it paid.
type MarkOrderPaidOutput struct {
	OrderNo       string
	AccountID     int64
	OrderType     string // "topup" | "subscription" | "one_time"
	ProductID     string
	PlanID        int64  // 0 if nil
	AmountCNY     float64
	PaymentMethod string
	ExternalID    string
}

// MarkOrderPaid marks a payment order as paid and credits wallet for topup orders.
// Idempotent: calling twice for an already-paid order returns the order without side effects.
func (a *WalletActivities) MarkOrderPaid(ctx context.Context, orderNo string) (*MarkOrderPaidOutput, error) {
	order, err := a.Wallets.MarkOrderPaid(ctx, orderNo)
	if err != nil {
		slog.Error("activity/mark-order-paid: failed", "order_no", orderNo, "err", err)
		return nil, fmt.Errorf("mark order paid: %w", err)
	}
	slog.Info("activity/mark-order-paid", "order_no", orderNo, "account_id", order.AccountID, "order_type", order.OrderType, "amount_cny", order.AmountCNY)
	out := &MarkOrderPaidOutput{
		OrderNo:       order.OrderNo,
		AccountID:     order.AccountID,
		OrderType:     order.OrderType,
		ProductID:     order.ProductID,
		AmountCNY:     order.AmountCNY,
		PaymentMethod: order.PaymentMethod,
		ExternalID:    order.ExternalID,
	}
	if order.PlanID != nil {
		out.PlanID = *order.PlanID
	}
	return out, nil
}

// ExpireStalePendingOrders marks pending orders older than 24h as expired.
func (a *WalletActivities) ExpireStalePendingOrders(ctx context.Context) (int64, error) {
	return a.Wallets.ExpireStalePendingOrders(ctx, 24*time.Hour)
}
