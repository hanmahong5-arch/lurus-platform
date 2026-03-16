package repo

import (
	"context"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AdminSettingsRepo handles persistence for admin.settings.
type AdminSettingsRepo struct {
	db *gorm.DB
}

func NewAdminSettingsRepo(db *gorm.DB) *AdminSettingsRepo {
	return &AdminSettingsRepo{db: db}
}

// GetAll returns all admin settings rows.
func (r *AdminSettingsRepo) GetAll(ctx context.Context) ([]entity.AdminSetting, error) {
	var settings []entity.AdminSetting
	err := r.db.WithContext(ctx).Find(&settings).Error
	return settings, err
}

// Set upserts a single setting: inserts or updates value + metadata.
func (r *AdminSettingsRepo) Set(ctx context.Context, key, value, updatedBy string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"value":      value,
				"updated_by": updatedBy,
				"updated_at": now,
			}),
		}).
		Create(&entity.AdminSetting{
			Key:       key,
			Value:     value,
			UpdatedBy: updatedBy,
			UpdatedAt: now,
		}).Error
}
