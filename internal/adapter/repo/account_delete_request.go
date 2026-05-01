package repo

import (
	"context"
	"errors"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// AccountDeleteRequestRepo persists user-self delete requests.
//
// The flow is much simpler than AccountPurgeRepo because the cascade
// runs out-of-band (cron worker, Sprint 1B). This repo only handles
// the registration + idempotency lookup that the user-facing handler
// needs.
type AccountDeleteRequestRepo struct {
	db *gorm.DB
}

// NewAccountDeleteRequestRepo wires the repo. Caller passes the same
// *gorm.DB used by every other identity-schema repo.
func NewAccountDeleteRequestRepo(db *gorm.DB) *AccountDeleteRequestRepo {
	return &AccountDeleteRequestRepo{db: db}
}

// ErrDeleteRequestPending is returned by Create when a pending request
// already exists for the same account. Callers translate this to an
// idempotent 200 with the existing request payload, NOT a 409 — the
// destructive intent already holds, the user just tapped twice.
var ErrDeleteRequestPending = errors.New("repo: account delete request already pending")

// Create inserts a new pending delete request. The partial UNIQUE
// index from migration 028 serialises concurrent submissions for the
// same account: the second caller gets ErrDeleteRequestPending and the
// handler turns that into the idempotent shape.
//
// The caller is expected to have populated AccountID, RequestedBy,
// Reason, ReasonText, and CoolingOffUntil. Status defaults to 'pending'
// when blank.
func (r *AccountDeleteRequestRepo) Create(ctx context.Context, req *entity.AccountDeleteRequest) error {
	if req.Status == "" {
		req.Status = entity.AccountDeleteRequestStatusPending
	}
	if err := r.db.WithContext(ctx).Create(req).Error; err != nil {
		// The only UNIQUE on this table is the partial pending index, so
		// any 23505 here means "user already has a pending request" —
		// see isUniqueViolation in account_purge.go for the detection
		// rationale.
		if isUniqueViolation(err) {
			return ErrDeleteRequestPending
		}
		return err
	}
	return nil
}

// GetPending returns the single pending delete request for an account,
// or (nil, nil) when none exists. Used by the handler to surface the
// existing request id on idempotent re-submit.
func (r *AccountDeleteRequestRepo) GetPending(ctx context.Context, accountID int64) (*entity.AccountDeleteRequest, error) {
	var row entity.AccountDeleteRequest
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND status = ?", accountID, entity.AccountDeleteRequestStatusPending).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// MarkCancelled flips a pending row to 'cancelled' and stamps
// cancelled_at. Idempotent on already-cancelled rows (no-op when status
// no longer matches the WHERE clause). Out of scope for the current
// handler — included so the future cancel endpoint and the cron worker
// share one repo surface.
func (r *AccountDeleteRequestRepo) MarkCancelled(ctx context.Context, id int64, cancelledAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&entity.AccountDeleteRequest{}).
		Where("id = ? AND status = ?", id, entity.AccountDeleteRequestStatusPending).
		Updates(map[string]any{
			"status":       entity.AccountDeleteRequestStatusCancelled,
			"cancelled_at": cancelledAt,
		}).Error
}

// ClaimExpiredPending atomically transitions up to `limit` rows from
// 'pending' → 'processing' for any row whose cooling_off_until has
// elapsed. The single round-trip claim is what makes the cron worker
// safe to run with N replicas: every claim is its own UPDATE with
// `FOR UPDATE SKIP LOCKED`, so two replicas attempting to claim the
// same row at the same instant cannot both succeed — the loser's
// subquery sees zero rows and the UPDATE turns into a no-op.
//
// Returns the claimed rows. An empty slice means "nothing to do" —
// not an error.
//
// IMPORTANT: claiming flips status to 'processing', which releases the
// partial UNIQUE on (account_id) WHERE status='pending'. This means
// migration 028's idempotency lock (one pending request per account)
// stays intact: no two pending rows ever, AND a re-submission while a
// claim is in flight on the same account is allowed (rare, but the
// new pending row blocks a second cron pickup until the original
// cascade lands). Documented here because the asymmetry only makes
// sense in the context of multi-replica safety.
func (r *AccountDeleteRequestRepo) ClaimExpiredPending(ctx context.Context, limit int) ([]*entity.AccountDeleteRequest, error) {
	if limit <= 0 {
		return nil, nil
	}
	// Postgres-only: SKIP LOCKED + RETURNING. Avoids the read-then-write
	// race between replicas. The CTE wraps the locking SELECT so the
	// UPDATE only touches rows we successfully locked.
	const query = `
		WITH claimed AS (
			SELECT id
			FROM identity.account_delete_requests
			WHERE status = ? AND cooling_off_until <= NOW()
			ORDER BY cooling_off_until ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE identity.account_delete_requests AS r
		SET status = ?
		FROM claimed
		WHERE r.id = claimed.id AND r.status = ?
		RETURNING r.id, r.account_id, r.requested_by, r.status, r.reason,
		          r.reason_text, r.cooling_off_until, r.requested_at,
		          r.cancelled_at, r.completed_at
	`
	var rows []*entity.AccountDeleteRequest
	if err := r.db.WithContext(ctx).Raw(query,
		entity.AccountDeleteRequestStatusPending,
		limit,
		entity.AccountDeleteRequestStatusProcessing,
		entity.AccountDeleteRequestStatusPending,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// MarkCompleted flips a processing row to 'completed' and stamps
// completed_at. Idempotent: if the row is no longer 'processing' the
// UPDATE matches zero rows and returns nil — that's the recovery shape
// when a previous worker process succeeded but crashed before
// recording the outcome.
func (r *AccountDeleteRequestRepo) MarkCompleted(ctx context.Context, id int64, completedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&entity.AccountDeleteRequest{}).
		Where("id = ? AND status = ?", id, entity.AccountDeleteRequestStatusProcessing).
		Updates(map[string]any{
			"status":       entity.AccountDeleteRequestStatusCompleted,
			"completed_at": completedAt,
		}).Error
}

// MarkExpired flips a processing row to 'expired' — the terminal state
// when the cascade failed. We deliberately don't retry: an automated
// retry against a partially-cascaded account risks double-charging the
// wallet zero-out / re-deactivating Zitadel, and the cascade is itself
// best-effort with per-step warn logs that operators can use to
// diagnose. 'expired' surfaces the row for human review.
func (r *AccountDeleteRequestRepo) MarkExpired(ctx context.Context, id int64, completedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&entity.AccountDeleteRequest{}).
		Where("id = ? AND status = ?", id, entity.AccountDeleteRequestStatusProcessing).
		Updates(map[string]any{
			"status":       entity.AccountDeleteRequestStatusExpired,
			"completed_at": completedAt,
		}).Error
}
