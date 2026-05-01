// Package nats provides NATS JetStream consumers for the notification service.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/event"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/metrics"
)

// Consumer subscribes to IDENTITY_EVENTS, LUCRUM_EVENTS, and LLM_EVENTS
// and dispatches notifications accordingly.
type Consumer struct {
	js       natsgo.JetStreamContext
	notifSvc *app.NotificationService
	rdb      *redis.Client
}

// NewConsumer creates a NATS JetStream consumer.
func NewConsumer(nc *natsgo.Conn, notifSvc *app.NotificationService, rdb *redis.Client) (*Consumer, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	return &Consumer{js: js, notifSvc: notifSvc, rdb: rdb}, nil
}

// subscription defines a NATS subject to listen on and how to handle it.
type subscription struct {
	subject string
	queue   string
	handler func(ctx context.Context, msg *natsgo.Msg) error
}

// Run starts all consumers and blocks until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	subs := []subscription{
		// IDENTITY_EVENTS
		{
			subject: event.SubjectAccountCreated,
			queue:   "notif-account-created",
			handler: c.handleAccountCreated,
		},
		{
			subject: event.SubjectAccountDeleteRequested,
			queue:   "notif-account-delete-requested",
			handler: c.handleAccountDeleteRequested,
		},
		{
			subject: event.SubjectSubscriptionActivated,
			queue:   "notif-sub-activated",
			handler: c.handleSubscriptionActivated,
		},
		{
			subject: event.SubjectSubscriptionExpired,
			queue:   "notif-sub-expired",
			handler: c.handleSubscriptionExpired,
		},
		{
			subject: event.SubjectTopupCompleted,
			queue:   "notif-topup-completed",
			handler: c.handleTopupCompleted,
		},
		{
			subject: event.SubjectVIPLevelChanged,
			queue:   "notif-vip-level-changed",
			handler: c.handleVIPLevelChanged,
		},
		// LUCRUM_EVENTS
		{
			subject: event.SubjectStrategyTriggered,
			queue:   "notif-strategy-triggered",
			handler: c.handleStrategyTriggered,
		},
		{
			subject: event.SubjectRiskAlert,
			queue:   "notif-risk-alert",
			handler: c.handleRiskAlert,
		},
		{
			subject: event.SubjectLucrumAdvisorOutput,
			queue:   "notif-lucrum-advisor-output",
			handler: c.handleLucrumAdvisorOutput,
		},
		{
			subject: event.SubjectLucrumMarketEvent,
			queue:   "notif-lucrum-market-event",
			handler: c.handleLucrumMarketEvent,
		},
		// LLM_EVENTS
		{
			subject: event.SubjectQuotaThreshold,
			queue:   "notif-quota-threshold",
			handler: c.handleQuotaThreshold,
		},
		{
			subject: event.SubjectLLMImageGenerated,
			queue:   "notif-llm-image-generated",
			handler: c.handleLLMImageGenerated,
		},
		// TODO(spec E.4 Q2): llm.usage.milestone is published per-event today.
		// If volume becomes a problem, route through digest_worker instead of
		// fanning out a notification per milestone crossing.
		{
			subject: event.SubjectLLMUsageMilestone,
			queue:   "notif-llm-usage-milestone",
			handler: c.handleLLMUsageMilestone,
		},
		// PSI_EVENTS
		{
			subject: event.SubjectPSIOrderApprovalNeeded,
			queue:   "notif-psi-order-approval",
			handler: c.handlePSIOrderApprovalNeeded,
		},
		{
			subject: event.SubjectPSIInventoryRedline,
			queue:   "notif-psi-inventory-redline",
			handler: c.handlePSIInventoryRedline,
		},
		{
			subject: event.SubjectPSIPaymentReceived,
			queue:   "notif-psi-payment-received",
			handler: c.handlePSIPaymentReceived,
		},
	}

	var activeSubs []*natsgo.Subscription
	for _, s := range subs {
		sub, err := c.subscribe(ctx, s)
		if err != nil {
			slog.Warn("nats consumer: failed to subscribe, will skip",
				"subject", s.subject, "err", err)
			continue
		}
		activeSubs = append(activeSubs, sub)
	}

	slog.Info("nats consumer started", "active_subscriptions", len(activeSubs))
	<-ctx.Done()

	for _, sub := range activeSubs {
		_ = sub.Unsubscribe()
	}
	return ctx.Err()
}

func (c *Consumer) subscribe(ctx context.Context, s subscription) (*natsgo.Subscription, error) {
	sub, err := c.js.QueueSubscribe(
		s.subject,
		s.queue,
		func(msg *natsgo.Msg) {
			if err := s.handler(ctx, msg); err != nil {
				metrics.NATSEventsTotal.WithLabelValues(s.subject, "error").Inc()
				slog.Error("notification handler error",
					"subject", s.subject,
					"err", err,
				)
				_ = msg.Nak()
				return
			}
			metrics.NATSEventsTotal.WithLabelValues(s.subject, "success").Inc()
			_ = msg.Ack()
		},
		natsgo.Durable(s.queue),
		natsgo.AckExplicit(),
		natsgo.MaxDeliver(5),
	)
	if err != nil {
		// Graceful degradation: if the upstream stream does not exist yet, skip.
		if strings.Contains(err.Error(), "no stream matches subject") {
			slog.Warn("nats consumer: upstream stream not found",
				"subject", s.subject)
			return nil, err
		}
		return nil, fmt.Errorf("subscribe %s: %w", s.subject, err)
	}
	slog.Info("subscribed", "subject", s.subject, "queue", s.queue)
	return sub, nil
}

func (c *Consumer) handleAccountCreated(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.IdentityEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal account.created: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectAccountCreated,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelEmail},
		Vars: map[string]string{
			"lurus_id": ev.LurusID,
		},
	})
}

// accountDeleteBodyFormat is the body template for the
// account.delete_requested in-app notification. The %s slot receives
// the cooling-off deadline rendered YYYY-MM-DD in UTC. (Switching to
// per-account TZ requires a registry lookup we don't yet have on the
// consumer; UTC is the source-of-truth on the producer side anyway.)
const accountDeleteBodyFormat = "您的账号注销将于 %s 生效，期间登录可取消。"

// buildAccountDeleteRequestedSend translates a raw
// identity.account.delete_requested NATS payload into the SendRequest
// the notification service expects. Pure function — no service or
// network calls — so the parsing + body-formatting logic can be
// unit-tested without standing up the Consumer.
//
// Returns (zero SendRequest, false) when the event should be silently
// dropped (corrupt envelope, no resolvable account, corrupt payload).
// The Consumer's caller treats the false return as "ACK and skip".
func buildAccountDeleteRequestedSend(raw []byte) (app.SendRequest, bool) {
	var ev event.IdentityEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		// Corrupt envelope. Log + drop so this event's slot in the
		// queue is reclaimed; treating as a permanent failure avoids
		// the otherwise-infinite retry that nats.MaxDeliver(5) would
		// induce on a deserialization bug.
		slog.Warn("notif: corrupt envelope account.delete_requested", "err", err)
		return app.SendRequest{}, false
	}
	if ev.AccountID <= 0 {
		_ = skipNoAccount(event.SubjectAccountDeleteRequested, ev.EventID)
		return app.SendRequest{}, false
	}
	var payload event.AccountDeleteRequestedPayload
	if len(ev.Payload) > 0 {
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			slog.Warn("notif: corrupt payload account.delete_requested",
				"account_id", ev.AccountID, "event_id", ev.EventID, "err", err)
			return app.SendRequest{}, false
		}
	}

	// Render YYYY-MM-DD from the cooling_off_until ISO8601 string.
	// On parse failure, fall back to the raw string so the user still
	// sees something — better than a "您的账号注销将于  生效" body.
	dateStr := payload.CoolingOffUntil
	if t, err := time.Parse(time.RFC3339, payload.CoolingOffUntil); err == nil {
		dateStr = t.UTC().Format("2006-01-02")
	}

	return app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectAccountDeleteRequested,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		// Title / body / category / priority are fully producer-known
		// strings — no template-DB row needed. See SendRequest doc for
		// the override semantics.
		Title:    "您已提交注销请求",
		Body:     fmt.Sprintf(accountDeleteBodyFormat, dateStr),
		Priority: entity.PriorityNormal,
		Category: "account.delete_requested",
		Payload: map[string]any{
			"request_id":        payload.RequestID,
			"cooling_off_until": payload.CoolingOffUntil,
		},
	}, true
}

// handleAccountDeleteRequested wires buildAccountDeleteRequestedSend
// into the NATS consumer dispatch. Stays trivial so the testable
// logic lives in the pure builder above.
func (c *Consumer) handleAccountDeleteRequested(ctx context.Context, msg *natsgo.Msg) error {
	req, ok := buildAccountDeleteRequestedSend(msg.Data)
	if !ok {
		return nil
	}
	return c.notifSvc.Send(ctx, req)
}

func (c *Consumer) handleSubscriptionActivated(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.IdentityEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal subscription.activated: %w", err)
	}
	var payload event.SubscriptionActivatedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal subscription payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectSubscriptionActivated,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelEmail},
		Vars: map[string]string{
			"plan_code":  payload.PlanCode,
			"expires_at": payload.ExpiresAt,
		},
	})
}

func (c *Consumer) handleSubscriptionExpired(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.IdentityEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal subscription.expired: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectSubscriptionExpired,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelEmail},
		Vars:      map[string]string{},
	})
}

func (c *Consumer) handleTopupCompleted(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.IdentityEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal topup.completed: %w", err)
	}
	var payload event.TopupCompletedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal topup payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectTopupCompleted,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"credits_added": fmt.Sprintf("%.2f", payload.CreditsAdded),
		},
	})
}

func (c *Consumer) handleStrategyTriggered(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.LucrumEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal strategy.triggered: %w", err)
	}
	if ev.AccountID <= 0 {
		return nil // skip if no account linked
	}
	var payload event.StrategyTriggeredPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal strategy payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectStrategyTriggered,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelFCM},
		Vars: map[string]string{
			"strategy_name": payload.StrategyName,
			"signal":        payload.Signal,
			"symbol":        payload.Symbol,
		},
	})
}

func (c *Consumer) handleRiskAlert(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.LucrumEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal risk.alert: %w", err)
	}
	if ev.AccountID <= 0 {
		return nil
	}
	var payload event.RiskAlertPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal risk payload: %w", err)
	}
	// Risk alerts go to all channels (urgent)
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectRiskAlert,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelEmail, entity.ChannelFCM},
		Vars: map[string]string{
			"alert_type": payload.AlertType,
			"symbol":     payload.Symbol,
			"message":    payload.Message,
		},
	})
}

// quotaThresholds defines the alert levels and their corresponding event types.
var quotaThresholds = []struct {
	percent   float64
	eventType string
}{
	{50, "llm.quota.50"},
	{80, "llm.quota.80"},
	{95, "llm.quota.95"},
	{100, "llm.quota.100"},
}

func (c *Consumer) handleQuotaThreshold(ctx context.Context, msg *natsgo.Msg) error {
	var payload event.QuotaThresholdPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal quota.threshold: %w", err)
	}
	if payload.AccountID <= 0 {
		return nil
	}

	// Determine which threshold(s) have been crossed.
	for _, t := range quotaThresholds {
		if payload.UsagePercent < t.percent {
			continue
		}

		// Redis dedup: one alert per threshold per account per month.
		month := time.Now().Format("2006-01")
		dedupKey := fmt.Sprintf("quota_alert:%d:%.0f:%s", payload.AccountID, t.percent, month)
		if c.rdb != nil {
			set, err := c.rdb.SetNX(ctx, dedupKey, "1", 31*24*time.Hour).Result()
			if err != nil {
				slog.Warn("quota alert dedup check failed",
					"key", dedupKey, "err", err)
			} else if !set {
				// Already alerted for this threshold this month.
				continue
			}
		}

		channels := []entity.Channel{entity.ChannelInApp}
		// Add email for 80%+ thresholds.
		if t.percent >= 80 {
			channels = append(channels, entity.ChannelEmail)
		}

		remaining := payload.LimitTokens - payload.UsedTokens
		if remaining < 0 {
			remaining = 0
		}

		if err := c.notifSvc.Send(ctx, app.SendRequest{
			AccountID: payload.AccountID,
			EventType: t.eventType,
			EventID:   fmt.Sprintf("quota_%d_%.0f_%s", payload.AccountID, t.percent, month),
			Channels:  channels,
			Vars: map[string]string{
				"percent":   fmt.Sprintf("%.0f", t.percent),
				"remaining": fmt.Sprintf("%d", remaining),
			},
			Payload: map[string]any{
				"used_tokens":   payload.UsedTokens,
				"limit_tokens":  payload.LimitTokens,
				"usage_percent": payload.UsagePercent,
			},
		}); err != nil {
			slog.Error("failed to send quota alert",
				"account_id", payload.AccountID,
				"threshold", t.percent,
				"err", err,
			)
		}
	}

	return nil
}

// skipNoAccount logs a structured line and returns nil so the consumer ACKs
// events that lack a resolvable account_id (e.g. workspace-scoped PSI events
// with no member mapping yet). Mirrors the existing LUCRUM behavior.
//
// TODO(spec E.4 Q1): when the PSI workspace_members.account_id resolution
// rule is finalised, replace this no-op with a fan-out to all linked accounts.
func skipNoAccount(subject, eventID string) error {
	slog.Info("notif: skipping event with no account_id",
		"subject", subject,
		"event_id", eventID,
	)
	return nil
}

func (c *Consumer) handleVIPLevelChanged(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.IdentityEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal vip.level_changed: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectVIPLevelChanged, ev.EventID)
	}
	var payload event.VIPLevelChangedPayload
	if len(ev.Payload) > 0 {
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return fmt.Errorf("unmarshal vip payload: %w", err)
		}
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectVIPLevelChanged,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"level":     payload.Level,
			"old_level": payload.OldLevel,
		},
		Payload: map[string]any{
			"level":     payload.Level,
			"old_level": payload.OldLevel,
		},
	})
}

func (c *Consumer) handleLucrumAdvisorOutput(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.LucrumEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal lucrum.advisor.output: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectLucrumAdvisorOutput, ev.EventID)
	}
	var payload event.LucrumAdvisorOutputPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal advisor payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectLucrumAdvisorOutput,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"advisor_name": payload.AdvisorName,
			"symbol":       payload.Symbol,
			"summary":      payload.Summary,
		},
		Payload: map[string]any{
			"advisor_id":   payload.AdvisorID,
			"advisor_name": payload.AdvisorName,
			"symbol":       payload.Symbol,
			"summary":      payload.Summary,
		},
	})
}

func (c *Consumer) handleLucrumMarketEvent(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.LucrumEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal lucrum.market.event: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectLucrumMarketEvent, ev.EventID)
	}
	var payload event.LucrumMarketEventPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal market payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectLucrumMarketEvent,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"symbol":   payload.Symbol,
			"headline": payload.Headline,
		},
		Payload: map[string]any{
			"symbol":   payload.Symbol,
			"headline": payload.Headline,
			"severity": payload.Severity,
		},
	})
}

func (c *Consumer) handleLLMImageGenerated(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.LLMEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal llm.image.generated: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectLLMImageGenerated, ev.EventID)
	}
	var payload event.LLMImageGeneratedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal image payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectLLMImageGenerated,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelFCM},
		Vars: map[string]string{
			"prompt": payload.Prompt,
		},
		Payload: map[string]any{
			"job_id":    payload.JobID,
			"image_url": payload.ImageURL,
			"prompt":    payload.Prompt,
		},
	})
}

func (c *Consumer) handleLLMUsageMilestone(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.LLMEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal llm.usage.milestone: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectLLMUsageMilestone, ev.EventID)
	}
	var payload event.LLMUsageMilestonePayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal usage milestone payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectLLMUsageMilestone,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"period":      payload.Period,
			"tokens_used": fmt.Sprintf("%d", payload.TokensUsed),
			"milestone":   payload.Milestone,
		},
		Payload: map[string]any{
			"period":      payload.Period,
			"tokens_used": payload.TokensUsed,
			"milestone":   payload.Milestone,
		},
	})
}

func (c *Consumer) handlePSIOrderApprovalNeeded(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.PSIEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal psi.order.approval_needed: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectPSIOrderApprovalNeeded, ev.EventID)
	}
	var payload event.PSIOrderApprovalPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal psi order payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectPSIOrderApprovalNeeded,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp, entity.ChannelFCM},
		Vars: map[string]string{
			"order_no":     payload.OrderNo,
			"amount_cny":   fmt.Sprintf("%.2f", payload.AmountCNY),
			"submitted_by": payload.SubmittedBy,
		},
		Payload: map[string]any{
			"order_id":     payload.OrderID,
			"order_no":     payload.OrderNo,
			"amount_cny":   payload.AmountCNY,
			"submitted_by": payload.SubmittedBy,
		},
	})
}

func (c *Consumer) handlePSIInventoryRedline(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.PSIEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal psi.inventory.redline: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectPSIInventoryRedline, ev.EventID)
	}
	var payload event.PSIInventoryRedlinePayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal psi inventory payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectPSIInventoryRedline,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"sku":       payload.SKU,
			"sku_name":  payload.SKUName,
			"on_hand":   fmt.Sprintf("%d", payload.OnHand),
			"threshold": fmt.Sprintf("%d", payload.Threshold),
		},
		Payload: map[string]any{
			"sku":       payload.SKU,
			"sku_name":  payload.SKUName,
			"on_hand":   payload.OnHand,
			"threshold": payload.Threshold,
		},
	})
}

func (c *Consumer) handlePSIPaymentReceived(ctx context.Context, msg *natsgo.Msg) error {
	var ev event.PSIEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		return fmt.Errorf("unmarshal psi.payment.received: %w", err)
	}
	if ev.AccountID <= 0 {
		return skipNoAccount(event.SubjectPSIPaymentReceived, ev.EventID)
	}
	var payload event.PSIPaymentReceivedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal psi payment payload: %w", err)
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: ev.AccountID,
		EventType: event.SubjectPSIPaymentReceived,
		EventID:   ev.EventID,
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"amount_cny": fmt.Sprintf("%.2f", payload.AmountCNY),
			"payer_name": payload.PayerName,
		},
		Payload: map[string]any{
			"payment_id": payload.PaymentID,
			"amount_cny": payload.AmountCNY,
			"payer_name": payload.PayerName,
		},
	})
}
