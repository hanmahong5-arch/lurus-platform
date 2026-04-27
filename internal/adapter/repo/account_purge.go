package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// AccountPurgeRepo persists purge audit rows.
type AccountPurgeRepo struct {
	db *gorm.DB
}

func NewAccountPurgeRepo(db *gorm.DB) *AccountPurgeRepo { return &AccountPurgeRepo{db: db} }

// ErrPurgeInFlight is returned by BeginPurge when another purge for the
// same account is already in-flight (status='purging'). Callers map
// this to HTTP 409 Conflict so the user sees a clear "another admin is
// already purging this account" message rather than a generic 500.
var ErrPurgeInFlight = errors.New("repo: account purge already in flight")

// BeginPurge inserts a new audit row in 'purging' state and returns
// it. Concurrent BeginPurge calls for the same account fail-fast on
// the partial UNIQUE index from migration 024 — the second caller
// gets ErrPurgeInFlight rather than blocking on a row lock.
func (r *AccountPurgeRepo) BeginPurge(ctx context.Context, p *entity.AccountPurge) error {
	if p.Status == "" {
		p.Status = entity.AccountPurgeStatusInflight
	}
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		// Postgres unique-violation surfaces as a duplicate-key string
		// in the wrapped error from gorm + pgx. Match conservatively —
		// any unique violation on this table can only originate from
		// the one in-flight partial index, since id is BIGSERIAL.
		if isUniqueViolation(err) {
			return ErrPurgeInFlight
		}
		return err
	}
	return nil
}

// MarkCompleted flips a row to 'completed' and stamps completed_at.
// Idempotent on already-completed rows (no-op if status already
// matches). Does NOT validate the source state — the executor is
// trusted to call this only after the cascade succeeded end-to-end.
func (r *AccountPurgeRepo) MarkCompleted(ctx context.Context, purgeID int64, approvedBy int64, completedAt time.Time) error {
	updates := map[string]any{
		"status":       entity.AccountPurgeStatusCompleted,
		"approved_by":  approvedBy,
		"completed_at": completedAt,
	}
	return r.db.WithContext(ctx).Model(&entity.AccountPurge{}).
		Where("id = ?", purgeID).Updates(updates).Error
}

// MarkFailed flips a row to 'failed' with the cascade error captured
// for audit. Trims the error string to 1 KB so a runaway stack trace
// from a misbehaving downstream cannot bloat rows.
func (r *AccountPurgeRepo) MarkFailed(ctx context.Context, purgeID int64, errMsg string, completedAt time.Time) error {
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024]
	}
	updates := map[string]any{
		"status":       entity.AccountPurgeStatusFailed,
		"error":        errMsg,
		"completed_at": completedAt,
	}
	return r.db.WithContext(ctx).Model(&entity.AccountPurge{}).
		Where("id = ?", purgeID).Updates(updates).Error
}

// GetByID returns one audit row by primary key, or (nil, nil) when not
// found. Used by tests + the audit dashboard.
func (r *AccountPurgeRepo) GetByID(ctx context.Context, id int64) (*entity.AccountPurge, error) {
	var p entity.AccountPurge
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

// ListByAccount returns audit rows newest-first. Used by the admin
// audit dashboard to render an account's purge history.
func (r *AccountPurgeRepo) ListByAccount(ctx context.Context, accountID int64, limit int) ([]entity.AccountPurge, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []entity.AccountPurge
	err := r.db.WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("started_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// isUniqueViolation matches the substring Postgres / pgx surface for
// duplicate-key errors. We intentionally avoid importing pgconn just
// for the SQLSTATE check — the substring is stable across pgx
// versions and the false-positive surface is empty (all unique
// indexes on identity.account_purges go through BeginPurge).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "SQLSTATE 23505")
}
