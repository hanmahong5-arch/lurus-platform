package entity

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// OrgServiceStatus values mirror the lifecycle DDL in migration 029.
const (
	OrgServiceStatusPending = "pending"
	OrgServiceStatusActive  = "active"
	OrgServiceStatusFailed  = "failed"

	// OrgServiceKova names the kova-rest tester provisioning target.
	// Kept as a typed constant so handlers/tests cannot fat-finger it.
	OrgServiceKova = "kova"
)

// OrgService is one provisioned downstream service for an organization.
//
// Today only `kova` is produced — a kova-rest tester instance running on the
// R6 box. The shape is intentionally generic so future targets (forge
// workspace, lucrum live-trade slot) reuse the same row without DDL churn.
//
// The raw admin key is NEVER stored: callers receive it once via the synchronous
// provision response and must rotate if lost. KeyHash + KeyPrefix exist purely
// for log triage and leak-detection without holding plaintext.
type OrgService struct {
	OrgID          int64           `json:"org_id"          gorm:"primaryKey"`
	Service        string          `json:"service"         gorm:"primaryKey"`
	Status         string          `json:"status"          gorm:"not null;default:pending"`
	BaseURL        string          `json:"base_url"`
	KeyHash        string          `json:"-"`               // SHA-256 hex of raw admin key, never exposed
	KeyPrefix      string          `json:"key_prefix"`
	TesterName     string          `json:"tester_name"`
	Port           int             `json:"port"`
	Metadata       OrgServiceMeta  `json:"metadata"        gorm:"type:jsonb;not null;default:'{}'"`
	ProvisionedAt  *time.Time      `json:"provisioned_at"`
	CreatedAt      time.Time       `json:"created_at"      gorm:"autoCreateTime"`
	UpdatedAt      time.Time       `json:"updated_at"      gorm:"autoUpdateTime"`
}

// TableName binds to billing.org_services (see migration 029).
func (OrgService) TableName() string { return "billing.org_services" }

// OrgServiceMeta is the free-form JSONB sidecar used for forward-compatible
// fields: provision_request_id, R6 hostname, error message on failed status,
// per-service config knobs, etc. Concrete consumers should extract typed
// values rather than poking at the map directly so we can move fields into
// first-class columns later without breaking API.
type OrgServiceMeta map[string]any

// Value implements driver.Valuer so GORM can persist OrgServiceMeta to JSONB.
func (m OrgServiceMeta) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner for OrgServiceMeta. Accepts both []byte (the
// pgx default for JSONB) and string (sqlite/test fallback). Empty input
// becomes an empty map rather than nil to keep handler nil-checks simple.
func (m *OrgServiceMeta) Scan(src any) error {
	if src == nil {
		*m = OrgServiceMeta{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("OrgServiceMeta.Scan: unsupported source type")
	}
	if len(b) == 0 {
		*m = OrgServiceMeta{}
		return nil
	}
	return json.Unmarshal(b, m)
}

// UsageEvent is a single completed agent run reported back by a kova worker.
//
// Append-only. The aggregation / billing roll-up runs on a separate cadence
// (out of scope for this slice) and is implemented as a query, not a writer.
//
// CostMicros uses 1e-6 USD units so we can hold both per-token markup and
// bulk monthly totals in BIGINT without floating-point drift.
type UsageEvent struct {
	ID          int64           `json:"id"           gorm:"primaryKey;autoIncrement"`
	OrgID       int64           `json:"org_id"       gorm:"not null;index"`
	Service     string          `json:"service"      gorm:"not null"`
	TesterName  string          `json:"tester_name"`
	AgentID     string          `json:"agent_id"`
	TokensIn    int64           `json:"tokens_in"    gorm:"not null;default:0"`
	TokensOut   int64           `json:"tokens_out"   gorm:"not null;default:0"`
	CostMicros  int64           `json:"cost_micros"  gorm:"not null;default:0"`
	OccurredAt  time.Time       `json:"occurred_at"  gorm:"not null"`
	ReceivedAt  time.Time       `json:"received_at"  gorm:"autoCreateTime"`
	Metadata    OrgServiceMeta  `json:"metadata"     gorm:"type:jsonb;not null;default:'{}'"`
}

// TableName binds to billing.usage_events (see migration 029).
func (UsageEvent) TableName() string { return "billing.usage_events" }
