package entity

import (
	"encoding/json"
	"time"
)

// AccountVIP tracks the VIP tier for an account.
// Level = MAX(yearly_sub_grant, spend_grant).
type AccountVIP struct {
	AccountID       int64      `json:"account_id"        gorm:"primaryKey"`
	Level           int16      `json:"level"             gorm:"default:0"`
	LevelName       string     `json:"level_name"        gorm:"type:varchar(32)"`
	Points          int64      `json:"points"            gorm:"default:0"` // 1 CNY = 10 pts
	YearlySubGrant  int16      `json:"yearly_sub_grant"  gorm:"default:0"`
	SpendGrant      int16      `json:"spend_grant"       gorm:"default:0"`
	LevelExpiresAt  *time.Time `json:"level_expires_at"`
	UpdatedAt       time.Time  `json:"updated_at"        gorm:"autoUpdateTime"`
}

func (AccountVIP) TableName() string { return "identity.account_vip" }

// VIPLevelConfig is the operator-configurable table for VIP tiers.
type VIPLevelConfig struct {
	Level             int16           `json:"level"               gorm:"primaryKey"`
	Name              string          `json:"name"                gorm:"type:varchar(32);not null"`
	MinSpendCNY       float64         `json:"min_spend_cny"       gorm:"type:decimal(10,2);default:0"`
	YearlySubMinPlan  string          `json:"yearly_sub_min_plan" gorm:"type:varchar(32)"` // plan code threshold
	GlobalDiscount    float64         `json:"global_discount"     gorm:"type:decimal(4,3);default:1.000"`
	PerksJSON         json.RawMessage `json:"perks_json"          gorm:"type:jsonb;default:'{}'"`
}

func (VIPLevelConfig) TableName() string { return "identity.vip_level_configs" }
