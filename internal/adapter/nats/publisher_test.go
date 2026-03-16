package nats

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// panicJetStream embeds natsgo.JetStreamContext so we only override the methods we use.
// Calling any unimplemented method panics, which is acceptable in unit tests.
type panicJetStream struct {
	natsgo.JetStreamContext
	published  []publishedMsg
	publishErr error
}

type publishedMsg struct {
	subject string
	data    []byte
}

func (m *panicJetStream) Publish(subj string, data []byte, opts ...natsgo.PubOpt) (*natsgo.PubAck, error) {
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	m.published = append(m.published, publishedMsg{subject: subj, data: data})
	return &natsgo.PubAck{Stream: event.StreamIdentityEvents, Sequence: uint64(len(m.published))}, nil
}

func (m *panicJetStream) AddStream(cfg *natsgo.StreamConfig, opts ...natsgo.JSOpt) (*natsgo.StreamInfo, error) {
	return &natsgo.StreamInfo{Config: *cfg}, nil
}

func (m *panicJetStream) StreamInfo(stream string, opts ...natsgo.JSOpt) (*natsgo.StreamInfo, error) {
	return &natsgo.StreamInfo{}, nil
}

// newTestPublisher creates a Publisher backed by the panicJetStream mock.
func newTestPublisher(mock *panicJetStream) *Publisher {
	return &Publisher{js: mock}
}

// TestNATSPublisher_Publish_Success verifies that a well-formed event is published.
func TestNATSPublisher_Publish_Success(t *testing.T) {
	mock := &panicJetStream{}
	p := newTestPublisher(mock)

	ev, err := event.NewEvent(
		event.SubjectAccountCreated,
		42,
		"L-0000000042",
		"",
		map[string]string{"email": "user@example.com"},
	)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}

	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(mock.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(mock.published))
	}
	if mock.published[0].subject != event.SubjectAccountCreated {
		t.Errorf("subject = %q, want %q", mock.published[0].subject, event.SubjectAccountCreated)
	}
}

// TestNATSPublisher_Publish_PayloadIntegrity verifies the published payload is valid JSON.
func TestNATSPublisher_Publish_PayloadIntegrity(t *testing.T) {
	mock := &panicJetStream{}
	p := newTestPublisher(mock)

	ev, _ := event.NewEvent(event.SubjectTopupCompleted, 1, "L-0000000001", "", event.TopupCompletedPayload{
		PaymentOrderID: 100,
		AmountCNY:      99.99,
		CreditsAdded:   99.99,
	})

	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var decoded event.IdentityEvent
	if err := json.Unmarshal(mock.published[0].data, &decoded); err != nil {
		t.Fatalf("published data is not valid JSON: %v", err)
	}
	if decoded.AccountID != 1 {
		t.Errorf("AccountID = %d, want 1", decoded.AccountID)
	}
	if decoded.EventID == "" {
		t.Error("EventID should not be empty")
	}
}

// TestNATSPublisher_Publish_JetStreamError verifies that JetStream errors are propagated.
func TestNATSPublisher_Publish_JetStreamError(t *testing.T) {
	mock := &panicJetStream{publishErr: natsgo.ErrConnectionClosed}
	p := newTestPublisher(mock)

	ev, _ := event.NewEvent(event.SubjectAccountCreated, 1, "L-0000000001", "", nil)
	if err := p.Publish(context.Background(), ev); err == nil {
		t.Error("expected error on JetStream failure, got nil")
	}
}

// TestNATSPublisher_Publish_MultipleEvents verifies that multiple events are published in order.
func TestNATSPublisher_Publish_MultipleEvents(t *testing.T) {
	mock := &panicJetStream{}
	p := newTestPublisher(mock)

	subjects := []string{
		event.SubjectAccountCreated,
		event.SubjectSubscriptionActivated,
		event.SubjectTopupCompleted,
	}

	for _, subj := range subjects {
		ev, _ := event.NewEvent(subj, 1, "L-0000000001", "product-a", nil)
		if err := p.Publish(context.Background(), ev); err != nil {
			t.Fatalf("Publish(%q): %v", subj, err)
		}
	}

	if len(mock.published) != len(subjects) {
		t.Fatalf("expected %d messages, got %d", len(subjects), len(mock.published))
	}
	for i, msg := range mock.published {
		if msg.subject != subjects[i] {
			t.Errorf("message %d: subject = %q, want %q", i, msg.subject, subjects[i])
		}
	}
}

// TestNATSPublisher_Publish_EventTimestamp verifies that OccurredAt is set within a reasonable range.
func TestNATSPublisher_Publish_EventTimestamp(t *testing.T) {
	mock := &panicJetStream{}
	p := newTestPublisher(mock)

	before := time.Now()
	ev, _ := event.NewEvent(event.SubjectVIPLevelChanged, 1, "L-0000000001", "", nil)
	_ = p.Publish(context.Background(), ev)
	after := time.Now()

	if ev.OccurredAt.Before(before) || ev.OccurredAt.After(after) {
		t.Errorf("OccurredAt = %v, want between %v and %v", ev.OccurredAt, before, after)
	}
}

// TestNATSPublisher_Publish_AllEventTypes verifies that all defined event types can be published.
func TestNATSPublisher_Publish_AllEventTypes(t *testing.T) {
	subjects := []string{
		event.SubjectAccountCreated,
		event.SubjectSubscriptionActivated,
		event.SubjectSubscriptionExpired,
		event.SubjectTopupCompleted,
		event.SubjectEntitlementUpdated,
		event.SubjectVIPLevelChanged,
	}

	for _, subj := range subjects {
		t.Run(subj, func(t *testing.T) {
			mock := &panicJetStream{}
			p := newTestPublisher(mock)
			ev, err := event.NewEvent(subj, 1, "L-0000000001", "p1", nil)
			if err != nil {
				t.Fatalf("NewEvent: %v", err)
			}
			if err := p.Publish(context.Background(), ev); err != nil {
				t.Errorf("Publish(%q): %v", subj, err)
			}
		})
	}
}
