// Package entity defines domain entities for lurus-platform.
package entity

import "time"

// Organization represents a company / team account on the Lurus platform.
// Multiple personal accounts can join an organization and share a token wallet.
type Organization struct {
	ID             int64     `json:"id"               gorm:"primaryKey;autoIncrement"`
	Name           string    `json:"name"             gorm:"not null"`
	Slug           string    `json:"slug"             gorm:"uniqueIndex;not null"`
	OwnerAccountID int64     `json:"owner_account_id" gorm:"not null"`
	Status         string    `json:"status"           gorm:"not null;default:active"` // active | suspended
	Plan           string    `json:"plan"             gorm:"not null;default:free"`   // free | team | enterprise
	CreatedAt      time.Time `json:"created_at"       gorm:"autoCreateTime"`
	UpdatedAt      time.Time `json:"updated_at"       gorm:"autoUpdateTime"`
}

func (Organization) TableName() string { return "identity.organizations" }

// OrgMember links a personal account to an organization with a specific role.
type OrgMember struct {
	OrgID     int64     `json:"org_id"     gorm:"primaryKey"`
	AccountID int64     `json:"account_id" gorm:"primaryKey"`
	Role      string    `json:"role"       gorm:"not null;default:member"` // owner | admin | member
	JoinedAt  time.Time `json:"joined_at"  gorm:"autoCreateTime"`
}

func (OrgMember) TableName() string { return "identity.org_members" }

// OrgAPIKey is a long-lived API key scoped to an organization.
// The raw key is only returned once on creation; only its SHA-256 hash is stored.
type OrgAPIKey struct {
	ID         int64      `json:"id"           gorm:"primaryKey;autoIncrement"`
	OrgID      int64      `json:"org_id"       gorm:"not null;index"`
	KeyHash    string     `json:"-"            gorm:"uniqueIndex;not null"` // never exposed
	KeyPrefix  string     `json:"key_prefix"   gorm:"not null"`
	Name       string     `json:"name"         gorm:"not null"`
	CreatedBy  int64      `json:"created_by"   gorm:"not null"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Status     string     `json:"status"       gorm:"not null;default:active"` // active | revoked
	CreatedAt  time.Time  `json:"created_at"   gorm:"autoCreateTime"`
}

func (OrgAPIKey) TableName() string { return "identity.org_api_keys" }

// OrgWallet holds the shared token balance for an organization.
// Mirrors billing.wallets but keyed on org_id instead of account_id.
type OrgWallet struct {
	OrgID         int64     `json:"org_id"         gorm:"primaryKey"`
	Balance       float64   `json:"balance"        gorm:"type:decimal(14,4);default:0"`
	Frozen        float64   `json:"frozen"         gorm:"type:decimal(14,4);default:0"`
	LifetimeTopup float64   `json:"lifetime_topup" gorm:"type:decimal(14,4);default:0"`
	LifetimeSpend float64   `json:"lifetime_spend" gorm:"type:decimal(14,4);default:0"`
	UpdatedAt     time.Time `json:"updated_at"     gorm:"autoUpdateTime"`
}

func (OrgWallet) TableName() string { return "billing.org_wallets" }
