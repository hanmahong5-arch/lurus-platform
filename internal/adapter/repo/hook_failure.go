package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// errMessageMaxLen caps the persisted error string. A runaway stack trace
// from a misbehaving downstream shouldn't bloat the DLQ row.
const errMessageMaxLen = 2048

// HookFailureRepo is the Postgres-backed dead-letter store for module
// hook failures. Implements module.DeadLetterStore.
type HookFailureRepo struct {
	db *gorm.DB
}

// NewHookFailureRepo wires the repo. db must already have the
// module.hook_failures table migrated (migrations/030).
func NewHookFailureRepo(db *gorm.DB) *HookFailureRepo {
	return &HookFailureRepo{db: db}
}

// Save upserts a failure row. Recurring failures for the same
// (event, hook_name, account_id) tuple bump `attempts` and
// `last_failed_at`; the original `first_failed_at` is preserved so the
// dashboard can show "broken since X".
//
// The unique-key collision pattern uses different upsert SQL depending
// on whether account_id is null — Postgres treats NULLs as distinct in
// UNIQUE constraints, so we have two partial unique indexes (see
// migrations/030) and rely on `ON CONFLICT` against each respectively.
func (r *HookFailureRepo) Save(ctx context.Context, f *entity.HookFailure) error {
	if f == nil {
		return errors.New("hook_failure: nil row")
	}
	if f.Event == "" || f.HookName == "" {
		return errors.New("hook_failure: event + hook_name required")
	}
	if len(f.Error) > errMessageMaxLen {
		f.Error = f.Error[:errMessageMaxLen]
	}
	if len(f.Payload) == 0 {
		f.Payload = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	if f.FirstFailedAt.IsZero() {
		f.FirstFailedAt = now
	}
	f.LastFailedAt = now
	if f.Attempts <= 0 {
		f.Attempts = 1
	}

	// Upsert keyed on (event, hook_name, account_id). On conflict, bump
	// attempts and refresh error+last_failed_at. We rely on the partial
	// unique indexes from migration 030 — Postgres' ON CONFLICT can
	// target either by column tuple or by index name. Targeting
	// columns is more portable, but Postgres requires the columns to
	// match an existing UNIQUE constraint or partial index; with
	// partial indexes you must use the index target syntax. We split.
	if f.AccountID != nil {
		return r.db.WithContext(ctx).Exec(`
			INSERT INTO module.hook_failures
				(event, hook_name, account_id, payload, error, attempts,
				 first_failed_at, last_failed_at, replayed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
			ON CONFLICT (event, hook_name, account_id)
				WHERE account_id IS NOT NULL
			DO UPDATE SET
				payload         = EXCLUDED.payload,
				error           = EXCLUDED.error,
				attempts        = module.hook_failures.attempts + 1,
				last_failed_at  = EXCLUDED.last_failed_at,
				replayed_at     = NULL
		`, f.Event, f.HookName, *f.AccountID, []byte(f.Payload), f.Error, f.Attempts,
			f.FirstFailedAt, f.LastFailedAt).Error
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO module.hook_failures
			(event, hook_name, account_id, payload, error, attempts,
			 first_failed_at, last_failed_at, replayed_at)
		VALUES (?, ?, NULL, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT (event, hook_name)
			WHERE account_id IS NULL
		DO UPDATE SET
			payload         = EXCLUDED.payload,
			error           = EXCLUDED.error,
			attempts        = module.hook_failures.attempts + 1,
			last_failed_at  = EXCLUDED.last_failed_at,
			replayed_at     = NULL
	`, f.Event, f.HookName, []byte(f.Payload), f.Error, f.Attempts,
		f.FirstFailedAt, f.LastFailedAt).Error
}

// List returns DLQ rows newest-first. When pendingOnly is true only rows
// with replayed_at IS NULL are returned (the operator's "still broken"
// view). Returns rows + total count for pagination.
func (r *HookFailureRepo) List(ctx context.Context, pendingOnly bool, limit, offset int) ([]entity.HookFailure, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	q := r.db.WithContext(ctx).Model(&entity.HookFailure{})
	if pendingOnly {
		q = q.Where("replayed_at IS NULL")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("hook_failure list: count: %w", err)
	}

	var rows []entity.HookFailure
	err := q.Order("last_failed_at DESC").
		Limit(limit).Offset(offset).Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("hook_failure list: scan: %w", err)
	}
	return rows, total, nil
}

// GetByID fetches one row. Returns (nil, nil) on not-found.
func (r *HookFailureRepo) GetByID(ctx context.Context, id int64) (*entity.HookFailure, error) {
	var f entity.HookFailure
	err := r.db.WithContext(ctx).First(&f, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("hook_failure get: %w", err)
	}
	return &f, nil
}

// MarkReplayed stamps replayed_at on a row. Idempotent — re-marking is a
// no-op (latest stamp wins via gorm's Updates semantics).
func (r *HookFailureRepo) MarkReplayed(ctx context.Context, id int64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&entity.HookFailure{}).
		Where("id = ?", id).
		Update("replayed_at", at.UTC()).Error
}

// PendingDepth returns the count of unresolved DLQ rows. Used by the
// `hook_dlq_pending` gauge.
func (r *HookFailureRepo) PendingDepth(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&entity.HookFailure{}).
		Where("replayed_at IS NULL").Count(&n).Error
	return n, err
}
