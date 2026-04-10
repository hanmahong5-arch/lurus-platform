package entity

import (
	"encoding/json"
	"time"
)

// UserPreference stores per-account, per-namespace preference data (JSONB).
// Used for cross-device sync (e.g. Creator model usage stats, UI preferences).
type UserPreference struct {
	ID        int64           `gorm:"primaryKey"                           json:"id"`
	AccountID int64           `gorm:"uniqueIndex:idx_pref_acct_ns;not null" json:"account_id"`
	Namespace string          `gorm:"uniqueIndex:idx_pref_acct_ns;size:64;default:default" json:"namespace"`
	Data      json.RawMessage `gorm:"type:jsonb;default:'{}'"             json:"data"`
	UpdatedAt time.Time       `gorm:"autoUpdateTime"                      json:"updated_at"`
}

func (UserPreference) TableName() string {
	return "identity.user_preferences"
}
