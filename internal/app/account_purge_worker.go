package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// AccountPurgePublisher is the narrow event-publish surface used by
// the worker after a cascade success — kept as a local interface so
// the worker file stays free of the concrete *outbox.DLQPublisher
// type and tests can inject a capture/error fake. Mirrors the shape
// of RefundPublisher in refund_service.go.
type AccountPurgePublisher interface {
	Publish(ctx context.Context, ev *event.IdentityEvent) error
}

// purgeEmitTimeout bounds the best-effort emit of identity.account.deleted
// after MarkCompleted. The publish is a notification, not a transaction;
// a hung NATS broker must not pin the worker tick long enough to delay
// the next claimed row.
const purgeEmitTimeout = 5 * time.Second

// Default tuning for AccountPurgeWorker. Operators override via env
// (CRON_PURGE_INTERVAL, CRON_PURGE_BATCH).
const (
	// DefaultAccountPurgeInterval is how often the worker scans for
	// expired pending delete requests. One hour matches the natural
	// clock granularity of "30-day cooling off" and keeps load on the
	// shared identity database minimal — a delayed cascade by up to
	// 1 hour is well inside the user expectation set by PIPL §47's
	// "without undue delay" clause.
	DefaultAccountPurgeInterval = 1 * time.Hour
	// DefaultAccountPurgeBatch caps the per-tick batch so a backlog
	// (e.g. after a long worker outage) doesn't pin the cascade
	// downstream services for an unbounded period. 20 rows per hour
	// is roughly two orders of magnitude above realistic steady-state
	// volume for the account-delete pipeline.
	DefaultAccountPurgeBatch = 20
	// purgeCascadeTimeout bounds a single per-row cascade. The cascade
	// itself runs subscription cancel + wallet debit + Zitadel deactivate;
	// 60s is generous — most steps return well under 5s. Hitting this
	// timeout will land the row in 'expired' for human review.
	purgeCascadeTimeout = 60 * time.Second
)

// AccountPurgeStore is the persistence surface AccountPurgeWorker
// depends on. Implemented by *repo.AccountDeleteRequestRepo. Declared
// here so unit tests can pass an in-memory fake without standing up
// gorm + Postgres.
//
// The interface is deliberately narrow: only the four methods the
// worker actually calls. Any additional repo methods stay invisible.
type AccountPurgeStore interface {
	ClaimExpiredPending(ctx context.Context, limit int) ([]*entity.AccountDeleteRequest, error)
	MarkCompleted(ctx context.Context, id int64, completedAt time.Time) error
	MarkExpired(ctx context.Context, id int64, completedAt time.Time) error
}

// AccountPurgeCascade is the side-effect surface invoked per claimed
// row. Production passes a thin adapter around handler.AccountDeleteExecutor's
// ExecuteDelegate so the worker stays free of any handler types.
//
// The worker calls this with a long-running context (per-row timeout
// applied internally) and the synthetic caller id "0" because the
// cron has no human approver — audit logging downstream uses the
// row's RequestedBy as the user-attribution field.
type AccountPurgeCascade interface {
	PurgeAccount(ctx context.Context, accountID int64) error
}

// AccountPurgeWorker scans the account_delete_requests table for
// rows whose cooling-off window has elapsed and dispatches the
// existing GDPR cascade (handler.AccountDeleteExecutor) under a
// "approved by self / cron" audit attribution.
//
// Multi-replica safety: every scan claims rows via UPDATE...RETURNING
// with FOR UPDATE SKIP LOCKED in the underlying store, so two
// concurrent workers never both win the same row. A worker that
// crashed mid-cascade leaves the row in 'processing'; the row will
// never be re-claimed (the WHERE filter is status='pending') —
// instead a one-time operator query identifies stuck rows for manual
// resolution. We trade automated retry for safety: re-running the
// cascade against a partially-cleaned account risks double-debit on
// the wallet and re-deactivating Zitadel, which would mask the
// original failure rather than expose it.
//
// Disabled by default (Enabled=false). Production turns it on via
// CRON_PURGE_ENABLED=true after the first deployment window so the
// rollout can be observed without flipping behavior.
type AccountPurgeWorker struct {
	store     AccountPurgeStore
	cascade   AccountPurgeCascade
	publisher AccountPurgePublisher // optional; nil = no PIPL §47 emit
	interval  time.Duration
	batch     int
	enabled   bool
}

// AccountPurgeWorkerConfig is the wiring shape. Zero-valued fields fall
// back to the package defaults; this lets the boot path keep its
// `New(...)` signature stable while still allowing test overrides.
type AccountPurgeWorkerConfig struct {
	Interval time.Duration
	Batch    int
	Enabled  bool
}

// NewAccountPurgeWorker wires the worker. Both store and cascade are
// required; passing nil panics on first scan rather than silently
// no-oping.
func NewAccountPurgeWorker(store AccountPurgeStore, cascade AccountPurgeCascade, cfg AccountPurgeWorkerConfig) *AccountPurgeWorker {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultAccountPurgeInterval
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultAccountPurgeBatch
	}
	return &AccountPurgeWorker{
		store:    store,
		cascade:  cascade,
		interval: cfg.Interval,
		batch:    cfg.Batch,
		enabled:  cfg.Enabled,
	}
}

// WithPublisher attaches an event publisher used to emit
// identity.account.deleted after a successful cascade + MarkCompleted.
// Nil-safe: passing nil simply skips the emit branch, which keeps the
// boot path resilient when NATS wiring is not yet in place. Returns
// the worker for chaining.
func (w *AccountPurgeWorker) WithPublisher(p AccountPurgePublisher) *AccountPurgeWorker {
	w.publisher = p
	return w
}

// Name implements lifecycle.Task.
func (w *AccountPurgeWorker) Name() string { return "account_purge_worker" }

// Run is the lifecycle entry point. Returns when ctx is cancelled.
// When disabled, logs once and returns immediately so a missing
// Enabled flag does not block the lifecycle manager's wait.
func (w *AccountPurgeWorker) Run(ctx context.Context) error {
	if !w.enabled {
		slog.Info("account_purge_worker.disabled",
			"reason", "CRON_PURGE_ENABLED is false")
		return nil
	}
	if w.store == nil || w.cascade == nil {
		return errors.New("account_purge_worker: store or cascade not wired")
	}

	slog.Info("account_purge_worker.started",
		"interval", w.interval.String(),
		"batch", w.batch)

	// Run an initial tick immediately so a freshly-deployed worker
	// drains any backlog without waiting a full interval. Subsequent
	// ticks are interval-spaced.
	w.tick(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("account_purge_worker.stopped")
			return nil
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick performs one scan + cascade pass. Each row is processed
// independently — a failure on row N does not abort the loop, so
// a single misbehaving row cannot starve the rest of the batch.
func (w *AccountPurgeWorker) tick(ctx context.Context) {
	rows, err := w.store.ClaimExpiredPending(ctx, w.batch)
	if err != nil {
		slog.Error("account_purge_worker.claim_failed", "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	slog.Info("account_purge_worker.claimed", "count", len(rows))
	for _, row := range rows {
		w.processRow(ctx, row)
	}
}

// processRow runs the cascade for a single claimed row and records
// the outcome. Errors do not propagate — they're logged and persisted
// as 'expired' so the row exits the pending pool.
func (w *AccountPurgeWorker) processRow(ctx context.Context, row *entity.AccountDeleteRequest) {
	cascadeCtx, cancel := context.WithTimeout(ctx, purgeCascadeTimeout)
	defer cancel()

	cascadeErr := w.cascade.PurgeAccount(cascadeCtx, row.AccountID)
	now := time.Now().UTC()

	if cascadeErr == nil {
		if err := w.store.MarkCompleted(ctx, row.ID, now); err != nil {
			// Row stays in 'processing' — the cascade succeeded but
			// we lost the bookkeeping write. Operators can detect this
			// with a "rows in processing for > 1h" query and patch
			// manually. We deliberately don't auto-flip to 'expired'
			// here because the cascade succeeded.
			slog.Error("account_purge_worker.mark_completed_failed",
				"request_id", row.ID,
				"account_id", row.AccountID,
				"err", err)
			return
		}
		slog.Info("account_purge_worker.completed",
			"request_id", row.ID,
			"account_id", row.AccountID,
			"requested_at", row.RequestedAt.Format(time.RFC3339),
			"outcome", "completed")
		w.emitAccountDeleted(ctx, row, now)
		return
	}

	// Cascade failed — record terminal 'expired' state. No retry.
	if err := w.store.MarkExpired(ctx, row.ID, now); err != nil {
		slog.Error("account_purge_worker.mark_expired_failed",
			"request_id", row.ID,
			"account_id", row.AccountID,
			"cascade_err", cascadeErr,
			"err", err)
		return
	}
	slog.Warn("account_purge_worker.expired",
		"request_id", row.ID,
		"account_id", row.AccountID,
		"requested_at", row.RequestedAt.Format(time.RFC3339),
		"outcome", "expired",
		"cascade_err", fmt.Sprintf("%v", cascadeErr))
}

// emitAccountDeleted publishes identity.account.deleted as a
// best-effort PIPL §47 cascade signal to downstream services
// (newapi / memorus / lucrum / tally). Failure to publish does NOT
// undo the purge — purge already succeeded; this is a notification,
// not a transaction. NATS delivery is at-least-once, so consumers
// MUST be idempotent.
//
// LurusID is derived deterministically from AccountID via
// entity.GenerateLurusID, so the worker does not need to round-trip
// through the accounts table just to populate the envelope. The
// payload is deliberately minimal — the whole point of the event
// is to trigger downstream deletion of personal data, so including
// any here would just leak it again into NATS audit logs.
func (w *AccountPurgeWorker) emitAccountDeleted(parent context.Context, row *entity.AccountDeleteRequest, purgedAt time.Time) {
	if w.publisher == nil {
		return
	}
	payload := event.AccountDeletedPayload{
		PurgedAt: purgedAt.Format(time.RFC3339),
	}
	ev, err := event.NewEvent(
		event.SubjectAccountDeleted,
		row.AccountID,
		entity.GenerateLurusID(row.AccountID),
		"", // no product scope — cascade spans all products
		payload,
	)
	if err != nil {
		slog.Warn("account_purge_worker.emit_build_failed",
			"request_id", row.ID,
			"account_id", row.AccountID,
			"err", err)
		return
	}
	pubCtx, cancel := context.WithTimeout(parent, purgeEmitTimeout)
	defer cancel()
	if perr := w.publisher.Publish(pubCtx, ev); perr != nil {
		slog.Warn("account_purge_worker.emit_failed",
			"request_id", row.ID,
			"account_id", row.AccountID,
			"subject", event.SubjectAccountDeleted,
			"err", perr)
		return
	}
	slog.Info("account_purge_worker.emitted",
		"request_id", row.ID,
		"account_id", row.AccountID,
		"subject", event.SubjectAccountDeleted)
}
