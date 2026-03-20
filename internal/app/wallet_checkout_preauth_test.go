package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ── GetBillingSummary ───────────────────────────────────────────────────────

func TestWalletService_GetBillingSummary_WithWallet(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	// Seed wallet with some data.
	ws.GetOrCreate(ctx, 1)
	ws.Credit(ctx, 1, 200.0, "topup", "seed", "", "", "")
	ws.Debit(ctx, 1, 50.0, "subscription", "sub", "", "", "prod")

	summary, err := svc.GetBillingSummary(ctx, 1)
	if err != nil {
		t.Fatalf("GetBillingSummary: %v", err)
	}
	if summary.Balance != 150.0 {
		t.Errorf("Balance = %f, want 150", summary.Balance)
	}
	if summary.LifetimeTopup != 200.0 {
		t.Errorf("LifetimeTopup = %f, want 200", summary.LifetimeTopup)
	}
	if summary.LifetimeSpend != 50.0 {
		t.Errorf("LifetimeSpend = %f, want 50", summary.LifetimeSpend)
	}
	if summary.Available != summary.Balance-summary.Frozen {
		t.Errorf("Available = %f, want Balance-Frozen = %f", summary.Available, summary.Balance-summary.Frozen)
	}
}

func TestWalletService_GetBillingSummary_NoWallet(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)

	// Account 999 has no wallet.
	summary, err := svc.GetBillingSummary(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetBillingSummary: %v", err)
	}
	if summary.Balance != 0 {
		t.Errorf("Balance = %f, want 0", summary.Balance)
	}
	if summary.ActivePreAuths != 0 {
		t.Errorf("ActivePreAuths = %d, want 0", summary.ActivePreAuths)
	}
}

// ── CreateCheckoutSession ───────────────────────────────────────────────────

func TestWalletService_CreateCheckoutSession_Success(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)

	order, err := svc.CreateCheckoutSession(context.Background(),
		1, 99.0, "stripe", "lurus-api", "idem-key-1", 0)
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	if order == nil {
		t.Fatal("expected non-nil order")
	}
	if order.AmountCNY != 99.0 {
		t.Errorf("AmountCNY = %f, want 99", order.AmountCNY)
	}
	if order.SourceService != "lurus-api" {
		t.Errorf("SourceService = %q, want 'lurus-api'", order.SourceService)
	}
	if order.IdempotencyKey != "idem-key-1" {
		t.Errorf("IdempotencyKey = %q, want 'idem-key-1'", order.IdempotencyKey)
	}
	if order.ExpiresAt == nil {
		t.Error("expected ExpiresAt to be set")
	}
}

func TestWalletService_CreateCheckoutSession_IdempotentReturn(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	// First call.
	order1, err := svc.CreateCheckoutSession(ctx, 1, 50.0, "stripe", "svc", "idem-dup", 0)
	if err != nil {
		t.Fatalf("first CreateCheckoutSession: %v", err)
	}

	// Second call with same idempotency key — should return existing order.
	order2, err := svc.CreateCheckoutSession(ctx, 1, 50.0, "stripe", "svc", "idem-dup", 0)
	if err != nil {
		t.Fatalf("second CreateCheckoutSession: %v", err)
	}
	if order2.OrderNo != order1.OrderNo {
		t.Errorf("expected same OrderNo, got %q vs %q", order2.OrderNo, order1.OrderNo)
	}
}

func TestWalletService_CreateCheckoutSession_NegativeAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.CreateCheckoutSession(context.Background(), 1, -10.0, "stripe", "svc", "", 0)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Errorf("error = %q, want containing 'positive'", err.Error())
	}
}

func TestWalletService_CreateCheckoutSession_ZeroAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.CreateCheckoutSession(context.Background(), 1, 0, "stripe", "svc", "", 0)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestWalletService_CreateCheckoutSession_CustomTTL(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	order, err := svc.CreateCheckoutSession(context.Background(),
		1, 10.0, "stripe", "svc", "", 5*time.Minute)
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	if order.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt")
	}
	// ExpiresAt should be roughly 5 minutes from now.
	diff := time.Until(*order.ExpiresAt)
	if diff < 4*time.Minute || diff > 6*time.Minute {
		t.Errorf("ExpiresAt diff = %v, want ~5m", diff)
	}
}

// ── GetCheckoutStatus ───────────────────────────────────────────────────────

func TestWalletService_GetCheckoutStatus_Found(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	// Create an order first.
	order, _ := svc.CreateCheckoutSession(ctx, 1, 50.0, "stripe", "svc", "", 0)

	got, err := svc.GetCheckoutStatus(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetCheckoutStatus: %v", err)
	}
	if got.OrderNo != order.OrderNo {
		t.Errorf("OrderNo = %q, want %q", got.OrderNo, order.OrderNo)
	}
}

func TestWalletService_GetCheckoutStatus_NotFound(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.GetCheckoutStatus(context.Background(), "NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for non-existent order")
	}
}

// ── PreAuthorize ────────────────────────────────────────────────────────────

func TestWalletService_PreAuthorize_Success(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	ws.GetOrCreate(ctx, 1)
	ws.Credit(ctx, 1, 100.0, "topup", "seed", "", "", "")

	pa, err := svc.PreAuthorize(ctx, 1, 30.0, "llm-api", "call-1", "streaming call", 0)
	if err != nil {
		t.Fatalf("PreAuthorize: %v", err)
	}
	if pa == nil {
		t.Fatal("expected non-nil pre-auth")
	}
	if pa.Amount != 30.0 {
		t.Errorf("Amount = %f, want 30", pa.Amount)
	}
	if pa.ProductID != "llm-api" {
		t.Errorf("ProductID = %q, want 'llm-api'", pa.ProductID)
	}
}

func TestWalletService_PreAuthorize_NegativeAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.PreAuthorize(context.Background(), 1, -5.0, "prod", "ref", "desc", 0)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestWalletService_PreAuthorize_ZeroAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.PreAuthorize(context.Background(), 1, 0, "prod", "ref", "desc", 0)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestWalletService_PreAuthorize_DefaultTTL(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	ws.GetOrCreate(ctx, 1)
	ws.Credit(ctx, 1, 100.0, "topup", "seed", "", "", "")

	pa, err := svc.PreAuthorize(ctx, 1, 10.0, "prod", "ref", "desc", 0)
	if err != nil {
		t.Fatalf("PreAuthorize: %v", err)
	}
	// Default TTL is 10 minutes.
	diff := time.Until(pa.ExpiresAt)
	if diff < 9*time.Minute || diff > 11*time.Minute {
		t.Errorf("ExpiresAt diff = %v, want ~10m", diff)
	}
}

// ── SettlePreAuth ───────────────────────────────────────────────────────────

func TestWalletService_SettlePreAuth_Success(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	ws.GetOrCreate(ctx, 1)
	ws.Credit(ctx, 1, 200.0, "topup", "seed", "", "", "")

	pa, _ := svc.PreAuthorize(ctx, 1, 50.0, "prod", "ref", "desc", 0)

	settled, err := svc.SettlePreAuth(ctx, pa.ID, 30.0)
	if err != nil {
		t.Fatalf("SettlePreAuth: %v", err)
	}
	if settled.Status != "settled" {
		t.Errorf("Status = %q, want 'settled'", settled.Status)
	}
}

func TestWalletService_SettlePreAuth_NegativeAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.SettlePreAuth(context.Background(), 1, -5.0)
	if err == nil {
		t.Fatal("expected error for negative actual amount")
	}
}

func TestWalletService_SettlePreAuth_ZeroAmount(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	ws.GetOrCreate(ctx, 1)
	ws.Credit(ctx, 1, 100.0, "topup", "seed", "", "", "")
	pa, _ := svc.PreAuthorize(ctx, 1, 20.0, "prod", "ref", "desc", 0)

	// Zero actual amount is valid (no charge, just release).
	settled, err := svc.SettlePreAuth(ctx, pa.ID, 0)
	if err != nil {
		t.Fatalf("SettlePreAuth zero: %v", err)
	}
	if settled.Status != "settled" {
		t.Errorf("Status = %q, want 'settled'", settled.Status)
	}
}

// ── ReleasePreAuth ──────────────────────────────────────────────────────────

func TestWalletService_ReleasePreAuth_Success(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	ws.GetOrCreate(ctx, 1)
	ws.Credit(ctx, 1, 100.0, "topup", "seed", "", "", "")

	pa, _ := svc.PreAuthorize(ctx, 1, 40.0, "prod", "ref", "desc", 0)

	released, err := svc.ReleasePreAuth(ctx, pa.ID)
	if err != nil {
		t.Fatalf("ReleasePreAuth: %v", err)
	}
	if released.Status != "released" {
		t.Errorf("Status = %q, want 'released'", released.Status)
	}
}

func TestWalletService_ReleasePreAuth_NotFound(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.ReleasePreAuth(context.Background(), 99999)
	if err == nil {
		t.Fatal("expected error for non-existent pre-auth")
	}
}

// ── CreateTopup (app-level) ─────────────────────────────────────────────────

func TestWalletService_CreateTopup_NegativeAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.CreateTopup(context.Background(), 1, -50.0, "stripe")
	if err == nil {
		t.Fatal("expected error for negative topup amount")
	}
}

func TestWalletService_CreateTopup_ZeroAmount(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	_, err := svc.CreateTopup(context.Background(), 1, 0, "stripe")
	if err == nil {
		t.Fatal("expected error for zero topup amount")
	}
}

func TestWalletService_CreateTopup_Success(t *testing.T) {
	svc := NewWalletService(newMockWalletStore(), nil)

	order, err := svc.CreateTopup(context.Background(), 1, 100.0, "epay_alipay")
	if err != nil {
		t.Fatalf("CreateTopup: %v", err)
	}
	if order.AmountCNY != 100.0 {
		t.Errorf("AmountCNY = %f, want 100", order.AmountCNY)
	}
	if order.PaymentMethod != "epay_alipay" {
		t.Errorf("PaymentMethod = %q, want 'epay_alipay'", order.PaymentMethod)
	}
	if order.OrderType != "topup" {
		t.Errorf("OrderType = %q, want 'topup'", order.OrderType)
	}
	if !strings.HasPrefix(order.OrderNo, "LO") {
		t.Errorf("OrderNo = %q, want prefix 'LO'", order.OrderNo)
	}
}

// ── GetOrderByNo (app-level IDOR) ───────────────────────────────────────────

func TestWalletService_GetOrderByNo_CorrectOwner(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	order, _ := svc.CreateTopup(ctx, 1, 50.0, "stripe")

	got, err := svc.GetOrderByNo(ctx, 1, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrderByNo: %v", err)
	}
	if got.OrderNo != order.OrderNo {
		t.Errorf("OrderNo mismatch")
	}
}

func TestWalletService_GetOrderByNo_WrongOwner_IDOR(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, nil)
	ctx := context.Background()

	order, _ := svc.CreateTopup(ctx, 1, 50.0, "stripe")

	// Account 2 tries to access account 1's order.
	_, err := svc.GetOrderByNo(ctx, 2, order.OrderNo)
	if err == nil {
		t.Fatal("IDOR: wrong owner should get error")
	}
	// Error message should be obscured (not "wrong account").
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want containing 'not found' (obscured)", err.Error())
	}
}
