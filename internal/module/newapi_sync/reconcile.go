package newapi_sync

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// Reconcile cron — backfill orphan accounts that missed the
// OnAccountCreated hook (NewAPI down at signup, hook crashed mid-flight,
// pod killed after DB row but before NewAPI call, etc.).
//
// Failure modes the cron protects against:
//
//   - 4c (OnAccountCreated) NewAPI HTTP failure → account.NewAPIUserID
//     stays NULL forever; user can never use LLM features.
//   - 4c partial success: NewAPI user created but SetNewAPIUserID DB
//     write lost (DB blip) → orphan NewAPI user; account still NULL.
//     Cron's find-then-create idempotency in OnAccountCreated reuses the
//     existing NewAPI user instead of creating duplicates.
//
// Frequency: every 5 minutes by default (DefaultReconcileInterval).
// Batch size: 100 per tick (configurable). Both chosen to clear a
// reasonable backlog without hammering NewAPI in a tight loop. With
// 1k orphans → cleared in ~50 minutes; alarming long before that on
// the metric (sustained orphan count > 0 = signal).

// DefaultReconcileInterval bounds the wallclock between cron ticks.
// Picked so a typical orphan is healed within 5 minutes — long enough
// that a brief NewAPI hiccup self-recovers between ticks; short enough
// that an LLM-needing user notices little delay.
const DefaultReconcileInterval = 5 * time.Minute

// DefaultReconcileBatch caps how many orphans we process per tick.
// 100 limits both DB load (one indexed query per tick) and NewAPI load
// (≤200 admin calls — find + create — per tick). Operators can tune
// via WithReconcileBatch when growth demands.
const DefaultReconcileBatch = 100

// ReconcileResult is the outcome of a single ReconcileTick call.
//
// Surfaced (a) so the cron loop can log a one-line summary per tick,
// (b) so tests can assert exact counts. Operators query the metric
// `newapi_sync_ops_total{op="account_provisioned",result=...}` for
// continuous data; this struct is for the per-tick log line.
type ReconcileResult struct {
	Scanned   int   // total orphan rows pulled this tick
	Healed    int   // OnAccountCreated returned nil → mapping persisted
	Failed    int   // OnAccountCreated returned non-nil; will retry next tick
	ListError error // top-level DB error fetching the batch (nothing scanned)
}

// ReconcileTick runs ONE backfill pass. Synchronous + bounded; safe to
// call from a manual /admin endpoint or from the periodic loop.
//
// Idempotency comes "for free" from OnAccountCreated's find-then-create
// pattern (4c). Repeated ticks against the same orphan converge cheaply:
// after one successful tick, ListWithoutNewAPIUser stops returning that
// row.
//
// Edge cases covered:
//
//   - List returns []: noop, no error. Healthy steady state.
//   - List error: returned in result.ListError; metric unhandled (the
//     subsequent ticks will probe DB again). Logged at WARN.
//   - Per-account OnAccountCreated error: counted, logged at WARN, NOT
//     fatal — keep processing the rest of the batch so a single
//     poison-pill account can't stall reconciliation forever.
//   - ctx cancellation: short-circuits between accounts so shutdown is
//     bounded by one OnAccountCreated call (≤ a few seconds).
func (m *Module) ReconcileTick(ctx context.Context, batch int) ReconcileResult {
	if m == nil || m.client == nil || m.accounts == nil {
		// Module disabled (env unset). Nothing to do; treat as a successful
		// no-op so the periodic loop doesn't spam errors.
		return ReconcileResult{}
	}
	if batch <= 0 {
		batch = DefaultReconcileBatch
	}

	orphans, err := m.accounts.ListWithoutNewAPIUser(ctx, batch)
	if err != nil {
		metrics.RecordNewAPISyncOp(opReconcileTick, resultError)
		slog.WarnContext(ctx, "newapi_sync: reconcile list failed",
			"err", err)
		return ReconcileResult{ListError: err}
	}

	res := ReconcileResult{Scanned: len(orphans)}
	for _, a := range orphans {
		if err := ctx.Err(); err != nil {
			// Caller cancelled — stop walking the batch but report what
			// we've done so the log line stays accurate.
			break
		}
		if hookErr := m.OnAccountCreated(ctx, a); hookErr != nil {
			res.Failed++
			slog.WarnContext(ctx, "newapi_sync: reconcile heal failed",
				"account_id", a.ID, "err", hookErr)
			continue
		}
		res.Healed++
	}

	// Single tick-level metric so operators can chart "how often does
	// reconcile actually find anything to do" — sustained scanned>0
	// signals 4c hooks are flaky; sustained scanned==0 is steady state.
	switch {
	case res.Healed > 0 && res.Failed == 0:
		metrics.RecordNewAPISyncOp(opReconcileTick, resultSuccess)
	case res.Failed > 0:
		metrics.RecordNewAPISyncOp(opReconcileTick, resultError)
	default:
		// Scanned == 0 OR all skipped (no orphans need processing).
		metrics.RecordNewAPISyncOp(opReconcileTick, resultSkipped)
	}

	if res.Scanned > 0 {
		slog.InfoContext(ctx, "newapi_sync: reconcile tick complete",
			"scanned", res.Scanned, "healed", res.Healed, "failed", res.Failed)
	}
	return res
}

// RunReconcileLoop runs ReconcileTick on a fixed cadence until ctx is
// cancelled. Intended to be launched as a goroutine from main.go's
// errgroup. Idempotent stop on ctx.Done.
//
// Tick interval defaults to DefaultReconcileInterval when 0 is passed.
// First tick fires AFTER the first interval — gives the rest of boot
// time to settle before adding NewAPI load. To run an immediate tick at
// boot, call ReconcileTick directly before launching the loop.
func (m *Module) RunReconcileLoop(ctx context.Context, interval time.Duration, batch int) error {
	if m == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	if interval <= 0 {
		interval = DefaultReconcileInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	slog.InfoContext(ctx, "newapi_sync: reconcile loop started",
		"interval", interval, "batch", batch)

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "newapi_sync: reconcile loop stopped",
				"reason", ctx.Err())
			// Treat shutdown as success — outer errgroup uses this
			// return to decide whether to abort sibling goroutines.
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		case <-t.C:
			_ = m.ReconcileTick(ctx, batch)
		}
	}
}

// op label value reserved for the reconcile tick — distinct from
// account_provisioned so dashboards can chart cron activity vs realtime
// hook activity.
const opReconcileTick = "reconcile_tick"
