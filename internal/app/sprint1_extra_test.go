package app

// Additional unit tests for Sprint 1 new methods.

import (
	"context"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── SubscriptionService: expiry scan methods ──────────────────────────────────

func TestSubscriptionService_ListActiveExpired(t *testing.T) {
	svc, store, plan := makeSubscriptionService()
	ctx := context.Background()

	_ = store // used via svc
	seedPlan(t, ctx, plan, "llm-api", "basic")

	// Create an active subscription with an already-expired date.
	p, _ := plan.GetPlanByID(ctx, 1)
	past := time.Now().Add(-2 * time.Hour)
	sub := &entity.Subscription{
		AccountID:  42,
		ProductID:  "llm-api",
		PlanID:     p.ID,
		Status:     entity.SubStatusActive,
		ExpiresAt:  &past,
	}
	if err := store.Create(ctx, sub); err != nil {
		t.Fatalf("create sub: %v", err)
	}

	// Also create an active sub that has NOT expired yet.
	future := time.Now().Add(24 * time.Hour)
	sub2 := &entity.Subscription{
		AccountID: 43,
		ProductID: "llm-api",
		PlanID:    p.ID,
		Status:    entity.SubStatusActive,
		ExpiresAt: &future,
	}
	if err := store.Create(ctx, sub2); err != nil {
		t.Fatalf("create sub2: %v", err)
	}

	expired, err := svc.ListActiveExpired(ctx)
	if err != nil {
		t.Fatalf("ListActiveExpired: %v", err)
	}
	if len(expired) != 1 {
		t.Errorf("len(expired)=%d, want 1", len(expired))
	}
	if expired[0].AccountID != 42 {
		t.Errorf("wrong account in expired list: %d", expired[0].AccountID)
	}
}

func TestSubscriptionService_ListGraceExpired(t *testing.T) {
	svc, store, _ := makeSubscriptionService()
	ctx := context.Background()

	// Grace sub with grace_until already passed.
	pastGrace := time.Now().Add(-time.Hour)
	sub := &entity.Subscription{
		AccountID:  55,
		ProductID:  "llm-api",
		PlanID:     1,
		Status:     entity.SubStatusGrace,
		GraceUntil: &pastGrace,
	}
	if err := store.Create(ctx, sub); err != nil {
		t.Fatalf("create sub: %v", err)
	}

	// Grace sub still within grace period.
	futureGrace := time.Now().Add(24 * time.Hour)
	sub2 := &entity.Subscription{
		AccountID:  56,
		ProductID:  "llm-api",
		PlanID:     1,
		Status:     entity.SubStatusGrace,
		GraceUntil: &futureGrace,
	}
	if err := store.Create(ctx, sub2); err != nil {
		t.Fatalf("create sub2: %v", err)
	}

	expired, err := svc.ListGraceExpired(ctx)
	if err != nil {
		t.Fatalf("ListGraceExpired: %v", err)
	}
	if len(expired) != 1 {
		t.Errorf("len(expired)=%d, want 1", len(expired))
	}
	if expired[0].AccountID != 55 {
		t.Errorf("wrong account: %d", expired[0].AccountID)
	}
}

// ── WalletService: new methods from Sprint 0 payment work ─────────────────────

func TestWalletService_Credit(t *testing.T) {
	svc, _ := makeWalletService()
	ctx := context.Background()

	// Create wallet first.
	_, _ = svc.GetWallet(ctx, 10)

	_, err := svc.Credit(ctx, 10, 50.0, "admin_adjust", "test credit", "admin", "ref-001", "")
	if err != nil {
		t.Fatalf("Credit: %v", err)
	}
	w, _ := svc.GetWallet(ctx, 10)
	if w.Balance != 50.0 {
		t.Errorf("Balance=%.2f, want 50.00", w.Balance)
	}
}

func TestWalletService_CreateTopup(t *testing.T) {
	svc, _ := makeWalletService()
	ctx := context.Background()

	_, _ = svc.GetWallet(ctx, 20)
	order, err := svc.CreateTopup(ctx, 20, 100.0, "epay_alipay")
	if err != nil {
		t.Fatalf("CreateTopup: %v", err)
	}
	if order == nil {
		t.Fatal("expected non-nil order")
	}
	if order.AmountCNY != 100.0 {
		t.Errorf("AmountCNY=%.2f, want 100.00", order.AmountCNY)
	}
	if order.OrderNo == "" {
		t.Error("expected non-empty OrderNo")
	}
}

func TestWalletService_ListOrders(t *testing.T) {
	svc, _ := makeWalletService()
	ctx := context.Background()

	_, _ = svc.GetWallet(ctx, 30)
	_, _ = svc.CreateTopup(ctx, 30, 10.0, "epay_alipay")

	orders, total, err := svc.ListOrders(ctx, 30, 1, 10)
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if total == 0 {
		t.Error("expected at least one order")
	}
	_ = orders
}

func TestWalletService_GetOrderByNo(t *testing.T) {
	svc, _ := makeWalletService()
	ctx := context.Background()

	_, _ = svc.GetWallet(ctx, 40)
	order, _ := svc.CreateTopup(ctx, 40, 20.0, "stripe")
	if order == nil {
		t.Fatal("CreateTopup returned nil order")
	}

	got, err := svc.GetOrderByNo(ctx, 40, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrderByNo: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil order")
	}
	if got.OrderNo != order.OrderNo {
		t.Errorf("OrderNo=%q, want %q", got.OrderNo, order.OrderNo)
	}
}

func TestWalletService_CreateSubscriptionOrder(t *testing.T) {
	svc, _ := makeWalletService()
	ctx := context.Background()
	planID := int64(1)
	o := &entity.PaymentOrder{
		AccountID:     50,
		ProductID:     "llm-api",
		PlanID:        &planID,
		AmountCNY:     99.0,
		PaymentMethod: "wallet",
		OrderType:     "subscription",
	}
	if err := svc.CreateSubscriptionOrder(ctx, o); err != nil {
		t.Fatalf("CreateSubscriptionOrder: %v", err)
	}
	if o.OrderNo == "" {
		t.Error("expected order to have an OrderNo")
	}
}
