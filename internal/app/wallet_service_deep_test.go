package app

import (
	"context"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// Tests in this file cover deeper wallet edge cases not in existing files:
// - MarkOrderPaid topup→wallet credit chain
// - MarkOrderPaid concurrent double-credit prevention
// - CreateSubscriptionOrder order number generation
// - GetOrderByNo ownership obscured error

// makeWalletSvcDeep creates a WalletService with mock stores for deep tests.
func makeWalletSvcDeep() (*WalletService, *mockWalletStore) {
	ws := newMockWalletStore()
	vs := newMockVIPStore(nil)
	vipSvc := NewVIPService(vs, ws)
	return NewWalletService(ws, vipSvc), ws
}

// ── MarkOrderPaid: topup chain ────────────────────────────────────────────

func TestWalletServiceDeep_MarkOrderPaid_TopupCreditsWallet(t *testing.T) {
	svc, ws := makeWalletSvcDeep()

	order, err := svc.CreateTopup(context.Background(), 1, 100.0, "stripe")
	if err != nil {
		t.Fatalf("CreateTopup: %v", err)
	}

	walletBefore, _ := ws.GetOrCreate(context.Background(), 1)
	balBefore := walletBefore.Balance

	paid, err := svc.MarkOrderPaid(context.Background(), order.OrderNo)
	if err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}
	if paid.Status != entity.OrderStatusPaid {
		t.Errorf("status = %s, want paid", paid.Status)
	}

	walletAfter, _ := ws.GetOrCreate(context.Background(), 1)
	if absFloat64(walletAfter.Balance-(balBefore+100.0)) > 0.01 {
		t.Errorf("balance = %.2f, want %.2f", walletAfter.Balance, balBefore+100.0)
	}
}

// ── MarkOrderPaid: concurrent no double credit ────────────────────────────

func TestWalletServiceDeep_MarkOrderPaid_ConcurrentSafe(t *testing.T) {
	svc, ws := makeWalletSvcDeep()

	order, _ := svc.CreateTopup(context.Background(), 1, 50.0, "stripe")

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			svc.MarkOrderPaid(context.Background(), order.OrderNo)
		}()
	}
	wg.Wait()

	wallet, _ := ws.GetOrCreate(context.Background(), 1)
	if wallet.Balance > 50.01 {
		t.Errorf("balance = %.2f, want <= 50.00 (double credit!)", wallet.Balance)
	}
}

// ── MarkOrderPaid: idempotent no duplicate credit ─────────────────────────

func TestWalletServiceDeep_MarkOrderPaid_SecondCallNoop(t *testing.T) {
	svc, ws := makeWalletSvcDeep()
	order, _ := svc.CreateTopup(context.Background(), 1, 30.0, "stripe")

	svc.MarkOrderPaid(context.Background(), order.OrderNo)
	w1, _ := ws.GetOrCreate(context.Background(), 1)

	svc.MarkOrderPaid(context.Background(), order.OrderNo)
	w2, _ := ws.GetOrCreate(context.Background(), 1)

	if w2.Balance != w1.Balance {
		t.Errorf("second pay changed balance: %.2f → %.2f", w1.Balance, w2.Balance)
	}
}

// ── CreateSubscriptionOrder: generates order no ───────────────────────────

func TestWalletServiceDeep_CreateSubOrder_GeneratesOrderNo(t *testing.T) {
	svc, _ := makeWalletSvcDeep()
	planID := int64(42)
	order := &entity.PaymentOrder{
		AccountID:     1,
		OrderType:     "subscription",
		ProductID:     "lucrum",
		PlanID:        &planID,
		AmountCNY:     19.9,
		PaymentMethod: "wallet",
	}
	if err := svc.CreateSubscriptionOrder(context.Background(), order); err != nil {
		t.Fatalf("CreateSubscriptionOrder: %v", err)
	}
	if order.OrderNo == "" {
		t.Error("OrderNo should be generated")
	}
	if order.Status != entity.OrderStatusPending {
		t.Errorf("Status = %s, want pending", order.Status)
	}
}

// ── GetOrderByNo: ownership obscuration ───────────────────────────────────

func TestWalletServiceDeep_GetOrderByNo_OwnershipObscured(t *testing.T) {
	svc, _ := makeWalletSvcDeep()
	order, _ := svc.CreateTopup(context.Background(), 1, 25.0, "stripe")

	// Account 2 tries to access account 1's order.
	_, err := svc.GetOrderByNo(context.Background(), 2, order.OrderNo)
	if err == nil {
		t.Fatal("expected error for cross-account access")
	}
	// Error message should NOT reveal the order exists — just "not found".
	errMsg := err.Error()
	if !stringContains(errMsg, "not found") {
		t.Errorf("error should say 'not found', got: %s", errMsg)
	}
}

// ── CreateTopup: order number uniqueness ──────────────────────────────────

func TestWalletServiceDeep_CreateTopup_UniqueOrderNos(t *testing.T) {
	svc, _ := makeWalletSvcDeep()
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		order, err := svc.CreateTopup(context.Background(), 1, 1.0, "stripe")
		if err != nil {
			t.Fatalf("CreateTopup[%d]: %v", i, err)
		}
		if seen[order.OrderNo] {
			t.Fatalf("duplicate OrderNo: %s at iteration %d", order.OrderNo, i)
		}
		seen[order.OrderNo] = true
	}
}
