package entity

import (
	"encoding/json"
	"time"
)

// Wallet is the unified cross-product credit balance for an account.
// 1 Credit = 1 CNY.
type Wallet struct {
	ID            int64   `json:"id"             gorm:"primaryKey;autoIncrement"`
	AccountID     int64   `json:"account_id"     gorm:"uniqueIndex;not null"`
	Balance       float64 `json:"balance"        gorm:"type:decimal(14,4);default:0"`
	Frozen        float64 `json:"frozen"         gorm:"type:decimal(14,4);default:0"`
	LifetimeTopup float64 `json:"lifetime_topup" gorm:"type:decimal(12,2);default:0"`
	LifetimeSpend float64 `json:"lifetime_spend" gorm:"type:decimal(12,2);default:0"`
}

func (Wallet) TableName() string { return "billing.wallets" }

// WalletTransaction is an append-only ledger entry. Never mutate after creation.
type WalletTransaction struct {
	ID            int64           `json:"id"             gorm:"primaryKey;autoIncrement"`
	WalletID      int64           `json:"wallet_id"      gorm:"not null;index"`
	AccountID     int64           `json:"account_id"     gorm:"not null;index"`
	Type          string          `json:"type"           gorm:"type:varchar(32);not null"`
	Amount        float64         `json:"amount"         gorm:"type:decimal(14,4);not null"`
	BalanceAfter  float64         `json:"balance_after"  gorm:"type:decimal(14,4);not null"`
	ProductID     string          `json:"product_id"     gorm:"type:varchar(32)"`
	ReferenceType string          `json:"reference_type" gorm:"type:varchar(32)"`
	ReferenceID   string          `json:"reference_id"   gorm:"type:varchar(128)"`
	Description   string          `json:"description"    gorm:"type:text"`
	Metadata      json.RawMessage `json:"metadata"       gorm:"type:jsonb;default:'{}'"`
	CreatedAt     time.Time       `json:"created_at"     gorm:"autoCreateTime"`
}

func (WalletTransaction) TableName() string { return "billing.wallet_transactions" }

// Transaction type constants.
const (
	TxTypeTopup            = "topup"
	TxTypeSubscription     = "subscription"
	TxTypeProductPurchase  = "product_purchase"
	TxTypeRefund           = "refund"
	TxTypeBonus            = "bonus"
	TxTypeReferralReward   = "referral_reward"
	TxTypeRedemption       = "redemption"
	TxTypeCheckinReward    = "checkin_reward"
	TxTypePreAuthSettle    = "preauth_settle"    // actual charge after pre-auth
	TxTypePreAuthRelease   = "preauth_release"   // refund of unused frozen amount
	TxTypeCurrencyExchange = "currency_exchange"  // LUC -> LUT one-way conversion
)

// PaymentOrder records an external payment intent and its lifecycle.
type PaymentOrder struct {
	ID             int64           `json:"id"              gorm:"primaryKey;autoIncrement"`
	AccountID      int64           `json:"account_id"      gorm:"not null;index"`
	OrderNo        string          `json:"order_no"        gorm:"type:varchar(64);uniqueIndex;not null"`
	OrderType      string          `json:"order_type"      gorm:"type:varchar(32);not null"` // topup/subscription/one_time
	ProductID      string          `json:"product_id"      gorm:"type:varchar(32)"`
	PlanID         *int64          `json:"plan_id"`
	AmountCNY      float64         `json:"amount_cny"      gorm:"type:decimal(10,2);not null"`
	Currency       string          `json:"currency"        gorm:"type:varchar(8);default:'CNY'"`
	PaymentMethod  string          `json:"payment_method"  gorm:"type:varchar(32)"`
	Status         string          `json:"status"          gorm:"type:varchar(16);default:'pending'"` // pending/paid/failed/cancelled/expired/refunded
	ExternalID     string          `json:"external_id"     gorm:"type:varchar(128)"`
	PaidAt         *time.Time      `json:"paid_at"`
	CallbackData   json.RawMessage `json:"callback_data"   gorm:"type:jsonb;default:'{}'"`
	SourceService  string          `json:"source_service"  gorm:"type:varchar(32);default:'platform'"` // which product initiated the checkout
	IdempotencyKey string          `json:"idempotency_key" gorm:"type:varchar(128)"`
	ExpiresAt      *time.Time      `json:"expires_at"`
	PayURL         string          `json:"pay_url"         gorm:"type:text"`
	CreatedAt      time.Time       `json:"created_at"      gorm:"autoCreateTime"`
	UpdatedAt      time.Time       `json:"updated_at"      gorm:"autoUpdateTime"`
}

func (PaymentOrder) TableName() string { return "billing.payment_orders" }

// Order status constants.
const (
	OrderStatusPending   = "pending"
	OrderStatusPaid      = "paid"
	OrderStatusFailed    = "failed"
	OrderStatusCancelled = "cancelled"
	OrderStatusExpired   = "expired"
	OrderStatusRefunded  = "refunded"
)

// WalletPreAuthorization records a frozen balance hold for streaming API calls.
// Flow: PreAuthorize (freeze) -> Settle (charge actual) or Release (unfreeze).
type WalletPreAuthorization struct {
	ID           int64      `json:"id"            gorm:"primaryKey;autoIncrement"`
	AccountID    int64      `json:"account_id"    gorm:"not null;index"`
	WalletID     int64      `json:"wallet_id"     gorm:"not null"`
	Amount       float64    `json:"amount"        gorm:"type:decimal(14,4);not null"` // frozen amount
	ActualAmount *float64   `json:"actual_amount" gorm:"type:decimal(14,4)"`          // settled amount
	Status       string     `json:"status"        gorm:"type:varchar(16);default:'active'"`
	ProductID    string     `json:"product_id"    gorm:"type:varchar(32);not null"`
	ReferenceID  string     `json:"reference_id"  gorm:"type:varchar(128)"`
	Description  string     `json:"description"   gorm:"type:text"`
	ExpiresAt    time.Time  `json:"expires_at"    gorm:"not null"`
	CreatedAt    time.Time  `json:"created_at"    gorm:"autoCreateTime"`
	SettledAt    *time.Time `json:"settled_at"`
}

func (WalletPreAuthorization) TableName() string { return "billing.wallet_pre_authorizations" }

// Pre-authorization status constants.
const (
	PreAuthStatusActive   = "active"
	PreAuthStatusSettled  = "settled"
	PreAuthStatusReleased = "released"
	PreAuthStatusExpired  = "expired"
)

// RedemptionCode is a one-use or multi-use promo code.
type RedemptionCode struct {
	ID             int64           `json:"id"              gorm:"primaryKey;autoIncrement"`
	Code           string          `json:"code"            gorm:"type:varchar(32);uniqueIndex;not null"`
	ProductID      string          `json:"product_id"      gorm:"type:varchar(32)"` // NULL = global Credits
	RewardType     string          `json:"reward_type"     gorm:"type:varchar(32);not null"` // credits/subscription_trial/quota_grant
	RewardValue    float64         `json:"reward_value"    gorm:"type:decimal(12,4)"`
	RewardMetadata json.RawMessage `json:"reward_metadata" gorm:"type:jsonb;default:'{}'"`
	MaxUses        int             `json:"max_uses"        gorm:"default:1"`
	UsedCount      int             `json:"used_count"      gorm:"default:0"`
	ExpiresAt      *time.Time      `json:"expires_at"`
	BatchID        string          `json:"batch_id"        gorm:"type:varchar(64)"`
	CreatedBy      *int64          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"      gorm:"autoCreateTime"`
}

func (RedemptionCode) TableName() string { return "billing.redemption_codes" }
