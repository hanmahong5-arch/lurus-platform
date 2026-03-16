package entity

import (
	"encoding/json"
	"time"
)

// Product is a Lurus platform product (llm-api, quant-trading, webmail…).
type Product struct {
	ID           string          `json:"id"            gorm:"primaryKey;type:varchar(32)"`
	Name         string          `json:"name"          gorm:"type:varchar(64);not null"`
	Description  string          `json:"description"   gorm:"type:text"`
	Category     string          `json:"category"      gorm:"type:varchar(32)"`
	BillingModel string          `json:"billing_model" gorm:"type:varchar(32);not null"`
	Status       int16           `json:"status"        gorm:"default:1"`
	SortOrder    int             `json:"sort_order"    gorm:"default:0"`
	Config       json.RawMessage `json:"config"        gorm:"type:jsonb;default:'{}'"`
}

func (Product) TableName() string { return "identity.products" }

// ProductPlan defines pricing and entitlement features for a product tier.
type ProductPlan struct {
	ID           int64           `json:"id"            gorm:"primaryKey;autoIncrement"`
	ProductID    string          `json:"product_id"    gorm:"type:varchar(32);not null;index"`
	Code         string          `json:"code"          gorm:"type:varchar(32);not null"`
	Name         string          `json:"name"          gorm:"type:varchar(64);not null"`
	BillingCycle string          `json:"billing_cycle" gorm:"type:varchar(16)"` // forever/weekly/monthly/quarterly/yearly/one_time
	PriceCNY     float64         `json:"price_cny"     gorm:"type:decimal(10,2);default:0"`
	PriceUSD     float64         `json:"price_usd"     gorm:"type:decimal(10,2);default:0"`
	IsDefault    bool            `json:"is_default"    gorm:"default:false"`
	SortOrder    int             `json:"sort_order"    gorm:"default:0"`
	Status       int16           `json:"status"        gorm:"default:1"`
	Features     json.RawMessage `json:"features"      gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt    time.Time       `json:"created_at"    gorm:"autoCreateTime"`
	UpdatedAt    time.Time       `json:"updated_at"    gorm:"autoUpdateTime"`
}

func (ProductPlan) TableName() string { return "identity.product_plans" }

// BillingCycle constants.
const (
	BillingCycleForever   = "forever"
	BillingCycleWeekly    = "weekly"
	BillingCycleMonthly   = "monthly"
	BillingCycleQuarterly = "quarterly"
	BillingCycleYearly    = "yearly"
	BillingCycleOneTime   = "one_time"
)

// BillingModel constants.
const (
	BillingModelFree         = "free"
	BillingModelQuota        = "quota"
	BillingModelSubscription = "subscription"
	BillingModelHybrid       = "hybrid"
	BillingModelOneTime      = "one_time"
	BillingModelSeat         = "seat"
	BillingModelUsage        = "usage"
)
