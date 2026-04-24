// Package event defines shared NATS event types for lurus-platform stream.
package event

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Stream and subject constants.
const (
	StreamIdentityEvents = "IDENTITY_EVENTS"

	SubjectAccountCreated        = "identity.account.created"
	SubjectSubscriptionActivated = "identity.subscription.activated"
	SubjectSubscriptionExpired   = "identity.subscription.expired"
	SubjectTopupCompleted        = "identity.topup.completed"
	SubjectEntitlementUpdated    = "identity.entitlement.updated"
	SubjectVIPLevelChanged       = "identity.vip.level_changed"
	SubjectOrgMemberJoined       = "identity.org.member_joined"

	// Consumed from LLM_EVENTS (published by lurus-api)
	SubjectLLMUsageReported = "llm.usage.reported"
)

// IdentityEvent is the standard envelope for all IDENTITY_EVENTS messages.
type IdentityEvent struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	AccountID  int64           `json:"account_id"`
	LurusID    string          `json:"lurus_id"`
	ProductID  string          `json:"product_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// NewEvent creates an IdentityEvent with a generated UUID.
func NewEvent(eventType string, accountID int64, lurusID, productID string, payload any) (*IdentityEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &IdentityEvent{
		EventID:    uuid.NewString(),
		EventType:  eventType,
		AccountID:  accountID,
		LurusID:    lurusID,
		ProductID:  productID,
		Payload:    raw,
		OccurredAt: time.Now().UTC(),
	}, nil
}

// SubscriptionActivatedPayload is the payload for identity.subscription.activated.
type SubscriptionActivatedPayload struct {
	SubscriptionID int64  `json:"subscription_id"`
	PlanCode       string `json:"plan_code"`
	ExpiresAt      string `json:"expires_at"` // RFC3339
}

// TopupCompletedPayload is the payload for identity.topup.completed.
type TopupCompletedPayload struct {
	PaymentOrderID int64   `json:"payment_order_id"`
	AmountCNY      float64 `json:"amount_cny"`
	CreditsAdded   float64 `json:"credits_added"`
}

// EntitlementUpdatedPayload is the payload for identity.entitlement.updated.
type EntitlementUpdatedPayload struct {
	Keys []string `json:"keys"` // updated entitlement keys
}

// VIPLevelChangedPayload is the payload for identity.vip.level_changed.
type VIPLevelChangedPayload struct {
	OldLevel int `json:"old_level"`
	NewLevel int `json:"new_level"`
}

// OrgMemberJoinedPayload is the payload for identity.org.member_joined.
// Emitted when an account is added to an org; ConfirmedViaQR is true when the
// add was driven by the v2 QR primitive (authed create + scan confirm).
type OrgMemberJoinedPayload struct {
	OrgID          int64  `json:"org_id"`
	AccountID      int64  `json:"account_id"`
	Role           string `json:"role"`
	JoinedAt       string `json:"joined_at"` // RFC3339
	ConfirmedViaQR bool   `json:"confirmed_via_qr"`
}

// LLMUsageReportedPayload is the payload consumed from llm.usage.reported.
type LLMUsageReportedPayload struct {
	AccountID   int64   `json:"account_id"`
	LurusID     string  `json:"lurus_id"`
	AmountCNY   float64 `json:"amount_cny"`
	TokensUsed  int64   `json:"tokens_used"`
	ModelName   string  `json:"model_name"`
}
