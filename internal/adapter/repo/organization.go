// Package repo provides GORM-based repository implementations.
package repo

import (
	"context"
	"errors"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"gorm.io/gorm"
)

// OrganizationRepo handles persistence for organizations, members, API keys, and org wallets.
type OrganizationRepo struct {
	db *gorm.DB
}

func NewOrganizationRepo(db *gorm.DB) *OrganizationRepo { return &OrganizationRepo{db: db} }

// --- Organization ---

func (r *OrganizationRepo) Create(ctx context.Context, org *entity.Organization) error {
	return r.db.WithContext(ctx).Create(org).Error
}

func (r *OrganizationRepo) GetByID(ctx context.Context, id int64) (*entity.Organization, error) {
	var org entity.Organization
	err := r.db.WithContext(ctx).First(&org, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &org, err
}

func (r *OrganizationRepo) GetBySlug(ctx context.Context, slug string) (*entity.Organization, error) {
	var org entity.Organization
	err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&org).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &org, err
}

func (r *OrganizationRepo) ListByAccountID(ctx context.Context, accountID int64) ([]entity.Organization, error) {
	var orgs []entity.Organization
	err := r.db.WithContext(ctx).
		Joins("JOIN identity.org_members om ON om.org_id = identity.organizations.id").
		Where("om.account_id = ?", accountID).
		Order("identity.organizations.id DESC").
		Find(&orgs).Error
	return orgs, err
}

func (r *OrganizationRepo) UpdateStatus(ctx context.Context, id int64, status string) error {
	return r.db.WithContext(ctx).
		Model(&entity.Organization{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *OrganizationRepo) ListAll(ctx context.Context, limit, offset int) ([]entity.Organization, error) {
	var orgs []entity.Organization
	err := r.db.WithContext(ctx).Order("id DESC").Limit(limit).Offset(offset).Find(&orgs).Error
	return orgs, err
}

// --- Members ---

func (r *OrganizationRepo) AddMember(ctx context.Context, m *entity.OrgMember) error {
	return r.db.WithContext(ctx).
		Where(entity.OrgMember{OrgID: m.OrgID, AccountID: m.AccountID}).
		Assign(entity.OrgMember{Role: m.Role}).
		FirstOrCreate(m).Error
}

func (r *OrganizationRepo) RemoveMember(ctx context.Context, orgID, accountID int64) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND account_id = ?", orgID, accountID).
		Delete(&entity.OrgMember{}).Error
}

func (r *OrganizationRepo) GetMember(ctx context.Context, orgID, accountID int64) (*entity.OrgMember, error) {
	var m entity.OrgMember
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND account_id = ?", orgID, accountID).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
}

func (r *OrganizationRepo) ListMembers(ctx context.Context, orgID int64) ([]entity.OrgMember, error) {
	var members []entity.OrgMember
	err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Find(&members).Error
	return members, err
}

// --- API Keys ---

func (r *OrganizationRepo) CreateAPIKey(ctx context.Context, k *entity.OrgAPIKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

func (r *OrganizationRepo) GetAPIKeyByHash(ctx context.Context, hash string) (*entity.OrgAPIKey, error) {
	var k entity.OrgAPIKey
	err := r.db.WithContext(ctx).Where("key_hash = ?", hash).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &k, err
}

func (r *OrganizationRepo) ListAPIKeys(ctx context.Context, orgID int64) ([]entity.OrgAPIKey, error) {
	var keys []entity.OrgAPIKey
	err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("id DESC").Find(&keys).Error
	return keys, err
}

func (r *OrganizationRepo) RevokeAPIKey(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).
		Model(&entity.OrgAPIKey{}).
		Where("id = ?", id).
		Update("status", "revoked").Error
}

func (r *OrganizationRepo) TouchAPIKey(ctx context.Context, id int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&entity.OrgAPIKey{}).
		Where("id = ?", id).
		Update("last_used_at", now).Error
}

// --- Wallet ---

func (r *OrganizationRepo) GetOrCreateWallet(ctx context.Context, orgID int64) (*entity.OrgWallet, error) {
	var w entity.OrgWallet
	err := r.db.WithContext(ctx).
		Where(entity.OrgWallet{OrgID: orgID}).
		FirstOrCreate(&w).Error
	if err != nil {
		return nil, err
	}
	return &w, nil
}
