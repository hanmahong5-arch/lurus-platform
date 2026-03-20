package app

import (
	"context"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// ServiceKeyAdminService provides CRUD operations for service API keys.
type ServiceKeyAdminService struct {
	db *gorm.DB
}

// NewServiceKeyAdminService creates the admin service.
func NewServiceKeyAdminService(db *gorm.DB) *ServiceKeyAdminService {
	return &ServiceKeyAdminService{db: db}
}

// Create inserts a new service API key.
func (s *ServiceKeyAdminService) Create(ctx context.Context, key *entity.ServiceAPIKey) error {
	return s.db.WithContext(ctx).Create(key).Error
}

// ListAll returns all service keys (active, suspended, revoked) without hashes.
func (s *ServiceKeyAdminService) ListAll(ctx context.Context) ([]entity.ServiceAPIKey, error) {
	var keys []entity.ServiceAPIKey
	err := s.db.WithContext(ctx).
		Select("id, key_prefix, service_name, description, scopes, rate_limit_rpm, status, created_by, last_used_at, created_at, updated_at").
		Order("created_at DESC").
		Find(&keys).Error
	return keys, err
}

// Revoke permanently revokes a service key.
func (s *ServiceKeyAdminService) Revoke(ctx context.Context, id int64) error {
	result := s.db.WithContext(ctx).
		Model(&entity.ServiceAPIKey{}).
		Where("id = ? AND status != ?", id, entity.ServiceKeyRevoked).
		Update("status", entity.ServiceKeyRevoked)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("service key %d not found or already revoked", id)
	}
	return nil
}
