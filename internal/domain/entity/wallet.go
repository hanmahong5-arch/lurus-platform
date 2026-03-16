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
)

// PaymentOrder records an external payment intent and its lifecycle.
type PaymentOrder struct {
	ID            int64           `json:"id"             gorm:"primaryKey;autoIncrement"`
	AccountID     int64           `json:"account_id"     gorm:"not null;index"`
	OrderNo       string          `json:"order_no"       gorm:"type:varchar(64);uniqueIndex;not null"`
	OrderType     string          `json:"order_type"     gorm:"type:varchar(32);not null"` // topup/subscription/one_time
	ProductID     string          `json:"product_id"     gorm:"type:varchar(32)"`
	PlanID        *int64          `json:"plan_id"`
	AmountCNY     float64         `json:"amount_cny"     gorm:"type:decimal(10,2);not null"`
	Currency      string          `json:"currency"       gorm:"type:varchar(8);default:'CNY'"`
	PaymentMethod string          `json:"payment_method" gorm:"type:varchar(32)"`
	Status        string          `json:"status"         gorm:"type:varchar(16);default:'pending'"` // pending/paid/failed/cancelled/refunded
	ExternalID    string          `json:"external_id"    gorm:"type:varchar(128)"`
	PaidAt        *time.Time      `json:"paid_at"`
	CallbackData  json.RawMessage `json:"callback_data"  gorm:"type:jsonb;default:'{}'"`
	CreatedAt     time.Time       `json:"created_at"     gorm:"autoCreateTime"`
	UpdatedAt     time.Time       `json:"updated_at"     gorm:"autoUpdateTime"`
}

func (PaymentOrder) TableName() string { return "billing.payment_orders" }

// Order status constants.
const (
	OrderStatusPending   = "pending"
	OrderStatusPaid      = "paid"
	OrderStatusFailed    = "failed"
	OrderStatusCancelled = "cancelled"
	OrderStatusRefunded  = "refunded"
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
