package entity

import "time"

// APIKey is a Lurus-level abstraction over a Zitadel Service User + PAT.
//
// The point of this layer is to keep operators out of Zitadel console:
// the underlying Zitadel concepts (machine user / IAM_OWNER / PAT)
// never leak past the /admin/v1/api-keys endpoints. Operators see only
// `name`, `display_name`, `purpose`, `token` (returned once on create).
//
// State machine:
//
//	creating  — DB row exists, Zitadel side calls in flight
//	active    — Zitadel User + PAT created (zitadel_user_id required)
//	failed    — Zitadel call failed; Service.Create may clean up + retry
//	            on the next call with the same name
//	revoked   — operator-deleted; row kept for audit, may be reincarnated
//	            (a new Create with same name takes over the row)
type APIKey struct {
	ID              int64      `json:"id"            gorm:"primaryKey;autoIncrement"`
	Name            string     `json:"name"          gorm:"type:varchar(64);uniqueIndex;not null"`
	DisplayName     string     `json:"display_name"  gorm:"type:varchar(128);not null"`
	Purpose         string     `json:"purpose"       gorm:"type:varchar(32);not null"`
	ZitadelUserID   string     `json:"-"             gorm:"type:varchar(64)"`
	ZitadelTokenID  string     `json:"-"             gorm:"type:varchar(64)"`
	Status          string     `json:"status"        gorm:"type:varchar(16);not null;default:creating"`
	Error           string     `json:"error,omitempty"`
	TokenHash       string     `json:"-"             gorm:"type:varchar(64)"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedBy       *int64     `json:"created_by,omitempty"`
}

// TableName pins the GORM mapping to identity.api_keys (schema-qualified
// because the platform uses a non-public schema for tenant isolation).
func (APIKey) TableName() string { return "identity.api_keys" }

// IsActive reports whether the key is usable. Anything else (creating,
// failed, revoked) means callers should treat the key as not-yet-issued.
func (k *APIKey) IsActive() bool { return k.Status == APIKeyStatusActive }

// API key state-machine constants.
const (
	APIKeyStatusCreating = "creating"
	APIKeyStatusActive   = "active"
	APIKeyStatusFailed   = "failed"
	APIKeyStatusRevoked  = "revoked"
)

// Allowed values for APIKey.Purpose. Validated at the service layer,
// not enforced by the DB column (so adding a new purpose doesn't
// require a migration).
const (
	APIKeyPurposeLoginUI  = "login_ui"
	APIKeyPurposeMCP      = "mcp"
	APIKeyPurposeExternal = "external"
	APIKeyPurposeAdmin    = "admin"
)
