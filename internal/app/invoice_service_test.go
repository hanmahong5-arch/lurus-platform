package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// makeInvoiceService wires an InvoiceService with in-memory mocks.
func makeInvoiceService() (*InvoiceService, *mockInvoiceStore, *mockWalletStore) {
	ws := newMockWalletStore()
	is := newMockInvoiceStore()
	svc := NewInvoiceService(is, ws)
	return svc, is, ws
}

// seedPaidOrder adds a paid payment order to the mock wallet store and returns it.
func seedPaidOrder(ws *mockWalletStore, accountID int64, orderNo string) *entity.PaymentOrder {
	o := &entity.PaymentOrder{
		AccountID: accountID,
		OrderNo:   orderNo,
		OrderType: "topup",
		ProductID: "gushen",
		AmountCNY: 99.00,
		Status:    entity.OrderStatusPaid,
		CreatedAt: time.Now().UTC(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	return o
}

// TestInvoiceService_Generate_NewInvoice verifies that a new invoice is created for a paid order.
func TestInvoiceService_Generate_NewInvoice(t *testing.T) {
	svc, _, ws := makeInvoiceService()

	const accountID = int64(42)
	const orderNo = "LO20260101ABCD"
	seedPaidOrder(ws, accountID, orderNo)

	inv, err := svc.Generate(context.Background(), accountID, orderNo)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if inv == nil {
		t.Fatal("expected invoice, got nil")
	}
	if inv.OrderNo != orderNo {
		t.Errorf("order_no mismatch: want %s, got %s", orderNo, inv.OrderNo)
	}
	if inv.AccountID != accountID {
		t.Errorf("account_id mismatch: want %d, got %d", accountID, inv.AccountID)
	}
	if inv.Status != entity.InvoiceStatusIssued {
		t.Errorf("status mismatch: want issued, got %s", inv.Status)
	}
	if inv.TotalCNY != 99.00 {
		t.Errorf("total_cny mismatch: want 99.00, got %.2f", inv.TotalCNY)
	}
}

// TestInvoiceService_Generate_Idempotent verifies that calling Generate twice
// for the same order returns the same invoice without creating a duplicate.
func TestInvoiceService_Generate_Idempotent(t *testing.T) {
	svc, is, ws := makeInvoiceService()

	const accountID = int64(10)
	const orderNo = "LO20260101IDEM"
	seedPaidOrder(ws, accountID, orderNo)

	inv1, err := svc.Generate(context.Background(), accountID, orderNo)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}

	inv2, err := svc.Generate(context.Background(), accountID, orderNo)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}

	if inv1.InvoiceNo != inv2.InvoiceNo {
		t.Errorf("idempotency broken: got different invoice numbers %s vs %s",
			inv1.InvoiceNo, inv2.InvoiceNo)
	}

	// Confirm only one invoice record exists.
	list, total, _ := is.ListByAccount(context.Background(), accountID, 1, 100)
	if total != 1 {
		t.Errorf("expected 1 invoice, got %d (list len=%d)", total, len(list))
	}
}

// TestInvoiceService_Generate_OrderNotPaid verifies that invoices cannot be
// generated for orders that are not in paid status.
func TestInvoiceService_Generate_OrderNotPaid(t *testing.T) {
	svc, _, ws := makeInvoiceService()

	const accountID = int64(20)
	const orderNo = "LO20260101PEND"
	o := &entity.PaymentOrder{
		AccountID: accountID,
		OrderNo:   orderNo,
		OrderType: "topup",
		AmountCNY: 50.00,
		Status:    entity.OrderStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	_, err := svc.Generate(context.Background(), accountID, orderNo)
	if err == nil {
		t.Fatal("expected error for unpaid order, got nil")
	}
}

// TestInvoiceService_Generate_IDOR verifies that an account cannot generate an
// invoice for an order belonging to a different account.
func TestInvoiceService_Generate_IDOR(t *testing.T) {
	svc, _, ws := makeInvoiceService()

	const ownerID = int64(30)
	const attackerID = int64(31)
	const orderNo = "LO20260101IDOR"
	seedPaidOrder(ws, ownerID, orderNo)

	_, err := svc.Generate(context.Background(), attackerID, orderNo)
	if err == nil {
		t.Fatal("expected IDOR error, got nil")
	}
}

// TestInvoiceService_GetByNo_IDOR verifies that an account cannot retrieve an
// invoice belonging to a different account.
func TestInvoiceService_GetByNo_IDOR(t *testing.T) {
	svc, _, ws := makeInvoiceService()

	const ownerID = int64(40)
	const attackerID = int64(41)
	const orderNo = "LO20260101GETI"
	seedPaidOrder(ws, ownerID, orderNo)

	inv, err := svc.Generate(context.Background(), ownerID, orderNo)
	if err != nil {
		t.Fatalf("setup: generate invoice: %v", err)
	}

	_, err = svc.GetByNo(context.Background(), attackerID, inv.InvoiceNo)
	if err == nil {
		t.Fatal("expected IDOR error when fetching invoice cross-account, got nil")
	}
}

// TestInvoiceService_ListByAccount verifies that paginated listing returns invoices
// for the correct account only.
func TestInvoiceService_ListByAccount(t *testing.T) {
	svc, _, ws := makeInvoiceService()

	const accountID = int64(50)
	seedPaidOrder(ws, accountID, "LO20260101LISTA")
	seedPaidOrder(ws, accountID, "LO20260101LISTB")

	_, err := svc.Generate(context.Background(), accountID, "LO20260101LISTA")
	if err != nil {
		t.Fatalf("generate A: %v", err)
	}
	_, err = svc.Generate(context.Background(), accountID, "LO20260101LISTB")
	if err != nil {
		t.Fatalf("generate B: %v", err)
	}

	list, total, err := svc.ListByAccount(context.Background(), accountID, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 invoices, got %d (len=%d)", total, len(list))
	}
}

// TestInvoiceService_AdminList verifies that admin listing returns all invoices,
// and that filtering by account_id narrows the result.
func TestInvoiceService_AdminList(t *testing.T) {
	svc, _, ws := makeInvoiceService()

	const acc1 = int64(60)
	const acc2 = int64(61)
	seedPaidOrder(ws, acc1, "LO20260101ADM1")
	seedPaidOrder(ws, acc2, "LO20260101ADM2")

	_, _ = svc.Generate(context.Background(), acc1, "LO20260101ADM1")
	_, _ = svc.Generate(context.Background(), acc2, "LO20260101ADM2")

	// Filter 0 = all accounts.
	_, total, err := svc.AdminList(context.Background(), 0, 1, 10)
	if err != nil {
		t.Fatalf("admin list all: %v", err)
	}
	if total < 2 {
		t.Errorf("expected at least 2 invoices, got %d", total)
	}

	// Filtered by acc1.
	_, total1, err := svc.AdminList(context.Background(), acc1, 1, 10)
	if err != nil {
		t.Fatalf("admin list filtered: %v", err)
	}
	if total1 != 1 {
		t.Errorf("expected 1 invoice for acc1, got %d", total1)
	}
}

// errInvoiceStore returns an error from GetByInvoiceNo to cover the db-error branch.
type errInvoiceStore struct{ mockInvoiceStore }

func (s *errInvoiceStore) GetByInvoiceNo(_ context.Context, _ string) (*entity.Invoice, error) {
	return nil, fmt.Errorf("db error")
}

// TestInvoiceService_GetByNo_StoreError covers the GetByInvoiceNo error branch.
func TestInvoiceService_GetByNo_StoreError(t *testing.T) {
	is := &errInvoiceStore{*newMockInvoiceStore()}
	ws := newMockWalletStore()
	svc := NewInvoiceService(is, ws)

	_, err := svc.GetByNo(context.Background(), 1, "INV-DOESNOTMATTER")
	if err == nil {
		t.Fatal("expected error from store, got nil")
	}
}
