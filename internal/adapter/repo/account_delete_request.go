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
