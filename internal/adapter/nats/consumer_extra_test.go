package nats

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// mockJetStreamConsumer overrides QueueSubscribe to simulate errors.
type mockJetStreamConsumer struct {
	natsgo.JetStreamContext
	subscribeErr error
}

func (m *mockJetStreamConsumer) QueueSubscribe(
	subj, queue string,
	cb natsgo.MsgHandler,
	opts ...natsgo.SubOpt,
) (*natsgo.Subscription, error) {
	if m.subscribeErr != nil {
		return nil, m.subscribeErr
	}
	return &natsgo.Subscription{}, nil
}

// TestConsumer_Run_NoStreamMatchesSubject verifies graceful degradation when
// the upstream LLM_EVENTS stream does not yet exist.
func TestConsumer_Run_NoStreamMatchesSubject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled so <-ctx.Done() returns immediately

	mock := &mockJetStreamConsumer{
		subscribeErr: errors.New("no stream matches subject"),
	}
	c := &Consumer{js: mock, vip: nil}

	err := c.Run(ctx)
	if err != nil {
		t.Errorf("expected nil on 'no stream matches subject', got: %v", err)
	}
}

// TestConsumer_Run_TimeoutDeadlineExceeded verifies graceful degradation on
// context deadline exceeded error string.
func TestConsumer_Run_TimeoutDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockJetStreamConsumer{
		subscribeErr: errors.New("context deadline exceeded"),
	}
	c := &Consumer{js: mock, vip: nil}

	err := c.Run(ctx)
	if err != nil {
		t.Errorf("expected nil on 'context deadline exceeded', got: %v", err)
	}
}

// TestConsumer_Run_TimeoutWord verifies "timeout" substring also triggers graceful degradation.
func TestConsumer_Run_TimeoutWord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockJetStreamConsumer{
		subscribeErr: errors.New("connection timeout"),
	}
	c := &Consumer{js: mock, vip: nil}

	err := c.Run(ctx)
	if err != nil {
		t.Errorf("expected nil on 'timeout', got: %v", err)
	}
}

// TestConsumer_Run_UnknownError verifies that unexpected subscribe errors are propagated.
func TestConsumer_Run_UnknownError(t *testing.T) {
	mock := &mockJetStreamConsumer{
		subscribeErr: errors.New("some unexpected nats error"),
	}
	c := &Consumer{js: mock, vip: nil}

	err := c.Run(context.Background())
	if err == nil {
		t.Error("expected error for unknown subscribe failure, got nil")
	}
}

// TestConsumer_Run_ContextCancelled verifies that Run returns ctx.Err() when
// subscribe succeeds and the context is subsequently cancelled.
func TestConsumer_Run_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	mock := &mockJetStreamConsumer{} // subscribeErr = nil → returns empty subscription
	c := &Consumer{js: mock, vip: nil}

	err := c.Run(ctx)
	// The context deadline expires, so ctx.Err() == context.DeadlineExceeded.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

// TestConsumer_HandleLLMUsage_PositiveAccountID_ReachesVIP verifies that a valid
// message with positive AccountID attempts to call RecalculateFromWallet.
// With a nil vip service this causes a nil-pointer panic, which proves we
// successfully traversed the guard clause.
func TestConsumer_HandleLLMUsage_PositiveAccountID_ReachesVIP(t *testing.T) {
	c := &Consumer{vip: nil}

	payload, _ := json.Marshal(event.LLMUsageReportedPayload{
		AccountID:  7,
		AmountCNY:  12.5,
		TokensUsed: 2000,
		ModelName:  "gpt-4o",
	})
	msg := &natsgo.Msg{Data: payload}

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic from nil vip.RecalculateFromWallet, got none")
		}
	}()
	_ = c.handleLLMUsage(context.Background(), msg)
}
