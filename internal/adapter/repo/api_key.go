package repo

import (
	"context"
	"errors"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// APIKeyRepo persists Lurus-level API key rows.
type APIKeyRepo struct {
	db *gorm.DB
}

// NewAPIKeyRepo constructs the repo. Pass the same *gorm.DB the rest
// of the platform uses; the entity is schema-qualified so no extra
// schema setup is required at the repo level.
func NewAPIKeyRepo(db *gorm.DB) *APIKeyRepo { return &APIKeyRepo{db: db} }

// ErrAPIKeyNotFound is returned by FindByName / FindByID when no row
// matches the predicate. Surfaces to handlers as 404.
var ErrAPIKeyNotFound = errors.New("repo: api key not found")

// FindByName returns the row whose `name` column matches. Used for
// idempotency checks in Service.Create and Rotate. Returns
// ErrAPIKeyNotFound when no row exists (caller maps to "create new").
func (r *APIKeyRepo) FindByName(ctx context.Context, name string) (*entity.APIKey, error) {
	var k entity.APIKey
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// FindByID returns the row by primary key. Used by handlers that route
// on /:id. Returns ErrAPIKeyNotFound if missing.
func (r *APIKeyRepo) FindByID(ctx context.Context, id int64) (*entity.APIKey, error) {
	var k entity.APIKey
	err := r.db.WithContext(ctx).First(&k, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// Create inserts a new row in 'creating' state. Caller is expected to
// flip to 'active' (or 'failed') after the Zitadel calls complete.
//
// On UNIQUE collision (name already exists), GORM returns its native
// error — Service.Create catches that path via FindByName first, so a
// raw collision here is unexpected and surfaces as-is.
func (r *APIKeyRepo) Create(ctx context.Context, k *entity.APIKey) error {
	if k.Status == "" {
		k.Status = entity.APIKeyStatusCreating
	}
	return r.db.WithContext(ctx).Create(k).Error
}

// MarkActive flips a 'creating' row to 'active' and stores the
// Zitadel-side identifiers + token hash. Atomic via a single UPDATE.
func (r *APIKeyRepo) MarkActive(ctx context.Context, id int64, zitadelUserID, zitadelTokenID, tokenHash string) error {
	return r.db.WithContext(ctx).
		Model(&entity.APIKey{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":            entity.APIKeyStatusActive,
			"zitadel_user_id":   zitadelUserID,
			"zitadel_token_id":  zitadelTokenID,
			"token_hash":        tokenHash,
			"error":             "",
		}).Error
}

// MarkFailed records a Zitadel-side failure on the row. errMsg is
// truncated to 1 KB so a stack-trace from a misbehaving downstream
// can't bloat audit history.
func (r *APIKeyRepo) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024]
	}
	return r.db.WithContext(ctx).
		Model(&entity.APIKey{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status": entity.APIKeyStatusFailed,
			"error":  errMsg,
		}).Error
}

// MarkRevoked marks a row as revoked (operator deleted). Keeps the row
// for audit; a future Create with the same name will reuse it.
func (r *APIKeyRepo) MarkRevoked(ctx context.Context, id int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&entity.APIKey{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     entity.APIKeyStatusRevoked,
			"revoked_at": &now,
		}).Error
}

// UpdateToken swaps the Zitadel token id + token hash on an existing
// row. Used by Rotate after issuing a fresh PAT.
func (r *APIKeyRepo) UpdateToken(ctx context.Context, id int64, zitadelTokenID, tokenHash string) error {
	return r.db.WithContext(ctx).
		Model(&entity.APIKey{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"zitadel_token_id": zitadelTokenID,
			"token_hash":       tokenHash,
		}).Error
}

// Reincarnate clears the prior Zitadel ids on a 'failed' or 'revoked'
// row and flips status back to 'creating'. Lets Service.Create reuse
// the same DB row (preserving id + audit history) when an operator
// re-creates a key with the same name after a previous failure or
// deletion.
func (r *APIKeyRepo) Reincarnate(ctx context.Context, id int64, displayName, purpose string, expiresAt *time.Time, createdBy *int64) error {
	return r.db.WithContext(ctx).
		Model(&entity.APIKey{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"display_name":      displayName,
			"purpose":           purpose,
			"expires_at":        expiresAt,
			"created_by":        createdBy,
			"zitadel_user_id":   "",
			"zitadel_token_id":  "",
			"token_hash":        "",
			"status":            entity.APIKeyStatusCreating,
			"error":             "",
			"revoked_at":        nil,
		}).Error
}

// List returns rows filtered by purpose (empty = all) and status
// (empty = exclude 'revoked'). Ordered by created_at desc. Returns
// the paginated slice + total count.
func (r *APIKeyRepo) List(ctx context.Context, purpose, status string, limit, offset int) ([]entity.APIKey, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&entity.APIKey{})
	if purpose != "" {
		q = q.Where("purpose = ?", purpose)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	} else {
		// Default: hide revoked rows (still queryable via explicit status filter).
		q = q.Where("status <> ?", entity.APIKeyStatusRevoked)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []entity.APIKey
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
