// Package nats provides NATS JetStream consumers for the notification service.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	natsgo "github.com/nats-io/nats.go"

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
}

// NewConsumer creates a NATS JetStream consumer.
func NewConsumer(nc *natsgo.Conn, notifSvc *app.NotificationService) (*Consumer, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	return &Consumer{js: js, notifSvc: notifSvc}, nil
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
		// LLM_EVENTS
		{
			subject: event.SubjectQuotaThreshold,
			queue:   "notif-quota-threshold",
			handler: c.handleQuotaThreshold,
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

func (c *Consumer) handleQuotaThreshold(ctx context.Context, msg *natsgo.Msg) error {
	var payload event.QuotaThresholdPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal quota.threshold: %w", err)
	}
	if payload.AccountID <= 0 {
		return nil
	}
	return c.notifSvc.Send(ctx, app.SendRequest{
		AccountID: payload.AccountID,
		EventType: event.SubjectQuotaThreshold,
		EventID:   "", // no event_id for quota warnings
		Channels:  []entity.Channel{entity.ChannelInApp},
		Vars: map[string]string{
			"usage_percent": fmt.Sprintf("%.0f%%", payload.UsagePercent),
		},
	})
}
