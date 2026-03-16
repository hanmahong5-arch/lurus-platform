package entity

import "time"

// Subscription tracks a user's active plan for a specific product.
type Subscription struct {
	ID             int64      `json:"id"               gorm:"primaryKey;autoIncrement"`
	AccountID      int64      `json:"account_id"       gorm:"not null;index"`
	ProductID      string     `json:"product_id"       gorm:"type:varchar(32);not null;index"`
	PlanID         int64      `json:"plan_id"          gorm:"not null"`
	Status         string     `json:"status"           gorm:"type:varchar(16);default:'pending'"` // pending/trial/active/grace/expired/cancelled/suspended
	StartedAt      *time.Time `json:"started_at"`
	ExpiresAt      *time.Time `json:"expires_at"`  // NULL = forever
	GraceUntil     *time.Time `json:"grace_until"` // 7-day grace period
	AutoRenew      bool       `json:"auto_renew"   gorm:"default:false"`
	PaymentMethod  string     `json:"payment_method"   gorm:"type:varchar(32)"`
	ExternalSubID  string     `json:"external_sub_id"  gorm:"type:varchar(128)"`
	CreatedAt       time.Time  `json:"created_at"        gorm:"autoCreateTime"`
	UpdatedAt       time.Time  `json:"updated_at"        gorm:"autoUpdateTime"`
	RenewalAttempts int        `json:"renewal_attempts"  gorm:"default:0"`
	NextRenewalAt   *time.Time `json:"next_renewal_at,omitempty"`
}

func (Subscription) TableName() string { return "identity.subscriptions" }

// Subscription status constants.
const (
	SubStatusPending   = "pending"
	SubStatusTrial     = "trial"
	SubStatusActive    = "active"
	SubStatusGrace     = "grace"
	SubStatusExpired   = "expired"
	SubStatusCancelled = "cancelled"
	SubStatusSuspended = "suspended"
)

// IsLive reports whether the subscription currently grants entitlements.
func (s *Subscription) IsLive() bool {
	return s.Status == SubStatusActive || s.Status == SubStatusGrace || s.Status == SubStatusTrial
}

// AccountEntitlement is a pre-computed single-source-of-truth snapshot for a user's
// product permissions. Maintained by lurus-platform; consumed by all products.
type AccountEntitlement struct {
	ID         int64      `json:"id"          gorm:"primaryKey;autoIncrement"`
	AccountID  int64      `json:"account_id"  gorm:"not null;index"`
	ProductID  string     `json:"product_id"  gorm:"type:varchar(32);not null"`
	Key        string     `json:"key"         gorm:"type:varchar(64);not null"`
	Value      string     `json:"value"       gorm:"type:text;not null"`
	ValueType  string     `json:"value_type"  gorm:"type:varchar(16);default:'string'"` // string/integer/boolean/decimal
	Source     string     `json:"source"      gorm:"type:varchar(32)"`                  // subscription/admin_grant/promo
	SourceRef  string     `json:"source_ref"  gorm:"type:varchar(128)"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

func (AccountEntitlement) TableName() string { return "identity.account_entitlements" }
