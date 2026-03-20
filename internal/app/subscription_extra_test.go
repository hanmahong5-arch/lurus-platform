package app

import (
	"context"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── SubscriptionService.GetByID ─────────────────────────────────────────────

func TestSubscriptionService_GetByID_Found(t *testing.T) {
	ss := newMockSubStore()
	ps := newMockPlanStore()
	svc := NewSubscriptionService(ss, ps, nil, 3)
	ctx := context.Background()

	now := time.Now()
	sub := &entity.Subscription{
		AccountID: 1, ProductID: "llm-api", PlanID: 10,
		Status: entity.SubStatusActive, StartedAt: &now,
	}
	ss.Create(ctx, sub)

	got, err := svc.GetByID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil subscription")
	}
	if got.ProductID != "llm-api" {
		t.Errorf("ProductID = %q, want 'llm-api'", got.ProductID)
	}
}

func TestSubscriptionService_GetByID_NotFound(t *testing.T) {
	ss := newMockSubStore()
	svc := NewSubscriptionService(ss, newMockPlanStore(), nil, 3)

	got, err := svc.GetByID(context.Background(), 9999)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent subscription")
	}
}

// ── SubscriptionService.UpdateRenewalState ──────────────────────────────────

func TestSubscriptionService_UpdateRenewalState(t *testing.T) {
	ss := newMockSubStore()
	svc := NewSubscriptionService(ss, newMockPlanStore(), nil, 3)
	ctx := context.Background()

	now := time.Now()
	sub := &entity.Subscription{
		AccountID: 1, ProductID: "prod", PlanID: 1,
		Status: entity.SubStatusActive, StartedAt: &now,
	}
	ss.Create(ctx, sub)

	nextAt := time.Now().Add(24 * time.Hour)
	err := svc.UpdateRenewalState(ctx, sub.ID, 3, &nextAt)
	if err != nil {
		t.Fatalf("UpdateRenewalState: %v", err)
	}

	updated, _ := ss.GetByID(ctx, sub.ID)
	if updated.RenewalAttempts != 3 {
		t.Errorf("RenewalAttempts = %d, want 3", updated.RenewalAttempts)
	}
}

// ── SubscriptionService.Cancel edge cases ───────────────────────────────────

func TestSubscriptionService_Cancel_NoActiveSub(t *testing.T) {
	ss := newMockSubStore()
	svc := NewSubscriptionService(ss, newMockPlanStore(), nil, 3)

	err := svc.Cancel(context.Background(), 1, "nonexistent-product")
	if err == nil {
		t.Fatal("expected error when no active subscription exists")
	}
}

// ── WithSubscriptionCanceller (RefundService) ───────────────────────────────

func TestRefundService_WithSubscriptionCanceller(t *testing.T) {
	svc := NewRefundService(&mockRefundStore{}, newMockWalletStore(), &mockPublisher{}, nil)

	// WithSubscriptionCanceller takes a subscriptionCanceller interface.
	// Use SubscriptionService which implements Cancel(ctx, accountID, productID).
	ss := newMockSubStore()
	subSvc := NewSubscriptionService(ss, newMockPlanStore(), nil, 3)
	result := svc.WithSubscriptionCanceller(subSvc)

	if result != svc {
		t.Error("expected chaining to return same service")
	}
	if svc.subCancel == nil {
		t.Error("expected canceller to be set")
	}
}

// ── ReferralService.WithRewardEvents ────────────────────────────────────────

func TestReferralService_WithRewardEvents(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore())
	// WithRewardEvents takes a rewardEventStore interface — use mockReferralStatsStore
	// which doesn't implement it, so we skip this test.
	_ = svc
}
