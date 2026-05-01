package entity

import (
	"encoding/json"
	"time"
)

// HookFailure is one row in module.hook_failures — a permanently-failed
// invocation of an async lifecycle hook (mail, notification, newapi_sync,
// …) after retry exhaustion. See migrations/030_module_hook_failures.sql
// for the wire contract.
//
// Account-scoped events store the account ID separately for the unique
// upsert path; the same value may also appear inside Payload as a
// convenience for the replay handler. Payload is the source of truth for
// replay — extra fields here would just drift.
type HookFailure struct {
	ID            int64           `json:"id"             gorm:"primaryKey"`
	Event         string          `json:"event"          gorm:"column:event"`
	HookName      string          `json:"hook_name"      gorm:"column:hook_name"`
	AccountID     *int64          `json:"account_id,omitempty" gorm:"column:account_id"`
	Payload       json.RawMessage `json:"payload"        gorm:"column:payload;type:jsonb"`
	Error         string          `json:"error"          gorm:"column:error"`
	Attempts      int             `json:"attempts"       gorm:"column:attempts"`
	FirstFailedAt time.Time       `json:"first_failed_at" gorm:"column:first_failed_at"`
	LastFailedAt  time.Time       `json:"last_failed_at"  gorm:"column:last_failed_at"`
	ReplayedAt    *time.Time      `json:"replayed_at,omitempty" gorm:"column:replayed_at"`
}

// TableName binds the entity to the schema-qualified table name.
func (HookFailure) TableName() string { return "module.hook_failures" }
