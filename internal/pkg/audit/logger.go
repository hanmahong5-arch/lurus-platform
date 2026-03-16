// Package audit provides structured JSON audit logging for security-sensitive operations.
package audit

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Action constants for audit events.
const (
	ActionLogin              = "login"
	ActionLogout             = "logout"
	ActionAccountCreate      = "account.create"
	ActionAccountUpdate      = "account.update"
	ActionSubscriptionCreate = "subscription.create"
	ActionSubscriptionCancel = "subscription.cancel"
	ActionSubscriptionExpire = "subscription.expire"
	ActionWalletTopup        = "wallet.topup"
	ActionWalletAdjust       = "wallet.adjust"
	ActionPaymentWebhook     = "payment.webhook"
	ActionRedeemCode         = "redeem.code"
	ActionAdminGrant         = "admin.grant"
)

// Result constants.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultDenied  = "denied"
)

// Event is a structured audit record.
type Event struct {
	ActorID      string // Lurus account ID or "system" for automated actions
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	Detail       string // Optional extra context
	IP           string
}

// Logger writes structured audit events via slog.
type Logger struct {
	logger *slog.Logger
}

// New creates an audit Logger that writes to the given slog.Logger.
// If l is nil, the default slog logger is used.
func New(l *slog.Logger) *Logger {
	if l == nil {
		l = slog.Default()
	}
	return &Logger{logger: l}
}

// Log writes a structured audit entry. The ctx is used for log group propagation.
func (a *Logger) Log(ctx context.Context, ev Event) {
	a.logger.LogAttrs(ctx, slog.LevelInfo, "audit",
		slog.String("actor_id", ev.ActorID),
		slog.String("action", ev.Action),
		slog.String("resource_type", ev.ResourceType),
		slog.String("resource_id", ev.ResourceID),
		slog.String("result", ev.Result),
		slog.String("detail", ev.Detail),
		slog.String("ip", ev.IP),
		slog.Time("timestamp", time.Now().UTC()),
	)
}

// FromRequest extracts the client IP from a standard HTTP request.
func FromRequest(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
