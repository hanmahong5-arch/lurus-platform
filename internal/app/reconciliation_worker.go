package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// Default reconciliation intervals and thresholds.
const (
	defaultReconciliationInterval = 5 * time.Minute
	defaultOrderMaxAge            = 30 * time.Minute
	// stalePendingMinAge is the minimum age of a pending order before we query
	// the provider to check if a webhook was missed. Must be long enough for
	// normal webhook delivery (60s buffer for retry).
	stalePendingMinAge = 10 * time.Minute
)

// providerNameByMethod maps payment method IDs to provider registry names.
var providerNameByMethod = map[string]string{
	"alipay": "alipay", "alipay_qr": "alipay", "alipay_wap": "alipay",
	"wechat_native": "wechat", "wechat_h5": "wechat", "wechat_jsapi": "wechat",
	"epay_alipay": "epay", "epay_wxpay": "epay", "epay_wechat": "epay",
	"stripe": "stripe",
	"creem":  "creem",
}

// ReconciliationWorker periodically cleans up stale payment orders,
// expired pre-authorizations, checks for data integrity issues,
// and verifies stale pending orders against providers to detect missed webhooks.
type ReconciliationWorker struct {
	wallets     *WalletService
	payments    *payment.Registry
	interval    time.Duration
	orderMaxAge time.Duration
}

// NewReconciliationWorker creates a worker with sensible defaults.
// payments may be nil (provider verification will be skipped).
func NewReconciliationWorker(wallets *WalletService, payments *payment.Registry) *ReconciliationWorker {
	return &ReconciliationWorker{
		wallets:     wallets,
		payments:    payments,
		interval:    defaultReconciliationInterval,
		orderMaxAge: defaultOrderMaxAge,
	}
}

// Start runs the reconciliation loop in a blocking fashion. It returns when
// ctx is cancelled, making it safe for use with errgroup.
func (w *ReconciliationWorker) Start(ctx context.Context) {
	slog.Info("reconciliation: worker started",
		"interval", w.interval.String(),
		"order_max_age", w.orderMaxAge.String(),
	)

	w.tick(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("reconciliation: worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick performs a single reconciliation pass. Each step is independent.
func (w *ReconciliationWorker) tick(ctx context.Context) {
	// Step 1: Expire stale pending payment orders.
	orderCount, err := w.wallets.ExpireStalePendingOrders(ctx, w.orderMaxAge)
	if err != nil {
		slog.Error("reconciliation: expire stale orders failed", "err", err)
	} else if orderCount > 0 {
		slog.Info("reconciliation: expired stale orders", "count", orderCount)
	}

	// Step 2: Expire stale pre-authorizations.
	paCount, err := w.wallets.ExpireStalePreAuths(ctx)
	if err != nil {
		slog.Error("reconciliation: expire stale pre-auths failed", "err", err)
	} else if paCount > 0 {
		slog.Info("reconciliation: expired stale pre-auths", "count", paCount)
	}

	// Step 3: Integrity check — paid topup orders without wallet credit.
	w.checkPaidOrdersIntegrity(ctx)

	// Step 4: Verify stale pending orders against providers (missed webhook detection).
	w.verifyStalePendingOrders(ctx)
}

// checkPaidOrdersIntegrity finds topup orders that were marked paid but
// somehow didn't get the corresponding wallet credit.
func (w *ReconciliationWorker) checkPaidOrdersIntegrity(ctx context.Context) {
	orphans, err := w.wallets.FindPaidTopupOrdersWithoutCredit(ctx)
	if err != nil {
		slog.Error("reconciliation: integrity check failed", "err", err)
		return
	}
	if len(orphans) == 0 {
		return
	}

	slog.Warn("reconciliation: paid orders without wallet credit detected", "count", len(orphans))
	metrics.RecordReconciliationIssues(len(orphans))

	for _, o := range orphans {
		amount := o.AmountCNY
		issue := &entity.ReconciliationIssue{
			IssueType:      entity.ReconIssueMissingCredit,
			Severity:       "critical",
			OrderNo:        o.OrderNo,
			AccountID:      &o.AccountID,
			Provider:       o.PaymentMethod,
			ExpectedAmount: &amount,
			Description: fmt.Sprintf(
				"Topup order %s (%.2f CNY, account %d) is marked paid but has no wallet credit. "+
					"Likely the Credit call failed after MarkOrderPaid. Manual credit required.",
				o.OrderNo, o.AmountCNY, o.AccountID),
		}
		if err := w.wallets.CreateReconciliationIssue(ctx, issue); err != nil {
			slog.Error("reconciliation: failed to create issue",
				"order_no", o.OrderNo, "err", err)
		}
	}
}

// verifyStalePendingOrders queries providers for orders that have been pending
// too long, to detect cases where the provider accepted payment but the webhook
// was never received (network failure, DNS issue, etc.). If the provider confirms
// payment, the order is automatically recovered via MarkOrderPaid.
func (w *ReconciliationWorker) verifyStalePendingOrders(ctx context.Context) {
	if w.payments == nil {
		return
	}

	stale, err := w.wallets.FindStalePendingOrders(ctx, stalePendingMinAge)
	if err != nil {
		slog.Error("reconciliation: find stale pending orders failed", "err", err)
		return
	}
	if len(stale) == 0 {
		return
	}

	slog.Info("reconciliation: verifying stale pending orders", "count", len(stale))

	var recovered int
	for _, order := range stale {
		providerName := providerNameByMethod[order.PaymentMethod]
		if providerName == "" {
			continue
		}

		result, err := w.payments.QueryOrder(ctx, providerName, order.OrderNo)
		if err != nil {
			slog.Warn("reconciliation: provider query failed",
				"order_no", order.OrderNo, "provider", providerName, "err", err)
			continue
		}
		if result == nil {
			// Provider doesn't support order queries.
			continue
		}

		if !result.Paid {
			continue
		}

		// Provider says paid but we still have it as pending — recover!
		slog.Warn("reconciliation: missed webhook detected, recovering order",
			"order_no", order.OrderNo, "provider", providerName,
			"amount_provider", result.Amount, "amount_local", order.AmountCNY)

		if _, err := w.wallets.MarkOrderPaid(ctx, order.OrderNo); err != nil {
			slog.Error("reconciliation: auto-recover MarkOrderPaid failed",
				"order_no", order.OrderNo, "err", err)
			// Create an issue so it can be manually resolved.
			amount := order.AmountCNY
			issue := &entity.ReconciliationIssue{
				IssueType:      "missed_webhook",
				Severity:       "critical",
				OrderNo:        order.OrderNo,
				AccountID:      &order.AccountID,
				Provider:       providerName,
				ExpectedAmount: &amount,
				Description: fmt.Sprintf(
					"Order %s confirmed paid by %s (%.2f) but auto-recovery failed: %v",
					order.OrderNo, providerName, result.Amount, err),
			}
			_ = w.wallets.CreateReconciliationIssue(ctx, issue)
			continue
		}
		recovered++

		// Amount mismatch check.
		if result.Amount > 0 && result.Amount != order.AmountCNY {
			provAmt := result.Amount
			localAmt := order.AmountCNY
			issue := &entity.ReconciliationIssue{
				IssueType:      entity.ReconIssueAmountMismatch,
				Severity:       "warning",
				OrderNo:        order.OrderNo,
				AccountID:      &order.AccountID,
				Provider:       providerName,
				ExpectedAmount: &localAmt,
				ActualAmount:   &provAmt,
				Description: fmt.Sprintf(
					"Order %s recovered: local amount %.2f CNY != provider amount %.2f",
					order.OrderNo, order.AmountCNY, result.Amount),
			}
			_ = w.wallets.CreateReconciliationIssue(ctx, issue)
		}
	}

	if recovered > 0 {
		slog.Info("reconciliation: recovered missed-webhook orders", "count", recovered)
		metrics.RecordReconciliationIssues(recovered)
	}
}
