package entity

import "time"

// ServiceAPIKey represents a scoped API key for service-to-service calls.
// Each consuming service gets its own key with specific permissions.
type ServiceAPIKey struct {
	ID           int64      `json:"id"            gorm:"primaryKey;autoIncrement"`
	KeyHash      string     `json:"-"             gorm:"uniqueIndex;not null"` // SHA-256, never exposed
	KeyPrefix    string     `json:"key_prefix"    gorm:"not null"`             // first 8 chars for log identification
	ServiceName  string     `json:"service_name"  gorm:"not null;index"`
	Description  string     `json:"description"   gorm:"type:text"`
	Scopes       StringList `json:"scopes"        gorm:"type:text[];not null;default:'{}'"`
	RateLimitRPM int        `json:"rate_limit_rpm" gorm:"not null;default:1000"`
	Status       int16      `json:"status"        gorm:"not null;default:1"` // 1=active 2=suspended 3=revoked
	CreatedBy    string     `json:"created_by"    gorm:"type:varchar(64)"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	CreatedAt    time.Time  `json:"created_at"    gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at"    gorm:"autoUpdateTime"`
}

func (ServiceAPIKey) TableName() string { return "identity.service_api_keys" }

// Service API key status constants.
const (
	ServiceKeyActive    int16 = 1
	ServiceKeySuspended int16 = 2
	ServiceKeyRevoked   int16 = 3
)

// Service API scope constants — the complete set of permissions.
const (
	ScopeAccountRead  = "account:read"
	ScopeAccountWrite = "account:write"
	ScopeWalletRead   = "wallet:read"
	ScopeWalletDebit  = "wallet:debit"
	ScopeWalletCredit = "wallet:credit"
	ScopeEntitlement  = "entitlement"
	ScopeCheckout     = "checkout"
)

// AllScopes returns all valid scope values.
func AllScopes() []string {
	return []string{
		ScopeAccountRead, ScopeAccountWrite,
		ScopeWalletRead, ScopeWalletDebit, ScopeWalletCredit,
		ScopeEntitlement, ScopeCheckout,
	}
}

// HasScope reports whether the key has the specified scope.
func (k *ServiceAPIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// IsActive reports whether the key is currently usable.
func (k *ServiceAPIKey) IsActive() bool {
	return k.Status == ServiceKeyActive
}

// StringList is a []string that maps to PostgreSQL text[].
// For SQLite test compatibility, it also handles comma-separated strings.
type StringList []string
