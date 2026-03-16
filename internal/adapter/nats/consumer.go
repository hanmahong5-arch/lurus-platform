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

// Consumer subscribes to the LLM_EVENTS stream (published by lurus-api) and
// processes messages relevant to lurus-platform (VIP accumulation, etc.).
type Consumer struct {
	js  natsgo.JetStreamContext
	vip *app.VIPService
}

// NewConsumer creates a NATS JetStream consumer.
func NewConsumer(nc *natsgo.Conn, vip *app.VIPService) (*Consumer, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	return &Consumer{js: js, vip: vip}, nil
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

	<-ctx.Done()
	return ctx.Err()
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
