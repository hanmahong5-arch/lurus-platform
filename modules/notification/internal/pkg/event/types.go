// Package event defines NATS event types consumed by the notification service.
package event

import (
	"encoding/json"
	"time"
)

// Stream constants.
const (
	StreamIdentityEvents = "IDENTITY_EVENTS"
	StreamLucrumEvents   = "LUCRUM_EVENTS"
	StreamLLMEvents      = "LLM_EVENTS"
	StreamPSIEvents      = "PSI_EVENTS"
)

// Subject constants consumed from upstream services.
const (
	// IDENTITY_EVENTS subjects
	SubjectAccountCreated        = "identity.account.created"
	SubjectSubscriptionActivated = "identity.subscription.activated"
	SubjectSubscriptionExpired   = "identity.subscription.expired"
	SubjectTopupCompleted        = "identity.topup.completed"
	SubjectVIPLevelChanged       = "identity.vip.level_changed"

	// LUCRUM_EVENTS subjects
	SubjectStrategyTriggered   = "lucrum.strategy.triggered"
	SubjectRiskAlert           = "lucrum.risk.alert"
	SubjectLucrumAdvisorOutput = "lucrum.advisor.output"
	SubjectLucrumMarketEvent   = "lucrum.market.event"

	// LLM_EVENTS subjects
	SubjectQuotaThreshold    = "llm.quota.threshold"
	SubjectLLMImageGenerated = "llm.image.generated"
	SubjectLLMUsageMilestone = "llm.usage.milestone"

	// PSI_EVENTS subjects
	SubjectPSIOrderApprovalNeeded = "psi.order.approval_needed"
	SubjectPSIInventoryRedline    = "psi.inventory.redline"
	SubjectPSIPaymentReceived     = "psi.payment.received"
)

// IdentityEvent is the standard envelope from IDENTITY_EVENTS stream.
type IdentityEvent struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	AccountID  int64           `json:"account_id"`
	LurusID    string          `json:"lurus_id"`
	ProductID  string          `json:"product_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// LucrumEvent is the standard envelope from LUCRUM_EVENTS stream.
type LucrumEvent struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	UserID     string          `json:"user_id"`
	AccountID  int64           `json:"account_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// SubscriptionActivatedPayload from identity.subscription.activated.
type SubscriptionActivatedPayload struct {
	SubscriptionID int64  `json:"subscription_id"`
	PlanCode       string `json:"plan_code"`
	ExpiresAt      string `json:"expires_at"`
}

// TopupCompletedPayload from identity.topup.completed.
type TopupCompletedPayload struct {
	PaymentOrderID int64   `json:"payment_order_id"`
	AmountCNY      float64 `json:"amount_cny"`
	CreditsAdded   float64 `json:"credits_added"`
}

// StrategyTriggeredPayload from lucrum.strategy.triggered.
type StrategyTriggeredPayload struct {
	StrategyID   string `json:"strategy_id"`
	StrategyName string `json:"strategy_name"`
	Signal       string `json:"signal"` // "buy", "sell", "close"
	Symbol       string `json:"symbol"`
}

// RiskAlertPayload from lucrum.risk.alert.
type RiskAlertPayload struct {
	AlertType string  `json:"alert_type"` // "drawdown", "position_limit", "volatility"
	Symbol    string  `json:"symbol"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Message   string  `json:"message"`
}

// QuotaThresholdPayload from llm.quota.threshold.
type QuotaThresholdPayload struct {
	AccountID    int64   `json:"account_id"`
	UsedTokens   int64   `json:"used_tokens"`
	LimitTokens  int64   `json:"limit_tokens"`
	UsagePercent float64 `json:"usage_percent"` // 0-100
}

// LLMEvent is the standard envelope from LLM_EVENTS stream for events that
// carry an account_id + structured payload (image gen, usage milestones).
// Note: legacy llm.quota.threshold flattens its fields into QuotaThresholdPayload
// directly without an envelope; new events follow the envelope shape below.
type LLMEvent struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	AccountID  int64           `json:"account_id"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// PSIEvent is the standard envelope from PSI_EVENTS stream.
// PSI events are workspace-scoped at source; the producer must resolve
// the target account_id before publishing (PSI has its own
// workspace_members.account_id mapping). If account_id <= 0 the
// notification consumer drops the event with a structured log line.
type PSIEvent struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	AccountID  int64           `json:"account_id"`
	WorkspaceID int64          `json:"workspace_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}

// LLMImageGeneratedPayload from llm.image.generated.
type LLMImageGeneratedPayload struct {
	JobID    string `json:"job_id"`
	ImageURL string `json:"image_url"`
	Prompt   string `json:"prompt"`
}

// LLMUsageMilestonePayload from llm.usage.milestone.
type LLMUsageMilestonePayload struct {
	Period     string `json:"period"` // "day" | "month"
	TokensUsed int64  `json:"tokens_used"`
	Milestone  string `json:"milestone"` // human-readable bucket label
}

// LucrumAdvisorOutputPayload from lucrum.advisor.output.
type LucrumAdvisorOutputPayload struct {
	AdvisorID   string `json:"advisor_id"`
	AdvisorName string `json:"advisor_name"`
	Symbol      string `json:"symbol"`
	Summary     string `json:"summary"`
}

// LucrumMarketEventPayload from lucrum.market.event.
type LucrumMarketEventPayload struct {
	Symbol   string `json:"symbol"`
	Headline string `json:"headline"`
	Severity string `json:"severity"` // "info" | "warning" | "critical"
}

// VIPLevelChangedPayload from identity.vip.level_changed.
type VIPLevelChangedPayload struct {
	Level    string `json:"level"`
	OldLevel string `json:"old_level,omitempty"`
}

// PSIOrderApprovalPayload from psi.order.approval_needed.
type PSIOrderApprovalPayload struct {
	OrderID     int64   `json:"order_id"`
	OrderNo     string  `json:"order_no"`
	AmountCNY   float64 `json:"amount_cny"`
	SubmittedBy string  `json:"submitted_by"`
}

// PSIInventoryRedlinePayload from psi.inventory.redline.
type PSIInventoryRedlinePayload struct {
	SKU       string `json:"sku"`
	SKUName   string `json:"sku_name"`
	OnHand    int    `json:"on_hand"`
	Threshold int    `json:"threshold"`
}

// PSIPaymentReceivedPayload from psi.payment.received.
type PSIPaymentReceivedPayload struct {
	PaymentID int64   `json:"payment_id"`
	AmountCNY float64 `json:"amount_cny"`
	PayerName string  `json:"payer_name"`
}
