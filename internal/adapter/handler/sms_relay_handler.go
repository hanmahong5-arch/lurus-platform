package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	appsms "github.com/hanmahong5-arch/lurus-platform/internal/app/sms"
)

// SMSRelayUsecaseIface is the interface this handler depends on.
// It is satisfied by *appsms.SMSRelayUsecase and test doubles.
type SMSRelayUsecaseIface interface {
	SendOTP(ctx context.Context, phone, code string) error
}

// SMSRelayHandler handles POST /internal/v1/sms/relay.
// It bridges Zitadel webhook SMS notifications to the SMS provider.
type SMSRelayHandler struct {
	usecase SMSRelayUsecaseIface
}

// NewSMSRelayHandler creates the handler.
func NewSMSRelayHandler(uc SMSRelayUsecaseIface) *SMSRelayHandler {
	return &SMSRelayHandler{usecase: uc}
}

// zitadelRelayPayload is the JSON body sent by Zitadel's SMS webhook.
// Field names match the Zitadel notification webhook schema.
// See: https://zitadel.com/docs/apis/resources/notification_v3
//
// NOTE: The exact field names were verified from the Zitadel webhook spec.
// Operators should register a test webhook with returnCode:true to confirm
// payload format against their Zitadel instance before enabling production.
type zitadelRelayPayload struct {
	ContextInfo struct {
		Recipient string `json:"recipient"`
		EventType string `json:"eventType"`
	} `json:"contextInfo"`
	TemplateData struct {
		Code    string `json:"code"`
		Minutes string `json:"minutes"`
	} `json:"templateData"`
	// MessageContent is the pre-rendered SMS text (optional; not sent to provider).
	MessageContent string `json:"messageContent"`
}

// Relay handles POST /internal/v1/sms/relay.
//
// Accepts a Zitadel webhook payload, extracts the recipient phone number and
// OTP code, then dispatches the SMS via the configured provider.
//
// Responses:
//
//	200 OK             — SMS sent (or noop sender accepted it).
//	400 Bad Request    — Missing recipient, missing code, or invalid phone format.
//	429 Too Many Reqs  — Provider rate limit; Retry-After: 60 header is set.
//	500 Internal       — Transient provider failure after all retries.
func (h *SMSRelayHandler) Relay(c *gin.Context) {
	var payload zitadelRelayPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		respondBadRequest(c, "Invalid JSON payload")
		return
	}

	recipient := payload.ContextInfo.Recipient
	if recipient == "" {
		respondBadRequest(c, "contextInfo.recipient is required")
		return
	}

	code := payload.TemplateData.Code
	if code == "" {
		respondBadRequest(c, "templateData.code is required")
		return
	}

	slog.Info("sms_relay",
		"recipient", recipient,
		"event_type", payload.ContextInfo.EventType,
		"request_id", c.GetString("request_id"),
	)

	err := h.usecase.SendOTP(c.Request.Context(), recipient, code)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	if errors.Is(err, appsms.ErrInvalidPhone) {
		respondBadRequest(c, err.Error())
		return
	}

	if errors.Is(err, appsms.ErrRateLimit) {
		c.Header("Retry-After", "60")
		respondError(c, http.StatusTooManyRequests, ErrCodeRateLimited,
			"SMS rate limit exceeded — please retry after 60 seconds")
		return
	}

	respondInternalError(c, "sms_relay.send", err)
}
