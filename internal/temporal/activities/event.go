package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// EventPublisher is the interface for publishing NATS events.
type EventPublisher interface {
	Publish(ctx context.Context, ev *event.IdentityEvent) error
}

// EventActivities wraps NATS event publishing for Temporal.
type EventActivities struct {
	Publisher EventPublisher
}

// PublishEventInput is the input for the PublishToNATS activity.
type PublishEventInput struct {
	Subject   string
	AccountID int64
	LurusID   string
	ProductID string
	Payload   map[string]any
}

// PublishToNATS publishes an event to the IDENTITY_EVENTS stream.
func (a *EventActivities) PublishToNATS(ctx context.Context, in PublishEventInput) error {
	if a.Publisher == nil {
		return nil // graceful degradation
	}
	ev, err := event.NewEvent(in.Subject, in.AccountID, in.LurusID, in.ProductID, in.Payload)
	if err != nil {
		return fmt.Errorf("build event: %w", err)
	}
	if err := a.Publisher.Publish(ctx, ev); err != nil {
		slog.Warn("activity/publish-nats: failed", "subject", in.Subject, "account_id", in.AccountID, "err", err)
		return fmt.Errorf("publish %s: %w", in.Subject, err)
	}
	slog.Info("activity/publish-nats", "subject", in.Subject, "account_id", in.AccountID)
	return nil
}
