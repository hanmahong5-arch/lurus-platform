package entity

import "time"

// AccountPurge is the append-only audit row for a GDPR-grade account
// purge attempt. One row per attempt — re-runs after a failed cascade
// create a new row (status='failed' rows are not overwritten). The
// uniqueness constraint on (account_id WHERE status='purging') in
// migration 024 is what enforces "only one in-flight purge per
// account" without requiring a row-level lock on identity.accounts.
type AccountPurge struct {
	ID           int64      `json:"id"            gorm:"primaryKey;autoIncrement"`
	AccountID    int64      `json:"account_id"    gorm:"not null;index"`
	InitiatedBy  int64      `json:"initiated_by"  gorm:"not null"`
	ApprovedBy   *int64     `json:"approved_by"`
	Status       string     `json:"status"        gorm:"type:varchar(16);not null;default:purging"`
	Error        string     `json:"error,omitempty"`
	Attestation  string     `json:"biometric_attestation,omitempty" gorm:"column:biometric_attestation"`
	IP           string     `json:"ip,omitempty"  gorm:"type:inet"`
	UA           string     `json:"ua,omitempty"  gorm:"type:varchar(256);column:ua"`
	Geo          string     `json:"geo,omitempty" gorm:"type:varchar(64)"`
	StartedAt    time.Time  `json:"started_at"    gorm:"autoCreateTime"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

func (AccountPurge) TableName() string { return "identity.account_purges" }

// AccountPurge status constants. Mirrors the CHECK-style values used by
// migration 024.
const (
	AccountPurgeStatusInflight  = "purging"
	AccountPurgeStatusCompleted = "completed"
	AccountPurgeStatusFailed    = "failed"
)
