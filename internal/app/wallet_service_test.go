package app

import (
	"context"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func makeWalletService() (*WalletService, *mockWalletStore) {
	ws := newMockWalletStore()
	vipSvc := NewVIPService(newMockVIPStore(defaultVIPConfigs()), ws)
	return NewWalletService(ws, vipSvc), ws
}

func TestWalletService_GetWallet_CreatesIfMissing(t *testing.T) {
	svc, _ := makeWalletService()
	w, err := svc.GetWallet(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetWallet error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil wallet")
	}
	if w.AccountID != 1 {
		t.Errorf("AccountID=%d, want 1", w.AccountID)
	}
}

func TestWalletService_Topup(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()
	_, _ = ws.GetOrCreate(ctx, 1)

	tx, err := svc.Topup(ctx, 1, 100.0, "LO20260101000001")
	if err != nil {
		t.Fatalf("Topup error: %v", err)
	}
	if tx.Amount != 100.0 {
		t.Errorf("tx.Amount=%.2f, want 100.00", tx.Amount)
	}
	if tx.Type != entity.TxTypeTopup {
		t.Errorf("tx.Type=%q, want %q", tx.Type, entity.TxTypeTopup)
	}
	// Check balance updated
	w, _ := svc.GetWallet(ctx, 1)
	if w.Balance != 100.0 {
		t.Errorf("wallet balance=%.2f, want 100.00", w.Balance)
	}
	if w.LifetimeTopup != 100.0 {
		t.Errorf("LifetimeTopup=%.2f, want 100.00", w.LifetimeTopup)
	}
}

func TestWalletService_Redeem_Valid(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()
	_, _ = ws.GetOrCreate(ctx, 1)

	// Pre-seed a redemption code
	exp := time.Now().Add(24 * time.Hour)
	ws.codes["PROMO01"] = &entity.RedemptionCode{
		Code: "PROMO01", RewardType: "credits", RewardValue: 50.0,
		MaxUses: 1, UsedCount: 0, ExpiresAt: &exp,
	}

	if err := svc.Redeem(ctx, 1, "promo01"); err != nil { // lowercase should work
		t.Fatalf("Redeem error: %v", err)
	}
	w, _ := svc.GetWallet(ctx, 1)
	if w.Balance != 50.0 {
		t.Errorf("balance=%.2f after redeem, want 50.00", w.Balance)
	}
	// Code should be marked as used
	rc, _ := ws.GetRedemptionCode(ctx, "PROMO01")
	if rc.UsedCount != 1 {
		t.Errorf("UsedCount=%d, want 1", rc.UsedCount)
	}
}

func TestWalletService_Redeem_Expired(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()

	exp := time.Now().Add(-time.Hour) // already expired
	ws.codes["OLD01"] = &entity.RedemptionCode{
		Code: "OLD01", RewardType: "credits", RewardValue: 10.0,
		MaxUses: 1, UsedCount: 0, ExpiresAt: &exp,
	}
	err := svc.Redeem(ctx, 1, "OLD01")
	if err == nil {
		t.Error("expected error for expired code, got nil")
	}
}

func TestWalletService_Redeem_UsedUp(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()

	ws.codes["USED01"] = &entity.RedemptionCode{
		Code: "USED01", RewardType: "credits", RewardValue: 10.0,
		MaxUses: 1, UsedCount: 1,
	}
	err := svc.Redeem(ctx, 1, "USED01")
	if err == nil {
		t.Error("expected error for exhausted code, got nil")
	}
}

func TestWalletService_Redeem_InvalidCode(t *testing.T) {
	svc, _ := makeWalletService()
	err := svc.Redeem(context.Background(), 1, "NOEXIST")
	if err == nil {
		t.Error("expected error for non-existent code, got nil")
	}
}

func TestWalletService_Debit(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()
	_, _ = ws.GetOrCreate(ctx, 1)
	_, _ = svc.Topup(ctx, 1, 100.0, "LO-topup")

	tx, err := svc.Debit(ctx, 1, 30.0, entity.TxTypeSubscription, "pro plan", "subscription", "sub-1", "llm-api")
	if err != nil {
		t.Fatalf("Debit error: %v", err)
	}
	if tx.Amount != -30.0 {
		t.Errorf("tx.Amount=%.2f, want -30.00", tx.Amount)
	}
	w, _ := svc.GetWallet(ctx, 1)
	if w.Balance != 70.0 {
		t.Errorf("balance=%.2f after debit, want 70.00", w.Balance)
	}
}

func TestWalletService_ListTransactions(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()
	_, _ = ws.GetOrCreate(ctx, 1)
	_, _ = svc.Topup(ctx, 1, 50.0, "LO-tx1")
	_, _ = svc.Topup(ctx, 1, 25.0, "LO-tx2")

	txs, total, err := svc.ListTransactions(ctx, 1, 1, 10)
	if err != nil {
		t.Fatalf("ListTransactions error: %v", err)
	}
	if total < 2 {
		t.Errorf("total=%d, want ≥2", total)
	}
	if len(txs) < 2 {
		t.Errorf("len(txs)=%d, want ≥2", len(txs))
	}
}

func TestWalletService_CreatePaymentOrder(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()

	order := &entity.PaymentOrder{
		AccountID: 1, OrderNo: "LO-TEST-001", OrderType: "topup",
		AmountCNY: 100.0, Status: entity.OrderStatusPending, PaymentMethod: "wallet",
	}
	if err := svc.CreatePaymentOrder(ctx, order); err != nil {
		t.Fatalf("CreatePaymentOrder error: %v", err)
	}
	// Verify it was stored
	stored, _ := ws.GetPaymentOrderByNo(ctx, "LO-TEST-001")
	if stored == nil {
		t.Error("order not found in store after CreatePaymentOrder")
	}
	if stored.Status != entity.OrderStatusPending {
		t.Errorf("Status=%q, want pending", stored.Status)
	}
}

func TestWalletService_MarkOrderPaid_Idempotent(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()
	_, _ = ws.GetOrCreate(ctx, 1)

	ws.orders["LO001"] = &entity.PaymentOrder{
		AccountID: 1, OrderNo: "LO001", OrderType: "topup",
		AmountCNY: 200, Status: entity.OrderStatusPaid,
	}
	// Calling MarkOrderPaid on already-paid order should not double-credit
	o, err := svc.MarkOrderPaid(ctx, "LO001")
	if err != nil {
		t.Fatalf("MarkOrderPaid error: %v", err)
	}
	if o.Status != entity.OrderStatusPaid {
		t.Errorf("Status=%q, want paid", o.Status)
	}
	w, _ := svc.GetWallet(ctx, 1)
	if w.Balance != 0 { // already-paid: no credit applied
		t.Errorf("balance=%.2f, want 0 (idempotent)", w.Balance)
	}
}

// TestWalletService_GetBalance_ExistingWallet verifies GetBalance returns wallet for existing account.
func TestWalletService_GetBalance_ExistingWallet(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()

	// Seed a wallet.
	_, _ = svc.GetWallet(ctx, 10)

	w, err := svc.GetBalance(ctx, 10)
	if err != nil {
		t.Fatalf("GetBalance error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil wallet")
	}
	if w.AccountID != 10 {
		t.Errorf("AccountID = %d, want 10", w.AccountID)
	}
	_ = ws
}

// TestWalletService_GetBalance_NotFound verifies GetBalance returns nil for unknown account.
func TestWalletService_GetBalance_NotFound(t *testing.T) {
	svc, _ := makeWalletService()

	w, err := svc.GetBalance(context.Background(), 9999)
	if err != nil {
		t.Fatalf("GetBalance error: %v", err)
	}
	if w != nil {
		t.Error("expected nil wallet for unknown account")
	}
}
