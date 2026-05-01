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

	SubjectAccountCreated         = "identity.account.created"
	SubjectAccountDeleteRequested = "identity.account.delete_requested"
	SubjectAccountDeleted         = "identity.account.deleted"
	SubjectSubscriptionActivated  = "identity.subscription.activated"
	SubjectSubscriptionExpired    = "identity.subscription.expired"
	SubjectTopupCompleted         = "identity.topup.completed"
	SubjectEntitlementUpdated     = "identity.entitlement.updated"
	SubjectVIPLevelChanged        = "identity.vip.level_changed"
	SubjectOrgMemberJoined        = "identity.org.member_joined"

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

// AccountDeleteRequestedPayload is the payload for
// identity.account.delete_requested. Emitted by both the user-self
// flow (POST /api/v1/account/me/delete-request) and the admin
// QR-delegate flow at the moment the destructive intent is
// registered. Consumed by notification to surface the cooling-off
// reminder + a "cancel before {date}" deep-link in the APP.
type AccountDeleteRequestedPayload struct {
	// RequestID is the int64 PK of the row in
	// identity.account_delete_requests. Stringified at the wire is
	// fine — int64 round-trips cleanly through JSON for ids well below
	// 2^53. Kept as int64 so receivers do not need to re-parse.
	RequestID int64 `json:"request_id"`
	// Reason is the closed-enum reason code (no_longer_using, ...).
	// Empty string when the user submitted no reason.
	Reason string `json:"reason,omitempty"`
	// CoolingOffUntil is the RFC3339 timestamp when the cron worker
	// becomes eligible to dispatch the cascade. Used by the APP to
	// render the "您的账号将于 {date} 注销" body.
	CoolingOffUntil string `json:"cooling_off_until"`
}

// AccountDeletedPayload is the payload for identity.account.deleted.
// Emitted by account_purge_worker after the platform-side PIPL §47
// cascade (wallet zero, subscription cancel, Zitadel deactivate, …)
// completes successfully — i.e. AFTER MarkCompleted lands.
//
// Downstream subscribers (newapi / memorus / lucrum / tally) MUST
// idempotently delete personal data tied to the AccountID. NATS
// delivery is at-least-once, so consumers MUST tolerate "already
// deleted" gracefully (a no-op replay).
//
// The payload is deliberately minimal: AccountID + LurusID are
// already on the IdentityEvent envelope; PurgedAt records when the
// cascade-success bookkeeping landed. NO personal data here — the
// whole point of the event is to trigger downstream deletion of
// personal data, so including any here would just leak it again
// into NATS audit logs.
type AccountDeletedPayload struct {
	// PurgedAt is the RFC3339 timestamp when MarkCompleted ran. Receivers
	// can use it to bound replay decisions (e.g. ignore events older than
	// their own retention window).
	PurgedAt string `json:"purged_at"`
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
	AccountID  int64   `json:"account_id"`
	LurusID    string  `json:"lurus_id"`
	AmountCNY  float64 `json:"amount_cny"`
	TokensUsed int64   `json:"tokens_used"`
	ModelName  string  `json:"model_name"`
}
