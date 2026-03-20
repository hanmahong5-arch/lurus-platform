package repo

import (
	"context"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// ServiceKeyRepo manages service API key persistence.
type ServiceKeyRepo struct {
	db *gorm.DB
}

func NewServiceKeyRepo(db *gorm.DB) *ServiceKeyRepo {
	return &ServiceKeyRepo{db: db}
}

// GetByHash returns the active service key matching the SHA-256 hash, or nil.
func (r *ServiceKeyRepo) GetByHash(ctx context.Context, hash string) (*entity.ServiceAPIKey, error) {
	var key entity.ServiceAPIKey
	err := r.db.WithContext(ctx).
		Where("key_hash = ? AND status = ?", hash, entity.ServiceKeyActive).
		First(&key).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &key, nil
}

// ListActive returns all active service keys (for cache warming).
func (r *ServiceKeyRepo) ListActive(ctx context.Context) ([]entity.ServiceAPIKey, error) {
	var keys []entity.ServiceAPIKey
	err := r.db.WithContext(ctx).
		Where("status = ?", entity.ServiceKeyActive).
		Find(&keys).Error
	return keys, err
}

// Create inserts a new service key.
func (r *ServiceKeyRepo) Create(ctx context.Context, key *entity.ServiceAPIKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

// TouchLastUsed updates the last_used_at timestamp.
func (r *ServiceKeyRepo) TouchLastUsed(ctx context.Context, id int64) {
	r.db.WithContext(ctx).
		Model(&entity.ServiceAPIKey{}).
		Where("id = ?", id).
		Update("last_used_at", gorm.Expr("NOW()"))
}

// UpdateStatus changes the key status (suspend/revoke/reactivate).
func (r *ServiceKeyRepo) UpdateStatus(ctx context.Context, id int64, status int16) error {
	return r.db.WithContext(ctx).
		Model(&entity.ServiceAPIKey{}).
		Where("id = ?", id).
		Update("status", status).Error
}
