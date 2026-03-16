package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// PreferenceRepo manages per-account notification preferences.
type PreferenceRepo struct {
	db *gorm.DB
}

// NewPreferenceRepo creates a new PreferenceRepo.
func NewPreferenceRepo(db *gorm.DB) *PreferenceRepo {
	return &PreferenceRepo{db: db}
}

// GetByAccount returns all channel preferences for an account.
func (r *PreferenceRepo) GetByAccount(ctx context.Context, accountID int64) ([]entity.Preference, error) {
	var prefs []entity.Preference
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&prefs).Error; err != nil {
		return nil, fmt.Errorf("get preferences: %w", err)
	}
	return prefs, nil
}

// IsChannelEnabled checks if a specific channel is enabled for an account.
// Returns true by default if no preference record exists.
func (r *PreferenceRepo) IsChannelEnabled(ctx context.Context, accountID int64, channel entity.Channel) (bool, error) {
	var pref entity.Preference
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND channel = ?", accountID, channel).
		First(&pref).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return true, nil // enabled by default
		}
		return false, fmt.Errorf("check channel enabled: %w", err)
	}
	return pref.Enabled, nil
}

// Upsert creates or updates a channel preference for an account.
func (r *PreferenceRepo) Upsert(ctx context.Context, accountID int64, channel entity.Channel, enabled bool) error {
	result := r.db.WithContext(ctx).
		Where("account_id = ? AND channel = ?", accountID, channel).
		Assign(entity.Preference{Enabled: enabled}).
		FirstOrCreate(&entity.Preference{
			AccountID: accountID,
			Channel:   channel,
			Enabled:   enabled,
		})
	if result.Error != nil {
		return fmt.Errorf("upsert preference: %w", result.Error)
	}
	return nil
}
