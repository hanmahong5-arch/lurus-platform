package app

import (
	"context"
	"log/slog"
	"time"
)

// Default reconciliation intervals and thresholds.
const (
	defaultReconciliationInterval = 5 * time.Minute
	defaultOrderMaxAge            = 30 * time.Minute
)

// ReconciliationWorker periodically cleans up stale payment orders and
// expired pre-authorizations. It complements the Temporal ExpiryScanner
// (which runs hourly) by providing faster, goroutine-based cleanup on a
// 5-minute cadence.
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

// tick performs a single reconciliation pass: expire stale orders, then expire
// stale pre-authorizations. Each step is independent — a failure in one does
// not block the other.
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
}
