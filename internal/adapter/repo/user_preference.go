package repo

import (
	"context"
	"encoding/json"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PreferenceRepo manages user preference persistence.
type PreferenceRepo struct {
	db *gorm.DB
}

// NewPreferenceRepo creates a new preference repository.
func NewPreferenceRepo(db *gorm.DB) *PreferenceRepo {
	return &PreferenceRepo{db: db}
}

// Upsert creates or updates a preference entry for the given account+namespace.
func (r *PreferenceRepo) Upsert(ctx context.Context, accountID int64, namespace string, data json.RawMessage) (*entity.UserPreference, error) {
	pref := &entity.UserPreference{
		AccountID: accountID,
		Namespace: namespace,
		Data:      data,
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "account_id"}, {Name: "namespace"}},
			DoUpdates: clause.AssignmentColumns([]string{"data", "updated_at"}),
		}).
		Create(pref).Error
	if err != nil {
		return nil, err
	}
	return pref, nil
}

// Get returns the preference entry for the given account+namespace, or nil if not found.
func (r *PreferenceRepo) Get(ctx context.Context, accountID int64, namespace string) (*entity.UserPreference, error) {
	var pref entity.UserPreference
	err := r.db.WithContext(ctx).
		Where("account_id = ? AND namespace = ?", accountID, namespace).
		First(&pref).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &pref, nil
}
