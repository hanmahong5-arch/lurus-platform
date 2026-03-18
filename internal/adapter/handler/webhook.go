package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.temporal.io/sdk/client"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/workflows"
)

// WebhookHandler handles inbound payment provider callbacks.
type WebhookHandler struct {
	wallets        *app.WalletService
	subs           *app.SubscriptionService
	epay           *payment.EpayProvider
	stripe         *payment.StripeProvider
	creem          *payment.CreemProvider
	deduper        *idempotency.WebhookDeduper
	temporalClient client.Client // nil when Temporal disabled
}

func NewWebhookHandler(
	wallets *app.WalletService,
	subs *app.SubscriptionService,
	epay *payment.EpayProvider,
	stripe *payment.StripeProvider,
	creem *payment.CreemProvider,
	deduper *idempotency.WebhookDeduper,
) *WebhookHandler {
	return &WebhookHandler{
		wallets: wallets,
		subs:    subs,
		epay:    epay,
		stripe:  stripe,
		creem:   creem,
		deduper: deduper,
	}
}

// WithTemporalClient sets the Temporal client for workflow-based payment processing.
func (h *WebhookHandler) WithTemporalClient(c client.Client) *WebhookHandler {
	h.temporalClient = c
	return h
}

// EpayNotify handles 易支付 async callback via GET.
// GET /webhook/epay
func (h *WebhookHandler) EpayNotify(c *gin.Context) {
	params := c.Request.URL.Query()
	tradeNo := params.Get("trade_no")

	// Deduplication: use the provider trade number as event ID.
	if err := h.deduper.TryProcess(c.Request.Context(), tradeNo); err != nil {
		if errors.Is(err, idempotency.ErrAlreadyProcessed) {
			slog.Info("webhook/epay: duplicate event, skipping", "trade_no", tradeNo)
			c.String(http.StatusOK, "success")
			return
		}
	}

	if h.epay == nil {
		c.String(http.StatusServiceUnavailable, "fail")
		return
	}

	orderNo, ok := h.epay.VerifyCallback(params)
	if !ok {
		slog.Warn("webhook/epay: signature verification failed", "trade_no", tradeNo)
		metrics.RecordWebhookEvent("epay", "invalid_signature")
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if err := h.processOrderPaid(c, orderNo, "epay"); err != nil {
		slog.Error("webhook/epay: process order failed", "order_no", orderNo, "err", err)
		metrics.RecordWebhookEvent("epay", "error")
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	metrics.RecordWebhookEvent("epay", "success")
	c.String(http.StatusOK, "success")
}

// StripeWebhook handles Stripe webhook events.
// POST /webhook/stripe
func (h *WebhookHandler) StripeWebhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}

	if h.stripe == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "stripe not configured"})
		return
	}

	sig := c.GetHeader("Stripe-Signature")
	orderNo, eventID, ok := h.stripe.VerifyWebhook(body, sig)
	if !ok {
		slog.Warn("webhook/stripe: signature verification failed")
		metrics.RecordWebhookEvent("stripe", "invalid_signature")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stripe signature"})
		return
	}

	// Use Stripe's stable event ID for deduplication (not the signature header).
	if err := h.deduper.TryProcess(c.Request.Context(), eventID); err != nil {
		if errors.Is(err, idempotency.ErrAlreadyProcessed) {
			slog.Info("webhook/stripe: duplicate event, skipping", "event_id", eventID)
			c.JSON(http.StatusOK, gin.H{"received": true})
			return
		}
	}

	if orderNo != "" {
		if err := h.processOrderPaid(c, orderNo, "stripe"); err != nil {
			slog.Error("webhook/stripe: process order failed", "order_no", orderNo, "err", err)
			metrics.RecordWebhookEvent("stripe", "error")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "order processing failed"})
			return
		}
	}
	metrics.RecordWebhookEvent("stripe", "success")
	c.JSON(http.StatusOK, gin.H{"received": true})
}

// CreemWebhook handles Creem webhook events.
// POST /webhook/creem
func (h *WebhookHandler) CreemWebhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}

	if h.creem == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "creem not configured"})
		return
	}

	sig := c.GetHeader("X-Creem-Signature")
	orderNo, ok := h.creem.VerifyWebhook(body, sig)
	if !ok {
		slog.Warn("webhook/creem: signature verification failed")
		metrics.RecordWebhookEvent("creem", "invalid_signature")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid creem signature"})
		return
	}

	// Extract Creem event ID from payload for deduplication.
	var payload struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(body, &payload)
	if err := h.deduper.TryProcess(c.Request.Context(), payload.EventID); err != nil {
		if errors.Is(err, idempotency.ErrAlreadyProcessed) {
			slog.Info("webhook/creem: duplicate event, skipping", "event_id", payload.EventID)
			c.JSON(http.StatusOK, gin.H{"received": true})
			return
		}
	}

	if orderNo != "" {
		if err := h.processOrderPaid(c, orderNo, "creem"); err != nil {
			slog.Error("webhook/creem: process order failed", "order_no", orderNo, "err", err)
			metrics.RecordWebhookEvent("creem", "error")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "order processing failed"})
			return
		}
	}
	metrics.RecordWebhookEvent("creem", "success")
	c.JSON(http.StatusOK, gin.H{"received": true})
}

// processOrderPaid marks an order as paid and handles subscription activation if needed.
// When Temporal is enabled, it starts a PaymentCompletionWorkflow (idempotent via workflow ID).
// Otherwise falls back to the direct synchronous path.
func (h *WebhookHandler) processOrderPaid(c *gin.Context, orderNo string, provider string) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(c.Request.Context(), "webhook.process_order")
	defer span.End()
	span.SetAttributes(
		attribute.String("order.no", orderNo),
		attribute.String("payment.provider", provider),
	)
	c.Request = c.Request.WithContext(ctx)

	if h.temporalClient != nil {
		return h.processOrderPaidTemporal(c, orderNo, provider)
	}
	return h.processOrderPaidDirect(c, orderNo)
}

// processOrderPaidTemporal starts a Temporal workflow for reliable post-payment processing.
func (h *WebhookHandler) processOrderPaidTemporal(c *gin.Context, orderNo, provider string) error {
	_, err := h.temporalClient.ExecuteWorkflow(c.Request.Context(), client.StartWorkflowOptions{
		ID:        fmt.Sprintf("payment:%s", orderNo),
		TaskQueue: activities.TaskQueue,
	}, workflows.PaymentCompletionWorkflow, workflows.PaymentInput{
		OrderNo:  orderNo,
		Provider: provider,
	})
	if err != nil {
		slog.Error("webhook/temporal: start payment workflow failed", "order_no", orderNo, "provider", provider, "err", err)
		return fmt.Errorf("start payment workflow: %w", err)
	}
	slog.Info("webhook/temporal: payment workflow started", "order_no", orderNo, "provider", provider, "workflow_id", fmt.Sprintf("payment:%s", orderNo))
	return nil
}

// processOrderPaidDirect is the original synchronous processing path (fallback).
func (h *WebhookHandler) processOrderPaidDirect(c *gin.Context, orderNo string) error {
	order, err := h.wallets.MarkOrderPaid(c.Request.Context(), orderNo)
	if err != nil {
		return err
	}
	if order != nil && order.OrderType == "subscription" && order.PlanID != nil && order.ProductID != "" {
		_, err = h.subs.Activate(c.Request.Context(), order.AccountID, order.ProductID, *order.PlanID, order.PaymentMethod, order.ExternalID)
		return err
	}
	return nil
}
