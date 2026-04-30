package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	natsgo "github.com/nats-io/nats.go"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// TopupHandler is the callback shape for "wallet credited via topup" events.
// Implementation lives in newapi_sync.Module (C.2 step 4d) — kept as a func
// signature here so the NATS layer doesn't import the module package.
//
// Signature carries `eventID` (envelope.event_id) so the handler can
// deduplicate at-least-once redeliveries on its own (typically via Redis
// SETNX — see internal/pkg/idempotency.WebhookDeduper.WithFailClosed).
// The transport layer doesn't dedup itself; that's a business-logic policy
// and lives next to the operation it protects.
//
// Returning a non-nil error → message is NAK'd and JetStream retries up to
// MaxDeliver. Idempotency on the implementor's side is required because
// JetStream is at-least-once.
type TopupHandler func(ctx context.Context, eventID string, accountID int64, amountCNY float64) error

// Consumer subscribes to the LLM_EVENTS stream (published by lurus-api) and
// processes messages relevant to lurus-platform (VIP accumulation, etc.).
// Optionally also subscribes to IDENTITY_EVENTS topup-completed for
// downstream sync hooks (NewAPI quota mirroring).
type Consumer struct {
	js          natsgo.JetStreamContext
	vip         *app.VIPService
	onTopup     TopupHandler
}

// NewConsumer creates a NATS JetStream consumer.
func NewConsumer(nc *natsgo.Conn, vip *app.VIPService) (*Consumer, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	return &Consumer{js: js, vip: vip}, nil
}

// WithTopupHandler wires the topup-completed callback. Chainable; nil disables
// the subscription (default). Used by main.go to plug newapi_sync.Module's
// OnTopupCompleted method.
func (c *Consumer) WithTopupHandler(h TopupHandler) *Consumer {
	c.onTopup = h
	return c
}

// Run starts consuming messages until ctx is cancelled.
// If the upstream LLM_EVENTS stream does not yet exist (lurus-api not deployed),
// the consumer logs a warning and exits cleanly — the service continues running.
func (c *Consumer) Run(ctx context.Context) error {
	sub, err := c.js.QueueSubscribe(
		event.SubjectLLMUsageReported,
		"lurus-platform-llm-usage",
		func(msg *natsgo.Msg) {
			if err := c.handleLLMUsage(ctx, msg); err != nil {
				slog.Error("handle llm usage", "err", err)
				_ = msg.Nak()
				return
			}
			_ = msg.Ack()
		},
		natsgo.Durable("lurus-platform-llm-usage"),
		natsgo.AckExplicit(),
		natsgo.MaxDeliver(5),
	)
	if err != nil {
		// Graceful degradation: if the upstream stream does not exist yet,
		// warn and skip rather than crashing the service.
		if strings.Contains(err.Error(), "no stream matches subject") {
			slog.Warn("nats consumer: upstream LLM_EVENTS stream not found; VIP LLM accumulation disabled until lurus-api is deployed",
				"subject", event.SubjectLLMUsageReported)
			<-ctx.Done()
			return nil
		}
		// Also degrade gracefully on connection timeout (NATS unreachable).
		if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "timeout") {
			slog.Warn("nats consumer: subscribe failed (timeout); event consumption disabled",
				"subject", event.SubjectLLMUsageReported, "err", err)
			<-ctx.Done()
			return nil
		}
		return fmt.Errorf("subscribe %s: %w", event.SubjectLLMUsageReported, err)
	}
	defer sub.Unsubscribe()

	// Optional second subscription: IDENTITY_EVENTS / topup completed.
	// Only wired when WithTopupHandler was called (nil = skip). Failures
	// to subscribe degrade gracefully — the LLM-usage path still works.
	if c.onTopup != nil {
		topupSub, terr := c.js.QueueSubscribe(
			event.SubjectTopupCompleted,
			"lurus-platform-topup-newapi-sync",
			func(msg *natsgo.Msg) {
				if err := c.handleTopup(ctx, msg); err != nil {
					slog.Warn("handle topup", "err", err)
					_ = msg.Nak()
					return
				}
				_ = msg.Ack()
			},
			natsgo.Durable("lurus-platform-topup-newapi-sync"),
			natsgo.AckExplicit(),
			natsgo.MaxDeliver(5),
		)
		if terr != nil {
			slog.Warn("nats consumer: topup subscription failed; newapi quota sync disabled",
				"subject", event.SubjectTopupCompleted, "err", terr)
		} else {
			defer topupSub.Unsubscribe()
		}
	}

	<-ctx.Done()
	return ctx.Err()
}

// handleTopup parses an IDENTITY_EVENTS envelope carrying a TopupCompleted
// payload and forwards (account_id, amount_cny) to the registered handler.
// Malformed messages are dropped (Ack returned by caller) so they don't
// trigger redelivery storms; only handler errors trigger NAK.
func (c *Consumer) handleTopup(ctx context.Context, msg *natsgo.Msg) error {
	if c.onTopup == nil {
		return nil
	}
	var env event.IdentityEvent
	if err := json.Unmarshal(msg.Data, &env); err != nil {
		slog.Warn("topup: drop malformed envelope", "err", err, "len", len(msg.Data))
		return nil // ack; replaying won't help
	}
	if env.AccountID <= 0 {
		return nil
	}
	var p struct {
		AmountCNY float64 `json:"amount_cny"`
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		slog.Warn("topup: drop malformed payload", "err", err, "event_id", env.EventID)
		return nil
	}
	return c.onTopup(ctx, env.EventID, env.AccountID, p.AmountCNY)
}

func (c *Consumer) handleLLMUsage(ctx context.Context, msg *natsgo.Msg) error {
	var payload event.LLMUsageReportedPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	if payload.AccountID <= 0 {
		return nil // ignore invalid messages
	}
	return c.vip.RecalculateFromWallet(ctx, payload.AccountID)
}
