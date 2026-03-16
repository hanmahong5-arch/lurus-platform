// Package nats provides NATS JetStream publisher and consumer for lurus-platform.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// Publisher publishes events to the IDENTITY_EVENTS JetStream stream.
type Publisher struct {
	js natsgo.JetStreamContext
}

// NewPublisher connects to NATS and ensures the IDENTITY_EVENTS stream exists.
func NewPublisher(nc *natsgo.Conn) (*Publisher, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	// Idempotent stream creation
	_, err = js.AddStream(&natsgo.StreamConfig{
		Name:       event.StreamIdentityEvents,
		Subjects:   []string{"identity.>"},
		MaxAge:     7 * 24 * time.Hour,
		Retention:  natsgo.LimitsPolicy,
		Storage:    natsgo.FileStorage,
		Replicas:   1,
	})
	if err != nil && err != natsgo.ErrStreamNameAlreadyInUse {
		// Stream may already exist with identical config — check
		if _, infoErr := js.StreamInfo(event.StreamIdentityEvents); infoErr != nil {
			return nil, fmt.Errorf("ensure stream: %w", err)
		}
	}
	return &Publisher{js: js}, nil
}

// Publish serialises and publishes an IdentityEvent.
func (p *Publisher) Publish(ctx context.Context, ev *event.IdentityEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	ack, err := p.js.Publish(ev.EventType, b)
	if err != nil {
		return fmt.Errorf("publish %s: %w", ev.EventType, err)
	}
	slog.Info("event published",
		"subject", ev.EventType,
		"event_id", ev.EventID,
		"seq", ack.Sequence,
	)
	return nil
}
