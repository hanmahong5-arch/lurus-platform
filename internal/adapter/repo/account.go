// Package repo provides GORM-based repository implementations.
package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// AccountRepo handles persistence for accounts and OAuth bindings.
type AccountRepo struct {
	db *gorm.DB
}

func NewAccountRepo(db *gorm.DB) *AccountRepo { return &AccountRepo{db: db} }

func (r *AccountRepo) Create(ctx context.Context, a *entity.Account) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *AccountRepo) Update(ctx context.Context, a *entity.Account) error {
	return r.db.WithContext(ctx).Save(a).Error
}

// SetNewAPIUserID 写入 NewAPI 用户映射。一次性 column update，避免 Save 冲掉
// 其他 mutation 字段。重复写同一值是 no-op（DB level）。
func (r *AccountRepo) SetNewAPIUserID(ctx context.Context, accountID int64, newapiUserID int) error {
	return r.db.WithContext(ctx).
		Model(&entity.Account{}).
		Where("id = ?", accountID).
		Update("newapi_user_id", newapiUserID).
		Error
}

// ListWithoutNewAPIUser 返回还没绑 NewAPI 用户的 platform 账号（最旧的优先）。
// limit 防止单次扫描扛起整个表 — reconcile cron 每 tick 处理一批，多轮追平。
//
// 用 partial unique index `idx_accounts_newapi_user_id_unique`（migration 027）
// 覆盖 NULL 行的查询，所以即便 accounts 表很大，这个 query 仍然 cheap。
func (r *AccountRepo) ListWithoutNewAPIUser(ctx context.Context, limit int) ([]*entity.Account, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []*entity.Account
	err := r.db.WithContext(ctx).
		Where("newapi_user_id IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&out).
		Error
	return out, err
}

func (r *AccountRepo) GetByID(ctx context.Context, id int64) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).First(&a, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *AccountRepo) GetByEmail(ctx context.Context, email string) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *AccountRepo) GetByZitadelSub(ctx context.Context, sub string) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).Where("zitadel_sub = ?", sub).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *AccountRepo) GetByLurusID(ctx context.Context, lurusID string) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).Where("lurus_id = ?", lurusID).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *AccountRepo) GetByAffCode(ctx context.Context, code string) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).Where("aff_code = ?", code).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *AccountRepo) GetByUsername(ctx context.Context, username string) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).Where("lower(username) = lower(?)", username).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

func (r *AccountRepo) GetByPhone(ctx context.Context, phone string) (*entity.Account, error) {
	var a entity.Account
	err := r.db.WithContext(ctx).Where("phone = ? AND phone != ''", phone).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

// List returns paginated accounts with optional keyword filter on email/display_name.
func (r *AccountRepo) List(ctx context.Context, keyword string, page, pageSize int) ([]*entity.Account, int64, error) {
	var accounts []*entity.Account
	var total int64

	q := r.db.WithContext(ctx).Model(&entity.Account{})
	if keyword != "" {
		like := fmt.Sprintf("%%%s%%", keyword)
		q = q.Where("email ILIKE ? OR display_name ILIKE ?", like, like)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	err := q.Order("id DESC").Limit(pageSize).Offset(offset).Find(&accounts).Error
	return accounts, total, err
}

// UpsertOAuthBinding creates or updates an OAuth binding for an account.
func (r *AccountRepo) UpsertOAuthBinding(ctx context.Context, b *entity.OAuthBinding) error {
	return r.db.WithContext(ctx).
		Where("provider = ? AND provider_id = ?", b.Provider, b.ProviderID).
		Assign(entity.OAuthBinding{AccountID: b.AccountID, ProviderEmail: b.ProviderEmail}).
		FirstOrCreate(b).Error
}

// GetByOAuthBinding looks up an account via its OAuth provider binding.
// Returns nil if no matching binding exists.
func (r *AccountRepo) GetByOAuthBinding(ctx context.Context, provider, providerID string) (*entity.Account, error) {
	var binding entity.OAuthBinding
	err := r.db.WithContext(ctx).
		Where("provider = ? AND provider_id = ?", provider, providerID).
		First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth binding: %w", err)
	}
	return r.GetByID(ctx, binding.AccountID)
}

func (r *AccountRepo) GetOAuthBindings(ctx context.Context, accountID int64) ([]entity.OAuthBinding, error) {
	var bindings []entity.OAuthBinding
	err := r.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&bindings).Error
	return bindings, err
}
