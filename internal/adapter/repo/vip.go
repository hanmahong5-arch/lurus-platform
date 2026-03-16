package repo

import (
	"context"
	"errors"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VIPRepo manages VIP levels and configs.
type VIPRepo struct {
	db *gorm.DB
}

func NewVIPRepo(db *gorm.DB) *VIPRepo { return &VIPRepo{db: db} }

func (r *VIPRepo) GetByAccountID(ctx context.Context, accountID int64) (*entity.AccountVIP, error) {
	var v entity.AccountVIP
	err := r.db.WithContext(ctx).First(&v, "account_id = ?", accountID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &v, err
}

// GetOrCreate returns the VIP record, initialising level 0 if missing.
func (r *VIPRepo) GetOrCreate(ctx context.Context, accountID int64) (*entity.AccountVIP, error) {
	var v entity.AccountVIP
	err := r.db.WithContext(ctx).
		Where(entity.AccountVIP{AccountID: accountID}).
		FirstOrCreate(&v).Error
	return &v, err
}

func (r *VIPRepo) Update(ctx context.Context, v *entity.AccountVIP) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "account_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"level", "level_name", "points", "yearly_sub_grant", "spend_grant", "level_expires_at", "updated_at"}),
		}).
		Create(v).Error
}

// ListConfigs loads all VIP level configurations ordered by level.
func (r *VIPRepo) ListConfigs(ctx context.Context) ([]entity.VIPLevelConfig, error) {
	var list []entity.VIPLevelConfig
	err := r.db.WithContext(ctx).Order("level ASC").Find(&list).Error
	return list, err
}
