package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// DeviceTokenRepo manages FCM/APNs device token persistence.
type DeviceTokenRepo struct {
	db *gorm.DB
}

// NewDeviceTokenRepo creates a new DeviceTokenRepo.
func NewDeviceTokenRepo(db *gorm.DB) *DeviceTokenRepo {
	return &DeviceTokenRepo{db: db}
}

// Upsert creates or reactivates a device token.
func (r *DeviceTokenRepo) Upsert(ctx context.Context, accountID int64, platform, token string) error {
	result := r.db.WithContext(ctx).
		Where("token = ?", token).
		Assign(entity.DeviceToken{
			AccountID: accountID,
			Platform:  platform,
			Active:    true,
		}).
		FirstOrCreate(&entity.DeviceToken{
			AccountID: accountID,
			Platform:  platform,
			Token:     token,
			Active:    true,
		})
	if result.Error != nil {
		return fmt.Errorf("upsert device token: %w", result.Error)
	}
	return nil
}

// Deactivate marks a device token as inactive.
func (r *DeviceTokenRepo) Deactivate(ctx context.Context, accountID int64, token string) error {
	result := r.db.WithContext(ctx).
		Model(&entity.DeviceToken{}).
		Where("account_id = ? AND token = ?", accountID, token).
		Update("active", false)
	if result.Error != nil {
		return fmt.Errorf("deactivate device token: %w", result.Error)
	}
	return nil
}

// GetActiveTokens returns all active tokens for an account.
func (r *DeviceTokenRepo) GetActiveTokens(ctx context.Context, accountID int64) ([]string, error) {
	var tokens []string
	err := r.db.WithContext(ctx).
		Model(&entity.DeviceToken{}).
		Where("account_id = ? AND active = ?", accountID, true).
		Pluck("token", &tokens).Error
	if err != nil {
		return nil, fmt.Errorf("get active tokens: %w", err)
	}
	return tokens, nil
}
