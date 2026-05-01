package entity

import "time"

// AccountDeleteRequest is the user-self-initiated counterpart to
// AccountPurge. It captures a request the user submitted via
// POST /api/v1/account/me/delete-request and tracks the 30-day
// cooling-off window during which the user may cancel by simply
// logging in again.
//
// The asymmetry with AccountPurge is intentional: AccountPurge is the
// terminal audit trail of an *executed* cascade, AccountDeleteRequest
// is the *intent* registered before the cascade runs. A successful
// completion writes one row to each table.
type AccountDeleteRequest struct {
	ID              int64      `json:"id"                  gorm:"primaryKey;autoIncrement"`
	AccountID       int64      `json:"account_id"          gorm:"not null;index"`
	RequestedBy     int64      `json:"requested_by"        gorm:"not null"`
	Status          string     `json:"status"              gorm:"type:varchar(16);not null;default:pending"`
	Reason          string     `json:"reason,omitempty"    gorm:"type:varchar(32)"`
	ReasonText      string     `json:"reason_text,omitempty" gorm:"type:varchar(1024)"`
	CoolingOffUntil time.Time  `json:"cooling_off_until"   gorm:"not null"`
	RequestedAt     time.Time  `json:"requested_at"        gorm:"autoCreateTime"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// TableName binds the model to identity.account_delete_requests.
func (AccountDeleteRequest) TableName() string {
	return "identity.account_delete_requests"
}

// AccountDeleteRequest status constants. Mirrors the values referenced
// by migration 028.
//
// Lifecycle:
//
//	pending    → freshly submitted, sitting in the cooling-off window
//	processing → claimed by the cron worker, cascade in flight
//	cancelled  → user changed their mind
//	completed  → cooling-off elapsed AND cascade succeeded
//	expired    → cooling-off elapsed but cascade failed (terminal — surface
//	             for human review rather than retrying forever)
//
// "processing" is a transient claim flag, not a separate column on the
// schema; the partial UNIQUE index in migration 028 only blocks
// status='pending', so claiming via UPDATE...RETURNING into 'processing'
// also releases the unique slot. That's load-bearing for replica safety.
const (
	AccountDeleteRequestStatusPending    = "pending"
	AccountDeleteRequestStatusProcessing = "processing"
	AccountDeleteRequestStatusCancelled  = "cancelled"
	AccountDeleteRequestStatusCompleted  = "completed"
	AccountDeleteRequestStatusExpired    = "expired"
)

// AccountDeleteReason* are the closed enum of reason codes accepted by
// the handler. Free-text explanation goes in ReasonText. Adding a new
// reason is a one-line change here — the handler validates against
// this set.
const (
	AccountDeleteReasonNoLongerUsing    = "no_longer_using"
	AccountDeleteReasonPrivacyConcern   = "privacy_concern"
	AccountDeleteReasonExperienceIssue  = "experience_issue"
	AccountDeleteReasonFoundAlternative = "found_alternative"
	AccountDeleteReasonOther            = "other"
)

// IsValidAccountDeleteReason returns true when the supplied reason is
// one of the recognised enum values. Empty string returns true so the
// handler can accept a body that omits the field entirely.
func IsValidAccountDeleteReason(r string) bool {
	switch r {
	case "",
		AccountDeleteReasonNoLongerUsing,
		AccountDeleteReasonPrivacyConcern,
		AccountDeleteReasonExperienceIssue,
		AccountDeleteReasonFoundAlternative,
		AccountDeleteReasonOther:
		return true
	}
	return false
}
