package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// Default reconciliation intervals and thresholds.
const (
	defaultReconciliationInterval = 5 * time.Minute
	defaultOrderMaxAge            = 30 * time.Minute
)

// ReconciliationWorker periodically cleans up stale payment orders,
// expired pre-authorizations, and checks for data integrity issues
// (paid orders with missing wallet credits).
type ReconciliationWorker struct {
	wallets     *WalletService
	interval    time.Duration
	orderMaxAge time.Duration
}

// NewReconciliationWorker creates a worker with sensible defaults:
// tick every 5 minutes, expire orders older than 30 minutes.
func NewReconciliationWorker(wallets *WalletService) *ReconciliationWorker {
	return &ReconciliationWorker{
		wallets:     wallets,
		interval:    defaultReconciliationInterval,
		orderMaxAge: defaultOrderMaxAge,
	}
}

// Start runs the reconciliation loop in a blocking fashion. It returns when
// ctx is cancelled, making it safe for use with errgroup. The first tick fires
// immediately so stale data is cleaned up on startup.
func (w *ReconciliationWorker) Start(ctx context.Context) {
	slog.Info("reconciliation: worker started",
		"interval", w.interval.String(),
		"order_max_age", w.orderMaxAge.String(),
	)

	// Run once immediately on startup.
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

// tick performs a single reconciliation pass. Each step is independent —
// a failure in one does not block the others.
func (w *ReconciliationWorker) tick(ctx context.Context) {
	// Step 1: Expire stale pending payment orders.
	orderCount, err := w.wallets.ExpireStalePendingOrders(ctx, w.orderMaxAge)
	if err != nil {
		slog.Error("reconciliation: expire stale orders failed", "err", err)
	} else if orderCount > 0 {
		slog.Info("reconciliation: expired stale orders", "count", orderCount)
	}

	// Step 2: Expire stale pre-authorizations (uses their own expires_at column).
	paCount, err := w.wallets.ExpireStalePreAuths(ctx)
	if err != nil {
		slog.Error("reconciliation: expire stale pre-auths failed", "err", err)
	} else if paCount > 0 {
		slog.Info("reconciliation: expired stale pre-auths", "count", paCount)
	}

	// Step 3: Integrity check — paid topup orders without a wallet credit.
	w.checkPaidOrdersIntegrity(ctx)
}

// checkPaidOrdersIntegrity finds topup orders that were marked paid but
// somehow didn't get the corresponding wallet credit (e.g. crash between
// MarkOrderPaid and Credit, or a partial failure). Creates reconciliation
// issues for each so they can be manually resolved.
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
				"Topup order %s (%.2f CNY, account %d) is marked paid but has no wallet credit transaction. "+
					"This likely means the payment webhook succeeded but the Credit call failed. Manual credit required.",
				o.OrderNo, o.AmountCNY, o.AccountID),
		}
		if err := w.wallets.CreateReconciliationIssue(ctx, issue); err != nil {
			slog.Error("reconciliation: failed to create issue",
				"order_no", o.OrderNo, "err", err)
		}
	}
}
