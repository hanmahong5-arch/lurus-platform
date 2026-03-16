package app

import (
	"context"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// makeRefundService wires a RefundService with in-memory mocks and a nil publisher.
func makeRefundService() (*RefundService, *mockRefundStore, *mockWalletStore) {
	ws := newMockWalletStore()
	rs := newMockRefundStore()
	// nil publisher: publishRefundCompleted is a no-op when publisher is nil.
	svc := NewRefundService(rs, ws, nil, nil)
	return svc, rs, ws
}

// seedPaidOrderForRefund adds a paid order within the refund window.
func seedPaidOrderForRefund(ws *mockWalletStore, accountID int64, orderNo string) {
	o := &entity.PaymentOrder{
		AccountID: accountID,
		OrderNo:   orderNo,
		OrderType: "topup",
		AmountCNY: 200.00,
		Status:    entity.OrderStatusPaid,
		CreatedAt: time.Now().UTC(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	// Ensure the wallet has sufficient balance so Credit calls succeed.
	ws.wallets[accountID] = &entity.Wallet{ID: accountID, AccountID: accountID, Balance: 0}
}

// TestRefundService_Request_Success verifies a successful refund request.
func TestRefundService_Request_Success(t *testing.T) {
	svc, rs, ws := makeRefundService()

	const accountID = int64(100)
	const orderNo = "LO20260101REF1"
	seedPaidOrderForRefund(ws, accountID, orderNo)

	r, err := svc.RequestRefund(context.Background(), accountID, orderNo, "changed my mind")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if r == nil {
		t.Fatal("expected refund, got nil")
	}
	if r.Status != entity.RefundStatusPending {
		t.Errorf("expected pending, got %s", r.Status)
	}
	if r.AmountCNY != 200.00 {
		t.Errorf("amount mismatch: want 200.00, got %.2f", r.AmountCNY)
	}

	// Verify the record is persisted.
	stored, _ := rs.GetByRefundNo(context.Background(), r.RefundNo)
	if stored == nil {
		t.Fatal("refund not found in store after creation")
	}
}

// TestRefundService_Request_OrderNotPaid verifies that a refund cannot be requested
// for an order that has not been paid.
func TestRefundService_Request_OrderNotPaid(t *testing.T) {
	svc, _, ws := makeRefundService()

	const accountID = int64(101)
	const orderNo = "LO20260101REF2"
	o := &entity.PaymentOrder{
		AccountID: accountID,
		OrderNo:   orderNo,
		AmountCNY: 100.00,
		Status:    entity.OrderStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	_, err := svc.RequestRefund(context.Background(), accountID, orderNo, "reason")
	if err == nil {
		t.Fatal("expected error for unpaid order, got nil")
	}
}

// TestRefundService_Request_Expired7Days verifies that a refund is rejected when
// the order was created more than 7 days ago.
func TestRefundService_Request_Expired7Days(t *testing.T) {
	svc, _, ws := makeRefundService()

	const accountID = int64(102)
	const orderNo = "LO20260101REF3"
	o := &entity.PaymentOrder{
		AccountID: accountID,
		OrderNo:   orderNo,
		AmountCNY: 150.00,
		Status:    entity.OrderStatusPaid,
		// 8 days ago — outside the refund window.
		CreatedAt: time.Now().UTC().AddDate(0, 0, -8),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	_, err := svc.RequestRefund(context.Background(), accountID, orderNo, "too late")
	if err == nil {
		t.Fatal("expected error for expired refund window, got nil")
	}
}

// TestRefundService_Request_Duplicate verifies that a second refund request is
// rejected when one is already in progress for the same order.
func TestRefundService_Request_Duplicate(t *testing.T) {
	svc, _, ws := makeRefundService()

	const accountID = int64(103)
	const orderNo = "LO20260101REF4"
	seedPaidOrderForRefund(ws, accountID, orderNo)

	// First request — should succeed.
	_, err := svc.RequestRefund(context.Background(), accountID, orderNo, "first request")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	// Second request — should be rejected.
	_, err = svc.RequestRefund(context.Background(), accountID, orderNo, "duplicate")
	if err == nil {
		t.Fatal("expected error for duplicate refund, got nil")
	}
}

// TestRefundService_Approve_CreditWallet verifies that approving a refund credits
// the account wallet with the refund amount and transitions to completed.
func TestRefundService_Approve_CreditWallet(t *testing.T) {
	svc, rs, ws := makeRefundService()

	const accountID = int64(104)
	const orderNo = "LO20260101REF5"
	seedPaidOrderForRefund(ws, accountID, orderNo)

	r, err := svc.RequestRefund(context.Background(), accountID, orderNo, "approve me")
	if err != nil {
		t.Fatalf("request refund: %v", err)
	}

	if err := svc.Approve(context.Background(), r.RefundNo, "admin1", "looks good"); err != nil {
		t.Fatalf("approve refund: %v", err)
	}

	// Verify wallet was credited.
	w, _ := ws.GetByAccountID(context.Background(), accountID)
	if w == nil {
		t.Fatal("wallet not found")
	}
	if w.Balance != 200.00 {
		t.Errorf("expected wallet balance 200.00 after refund, got %.2f", w.Balance)
	}

	// Verify refund status is completed.
	stored, _ := rs.GetByRefundNo(context.Background(), r.RefundNo)
	if stored == nil {
		t.Fatal("refund not found after approval")
	}
	if stored.Status != entity.RefundStatusCompleted {
		t.Errorf("expected completed status, got %s", stored.Status)
	}
	if stored.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

// TestRefundService_Reject_StatusChange verifies that rejecting a refund transitions
// it to rejected status without modifying the wallet balance.
func TestRefundService_Reject_StatusChange(t *testing.T) {
	svc, rs, ws := makeRefundService()

	const accountID = int64(105)
	const orderNo = "LO20260101REF6"
	seedPaidOrderForRefund(ws, accountID, orderNo)

	r, err := svc.RequestRefund(context.Background(), accountID, orderNo, "reject me")
	if err != nil {
		t.Fatalf("request refund: %v", err)
	}

	if err := svc.Reject(context.Background(), r.RefundNo, "admin2", "policy violation"); err != nil {
		t.Fatalf("reject refund: %v", err)
	}

	// Verify refund status is rejected.
	stored, _ := rs.GetByRefundNo(context.Background(), r.RefundNo)
	if stored == nil {
		t.Fatal("refund not found after rejection")
	}
	if stored.Status != entity.RefundStatusRejected {
		t.Errorf("expected rejected status, got %s", stored.Status)
	}

	// Wallet balance must remain unchanged (no credit).
	w, _ := ws.GetByAccountID(context.Background(), accountID)
	if w != nil && w.Balance != 0 {
		t.Errorf("wallet should not be credited on rejection, balance=%.2f", w.Balance)
	}
}

// TestRefundService_GetByNo_Success verifies that the owner can retrieve their refund.
func TestRefundService_GetByNo_Success(t *testing.T) {
	svc, _, ws := makeRefundService()

	const accountID = int64(106)
	const orderNo = "LO20260101REF7"
	seedPaidOrderForRefund(ws, accountID, orderNo)

	r, err := svc.RequestRefund(context.Background(), accountID, orderNo, "get by no")
	if err != nil {
		t.Fatalf("request refund: %v", err)
	}

	got, err := svc.GetByNo(context.Background(), accountID, r.RefundNo)
	if err != nil {
		t.Fatalf("GetByNo: %v", err)
	}
	if got.RefundNo != r.RefundNo {
		t.Errorf("refund_no mismatch: want %s, got %s", r.RefundNo, got.RefundNo)
	}
}

// TestRefundService_GetByNo_IDOR verifies that a different account cannot retrieve
// another account's refund.
func TestRefundService_GetByNo_IDOR(t *testing.T) {
	svc, _, ws := makeRefundService()

	const ownerID = int64(107)
	const attackerID = int64(108)
	const orderNo = "LO20260101REF8"
	seedPaidOrderForRefund(ws, ownerID, orderNo)

	r, _ := svc.RequestRefund(context.Background(), ownerID, orderNo, "owner refund")
	_, err := svc.GetByNo(context.Background(), attackerID, r.RefundNo)
	if err == nil {
		t.Fatal("expected IDOR error, got nil")
	}
}

// TestRefundService_ListByAccount verifies paginated listing returns the correct refunds.
func TestRefundService_ListByAccount(t *testing.T) {
	svc, _, ws := makeRefundService()

	const accountID = int64(109)
	const orderNo = "LO20260101REF9"
	seedPaidOrderForRefund(ws, accountID, orderNo)

	_, _ = svc.RequestRefund(context.Background(), accountID, orderNo, "list test")

	list, total, err := svc.ListByAccount(context.Background(), accountID, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 refund, got %d (len=%d)", total, len(list))
	}
}
