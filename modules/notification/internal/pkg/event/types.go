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
	SubjectStrategyTriggered = "lucrum.strategy.triggered"
	SubjectRiskAlert         = "lucrum.risk.alert"

	// LLM_EVENTS subjects
	SubjectQuotaThreshold = "llm.quota.threshold"
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
