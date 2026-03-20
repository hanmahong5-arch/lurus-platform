package activities

import (
	"context"
	"fmt"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// ── Mock implementations ────────────────────────────────────────────────────

type mockPublisher struct {
	published []*event.IdentityEvent
	err       error
}

func (m *mockPublisher) Publish(_ context.Context, ev *event.IdentityEvent) error {
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, ev)
	return nil
}

type mockPlanStore struct {
	plans map[int64]*entity.ProductPlan
	err   error
}

func (m *mockPlanStore) GetPlanByID(_ context.Context, id int64) (*entity.ProductPlan, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.plans[id], nil
}

type mockMailer struct {
	sent []sentEmail
	err  error
}

type sentEmail struct {
	to, subject, body string
}

func (m *mockMailer) Send(_ context.Context, to, subject, body string) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, sentEmail{to, subject, body})
	return nil
}

// ── Event Activities Tests ──────────────────────────────────────────────────

func TestEventActivities_PublishToNATS_Success(t *testing.T) {
	pub := &mockPublisher{}
	a := &EventActivities{Publisher: pub}

	err := a.PublishToNATS(context.Background(), PublishEventInput{
		Subject:   "identity.account.created",
		AccountID: 42,
		LurusID:   "LU0000042",
		ProductID: "llm-api",
		Payload:   map[string]any{"key": "value"},
	})
	if err != nil {
		t.Fatalf("PublishToNATS: %v", err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.published))
	}
	if pub.published[0].AccountID != 42 {
		t.Errorf("AccountID = %d, want 42", pub.published[0].AccountID)
	}
}

func TestEventActivities_PublishToNATS_NilPublisher(t *testing.T) {
	a := &EventActivities{Publisher: nil}

	err := a.PublishToNATS(context.Background(), PublishEventInput{
		Subject: "test.subject", AccountID: 1,
	})
	if err != nil {
		t.Fatalf("nil publisher should not error: %v", err)
	}
}

func TestEventActivities_PublishToNATS_PublishError(t *testing.T) {
	pub := &mockPublisher{err: fmt.Errorf("NATS connection lost")}
	a := &EventActivities{Publisher: pub}

	err := a.PublishToNATS(context.Background(), PublishEventInput{
		Subject: "test.subject", AccountID: 1,
	})
	if err == nil {
		t.Fatal("expected error when publisher fails")
	}
}

// ── Query Activities Tests ──────────────────────────────────────────────────

func TestQueryActivities_GetPlanByID_Success(t *testing.T) {
	store := &mockPlanStore{
		plans: map[int64]*entity.ProductPlan{
			10: {ID: 10, Code: "pro-monthly", PriceCNY: 99.0},
		},
	}
	a := &QueryActivities{Plans: store}

	out, err := a.GetPlanByID(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if out.PlanID != 10 {
		t.Errorf("PlanID = %d, want 10", out.PlanID)
	}
	if out.Code != "pro-monthly" {
		t.Errorf("Code = %q, want 'pro-monthly'", out.Code)
	}
	if out.PriceCNY != 99.0 {
		t.Errorf("PriceCNY = %f, want 99", out.PriceCNY)
	}
}

func TestQueryActivities_GetPlanByID_NotFound(t *testing.T) {
	store := &mockPlanStore{plans: map[int64]*entity.ProductPlan{}}
	a := &QueryActivities{Plans: store}

	_, err := a.GetPlanByID(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for non-existent plan")
	}
}

func TestQueryActivities_GetPlanByID_DBError(t *testing.T) {
	store := &mockPlanStore{err: fmt.Errorf("db timeout")}
	a := &QueryActivities{Plans: store}

	_, err := a.GetPlanByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for DB failure")
	}
}

// ── Notification Activities Tests ───────────────────────────────────────────
// NotificationActivities.Accounts is *app.AccountService (concrete type),
// which requires unexported accountStore/walletStore/vipStore interfaces.
// We can't construct it from this package without violating encapsulation.
// These tests are covered at the app/handler layer instead.

func TestNotificationActivities_SendExpiryReminder_NoteOnConcreteDep(t *testing.T) {
	t.Skip("NotificationActivities.Accounts is *app.AccountService (concrete type) — " +
		"notification flow tested via handler and workflow tests")
}
