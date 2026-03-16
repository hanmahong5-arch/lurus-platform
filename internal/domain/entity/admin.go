package entity

import "time"

// AdminSetting is a runtime-configurable key-value entry in admin.settings.
// Secrets (is_secret=true) are masked to "••••••••" in API responses.
type AdminSetting struct {
	Key       string    `json:"key"        gorm:"primaryKey;type:varchar(128)"`
	Value     string    `json:"value"      gorm:"type:text;not null;default:''"`
	IsSecret  bool      `json:"is_secret"  gorm:"column:is_secret;not null;default:false"`
	UpdatedBy string    `json:"updated_by" gorm:"type:varchar(128);not null;default:'system'"`
	UpdatedAt time.Time `json:"updated_at" gorm:"not null;default:now()"`
}

func (AdminSetting) TableName() string { return "admin.settings" }
