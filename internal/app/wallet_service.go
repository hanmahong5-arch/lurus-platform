package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
)

// generateOrderNo creates a unique order number: "LO" + yyyyMMdd + 8-hex-chars.
func generateOrderNo(_ int64) string {
	return fmt.Sprintf("LO%s%s", time.Now().UTC().Format("20060102"), uuid.New().String()[:8])
}

// WalletService orchestrates topup, debit, and redemption use cases.
type WalletService struct {
	wallets walletStore
	vip     *VIPService
}

func NewWalletService(wallets walletStore, vip *VIPService) *WalletService {
	return &WalletService{wallets: wallets, vip: vip}
}

// GetWallet returns the wallet for an account (creates it if missing).
func (s *WalletService) GetWallet(ctx context.Context, accountID int64) (*entity.Wallet, error) {
	return s.wallets.GetOrCreate(ctx, accountID)
}

// GetBalance returns the wallet for balance lookup (alias for GetWallet, read-only intent).
func (s *WalletService) GetBalance(ctx context.Context, accountID int64) (*entity.Wallet, error) {
	return s.wallets.GetByAccountID(ctx, accountID)
}

// Topup credits the wallet and triggers a VIP recalculation.
func (s *WalletService) Topup(ctx context.Context, accountID int64, amountCNY float64, orderNo string) (*entity.WalletTransaction, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.topup")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.Float64("amount.cny", amountCNY),
	)

	tx, err := s.wallets.Credit(ctx, accountID, amountCNY,
		entity.TxTypeTopup,
		fmt.Sprintf("充值 %.2f CNY", amountCNY),
		"payment_order", orderNo, "")
	if err != nil {
		slog.Error("wallet/topup: credit failed", "account_id", accountID, "amount_cny", amountCNY, "order_no", orderNo, "err", err)
		metrics.RecordWalletOperation("topup", "error")
		return nil, err
	}
	slog.Info("wallet/topup", "account_id", accountID, "amount_cny", amountCNY, "order_no", orderNo, "balance_after", tx.BalanceAfter)
	metrics.RecordWalletOperation("topup", "success")
	metrics.RecordWalletAmount("topup", amountCNY)
	// Async-safe: VIP recalculation is idempotent
	if err := s.vip.RecalculateFromWallet(ctx, accountID); err != nil {
		slog.Warn("vip recalculation failed", "account_id", accountID, "err", err)
	}
	return tx, nil
}

// Credit adds a balance to the wallet (admin adjustments, bonuses, etc.).
func (s *WalletService) Credit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.credit")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.Float64("amount.cny", amount),
		attribute.String("tx.type", txType),
	)

	tx, err := s.wallets.Credit(ctx, accountID, amount, txType, desc, refType, refID, productID)
	if err != nil {
		slog.Error("wallet/credit: failed", "account_id", accountID, "amount", amount, "tx_type", txType, "ref_id", refID, "err", err)
		metrics.RecordWalletOperation("credit", "error")
		return nil, err
	}
	slog.Info("wallet/credit", "account_id", accountID, "amount", amount, "tx_type", txType, "ref_id", refID, "balance_after", tx.BalanceAfter)
	metrics.RecordWalletOperation("credit", "success")
	metrics.RecordWalletAmount("credit", amount)
	return tx, nil
}

// Debit charges the wallet for a product purchase or subscription.
// Parameter order matches walletStore: txType, desc, refType, refID, productID.
func (s *WalletService) Debit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.debit")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.Float64("amount.cny", amount),
		attribute.String("tx.type", txType),
		attribute.String("product.id", productID),
	)

	tx, err := s.wallets.Debit(ctx, accountID, amount, txType, desc, refType, refID, productID)
	if err != nil {
		slog.Warn("wallet/debit: failed", "account_id", accountID, "amount", amount, "tx_type", txType, "product_id", productID, "ref_id", refID, "err", err)
		metrics.RecordWalletOperation("debit", "error")
		return nil, err
	}
	slog.Info("wallet/debit", "account_id", accountID, "amount", amount, "tx_type", txType, "product_id", productID, "balance_after", tx.BalanceAfter)
	metrics.RecordWalletOperation("debit", "success")
	metrics.RecordWalletAmount("debit", amount)
	return tx, nil
}

// UpdatePaymentOrder persists changes to a payment order (e.g. storing the external ID).
func (s *WalletService) UpdatePaymentOrder(ctx context.Context, o *entity.PaymentOrder) error {
	return s.wallets.UpdatePaymentOrder(ctx, o)
}

// Redeem validates and applies a redemption code atomically (TOCTOU safe).
func (s *WalletService) Redeem(ctx context.Context, accountID int64, code string) error {
	_, err := s.wallets.RedeemCode(ctx, accountID, strings.ToUpper(strings.TrimSpace(code)))
	return err
}

// ListTransactions returns paginated wallet transactions.
func (s *WalletService) ListTransactions(ctx context.Context, accountID int64, page, pageSize int) ([]entity.WalletTransaction, int64, error) {
	return s.wallets.ListTransactions(ctx, accountID, page, pageSize)
}

// CreatePaymentOrder inserts a new pending order and returns it.
func (s *WalletService) CreatePaymentOrder(ctx context.Context, o *entity.PaymentOrder) error {
	return s.wallets.CreatePaymentOrder(ctx, o)
}

// CreateSubscriptionOrder creates a pending payment order for a subscription purchase.
func (s *WalletService) CreateSubscriptionOrder(ctx context.Context, o *entity.PaymentOrder) error {
	o.OrderNo = generateOrderNo(o.AccountID)
	o.Status = entity.OrderStatusPending
	return s.wallets.CreatePaymentOrder(ctx, o)
}

// CreateTopup creates a payment order for a wallet topup and returns the order.
// The caller is responsible for redirecting the user to the returned payURL.
func (s *WalletService) CreateTopup(ctx context.Context, accountID int64, amountCNY float64, method string) (*entity.PaymentOrder, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.create_topup")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.Float64("amount.cny", amountCNY),
		attribute.String("payment.method", method),
	)

	if amountCNY <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	o := &entity.PaymentOrder{
		AccountID:     accountID,
		OrderNo:       generateOrderNo(accountID),
		OrderType:     "topup",
		AmountCNY:     amountCNY,
		Currency:      "CNY",
		PaymentMethod: method,
		Status:        entity.OrderStatusPending,
	}
	if err := s.wallets.CreatePaymentOrder(ctx, o); err != nil {
		return nil, fmt.Errorf("create payment order: %w", err)
	}
	return o, nil
}

// ListOrders returns paginated payment orders for an account.
func (s *WalletService) ListOrders(ctx context.Context, accountID int64, page, pageSize int) ([]entity.PaymentOrder, int64, error) {
	return s.wallets.ListOrders(ctx, accountID, page, pageSize)
}

// GetOrderByNo returns a specific payment order, validating ownership.
func (s *WalletService) GetOrderByNo(ctx context.Context, accountID int64, orderNo string) (*entity.PaymentOrder, error) {
	o, err := s.wallets.GetPaymentOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("order %s not found", orderNo)
	}
	if o.AccountID != accountID {
		return nil, fmt.Errorf("order %s not found", orderNo) // obscure to prevent enumeration
	}
	return o, nil
}

// BillingSummary holds aggregated billing information for an account.
type BillingSummary struct {
	Balance        float64 `json:"balance"`
	Frozen         float64 `json:"frozen"`
	Available      float64 `json:"available"`
	LifetimeTopup  float64 `json:"lifetime_topup"`
	LifetimeSpend  float64 `json:"lifetime_spend"`
	ActivePreAuths int64   `json:"active_pre_auths"`
	PendingOrders  int64   `json:"pending_orders"`
}

// GetBillingSummary returns an aggregated billing overview for an account.
func (s *WalletService) GetBillingSummary(ctx context.Context, accountID int64) (*BillingSummary, error) {
	w, err := s.wallets.GetByAccountID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	if w == nil {
		// Account has no wallet yet — return zeroed summary.
		return &BillingSummary{}, nil
	}

	preAuths, err := s.wallets.CountActivePreAuths(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("count active pre-auths: %w", err)
	}

	pendingOrders, err := s.wallets.CountPendingOrders(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("count pending orders: %w", err)
	}

	return &BillingSummary{
		Balance:        w.Balance,
		Frozen:         w.Frozen,
		Available:      w.Balance - w.Frozen,
		LifetimeTopup:  w.LifetimeTopup,
		LifetimeSpend:  w.LifetimeSpend,
		ActivePreAuths: preAuths,
		PendingOrders:  pendingOrders,
	}, nil
}

// ExpireStalePendingOrders marks pending orders older than maxAge as expired.
// Returns the number of orders expired.
func (s *WalletService) ExpireStalePendingOrders(ctx context.Context, maxAge time.Duration) (int64, error) {
	return s.wallets.ExpireStalePendingOrders(ctx, maxAge)
}

// ExpireStalePreAuths marks active pre-authorizations past their deadline as expired.
func (s *WalletService) ExpireStalePreAuths(ctx context.Context) (int64, error) {
	return s.wallets.ExpireStalePreAuths(ctx)
}

// --- Reconciliation pass-through ---

// FindPaidTopupOrdersWithoutCredit returns paid topup orders missing a wallet credit.
func (s *WalletService) FindPaidTopupOrdersWithoutCredit(ctx context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	return s.wallets.FindPaidTopupOrdersWithoutCredit(ctx)
}

// CreateReconciliationIssue persists a reconciliation issue (deduplicates by order_no + type).
func (s *WalletService) CreateReconciliationIssue(ctx context.Context, issue *entity.ReconciliationIssue) error {
	return s.wallets.CreateReconciliationIssue(ctx, issue)
}

// ListReconciliationIssues returns paginated reconciliation issues.
func (s *WalletService) ListReconciliationIssues(ctx context.Context, status string, page, pageSize int) ([]entity.ReconciliationIssue, int64, error) {
	return s.wallets.ListReconciliationIssues(ctx, status, page, pageSize)
}

// ResolveReconciliationIssue marks an issue as resolved or ignored.
func (s *WalletService) ResolveReconciliationIssue(ctx context.Context, id int64, status, resolution string) error {
	return s.wallets.ResolveReconciliationIssue(ctx, id, status, resolution)
}

// CreateCheckoutSession creates a checkout order initiated by an external product service.
// If idempotencyKey is provided and a pending order exists with that key, returns it.
func (s *WalletService) CreateCheckoutSession(ctx context.Context, accountID int64, amountCNY float64, method, sourceService, idempotencyKey string, ttl time.Duration) (*entity.PaymentOrder, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.create_checkout_session")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.Float64("amount.cny", amountCNY),
		attribute.String("payment.method", method),
		attribute.String("source.service", sourceService),
	)

	if amountCNY <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}

	// Idempotency: if key provided, check for existing pending order.
	if idempotencyKey != "" {
		existing, err := s.wallets.GetPendingOrderByIdempotencyKey(ctx, idempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("idempotency check: %w", err)
		}
		if existing != nil {
			slog.Info("wallet/checkout: returning existing order for idempotency key",
				"order_no", existing.OrderNo, "idempotency_key", idempotencyKey)
			return existing, nil
		}
	}

	if ttl == 0 {
		ttl = 30 * time.Minute
	}
	expiresAt := time.Now().UTC().Add(ttl)

	o := &entity.PaymentOrder{
		AccountID:      accountID,
		OrderNo:        generateOrderNo(accountID),
		OrderType:      "topup",
		AmountCNY:      amountCNY,
		Currency:       "CNY",
		PaymentMethod:  method,
		Status:         entity.OrderStatusPending,
		SourceService:  sourceService,
		IdempotencyKey: idempotencyKey,
		ExpiresAt:      &expiresAt,
	}
	if err := s.wallets.CreatePaymentOrder(ctx, o); err != nil {
		return nil, fmt.Errorf("create payment order: %w", err)
	}
	slog.Info("wallet/checkout: order created",
		"order_no", o.OrderNo, "account_id", accountID, "amount_cny", amountCNY,
		"method", method, "source", sourceService, "expires_at", expiresAt)
	return o, nil
}

// GetCheckoutStatus returns the current status of a checkout order.
// Does NOT require account_id ownership check — internal API trusts the caller.
func (s *WalletService) GetCheckoutStatus(ctx context.Context, orderNo string) (*entity.PaymentOrder, error) {
	o, err := s.wallets.GetPaymentOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("order %s not found", orderNo)
	}
	return o, nil
}

// PreAuthorize freezes an amount on the wallet for later settlement.
func (s *WalletService) PreAuthorize(ctx context.Context, accountID int64, amount float64, productID, referenceID, description string, ttl time.Duration) (*entity.WalletPreAuthorization, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.pre_authorize")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("account.id", accountID),
		attribute.Float64("amount", amount),
		attribute.String("product.id", productID),
	)

	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}
	if ttl == 0 {
		ttl = 10 * time.Minute
	}

	pa := &entity.WalletPreAuthorization{
		AccountID:   accountID,
		Amount:      amount,
		ProductID:   productID,
		ReferenceID: referenceID,
		Description: description,
		ExpiresAt:   time.Now().UTC().Add(ttl),
	}
	if err := s.wallets.CreatePreAuth(ctx, pa); err != nil {
		slog.Warn("wallet/pre-authorize: failed", "account_id", accountID, "amount", amount, "product_id", productID, "err", err)
		return nil, err
	}
	slog.Info("wallet/pre-authorize", "id", pa.ID, "account_id", accountID, "amount", amount, "product_id", productID, "expires_at", pa.ExpiresAt)
	metrics.RecordWalletOperation("pre_authorize", "success")
	return pa, nil
}

// SettlePreAuth charges the actual amount and releases the hold.
func (s *WalletService) SettlePreAuth(ctx context.Context, preAuthID int64, actualAmount float64) (*entity.WalletPreAuthorization, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.settle_pre_auth")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("preauth.id", preAuthID),
		attribute.Float64("actual_amount", actualAmount),
	)

	if actualAmount < 0 {
		return nil, fmt.Errorf("actual amount must be non-negative")
	}
	pa, err := s.wallets.SettlePreAuth(ctx, preAuthID, actualAmount)
	if err != nil {
		slog.Warn("wallet/settle-pre-auth: failed", "preauth_id", preAuthID, "actual_amount", actualAmount, "err", err)
		return nil, err
	}
	slog.Info("wallet/settle-pre-auth", "id", pa.ID, "account_id", pa.AccountID, "held", pa.Amount, "actual", actualAmount)
	metrics.RecordWalletOperation("settle_pre_auth", "success")
	metrics.RecordWalletAmount("settle_pre_auth", actualAmount)
	return pa, nil
}

// ReleasePreAuth cancels a pre-auth hold, unfreezing the amount.
func (s *WalletService) ReleasePreAuth(ctx context.Context, preAuthID int64) (*entity.WalletPreAuthorization, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.release_pre_auth")
	defer span.End()
	span.SetAttributes(attribute.Int64("preauth.id", preAuthID))

	pa, err := s.wallets.ReleasePreAuth(ctx, preAuthID)
	if err != nil {
		slog.Warn("wallet/release-pre-auth: failed", "preauth_id", preAuthID, "err", err)
		return nil, err
	}
	slog.Info("wallet/release-pre-auth", "id", pa.ID, "account_id", pa.AccountID, "amount", pa.Amount)
	metrics.RecordWalletOperation("release_pre_auth", "success")
	return pa, nil
}

// MarkOrderPaid atomically marks an order as paid and credits the wallet.
// Uses conditional UPDATE to prevent double-charge on concurrent webhook delivery.
func (s *WalletService) MarkOrderPaid(ctx context.Context, orderNo string) (*entity.PaymentOrder, error) {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "wallet.mark_order_paid")
	defer span.End()
	span.SetAttributes(attribute.String("order.no", orderNo))

	o, didTransition, err := s.wallets.MarkPaymentOrderPaid(ctx, orderNo)
	if err != nil {
		slog.Error("wallet/mark-order-paid: failed", "order_no", orderNo, "err", err)
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("order %s not found", orderNo)
	}
	slog.Info("wallet/mark-order-paid", "order_no", orderNo, "account_id", o.AccountID, "order_type", o.OrderType, "amount_cny", o.AmountCNY, "did_transition", didTransition)
	if didTransition {
		metrics.RecordPaymentOrderTransition("pending", "paid", o.OrderType, o.PaymentMethod)
	}
	// Only credit wallet when this call actually flipped pending→paid.
	if didTransition && o.OrderType == "topup" {
		if _, err := s.Topup(ctx, o.AccountID, o.AmountCNY, o.OrderNo); err != nil {
			return nil, fmt.Errorf("credit wallet: %w", err)
		}
	}
	return o, nil
}
