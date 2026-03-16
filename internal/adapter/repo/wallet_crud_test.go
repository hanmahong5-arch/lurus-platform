package repo

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestWalletRepo_GetOrCreate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	w, err := repo.GetOrCreate(ctx, 1)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if w == nil || w.AccountID != 1 {
		t.Fatalf("got %+v, want AccountID=1", w)
	}
	if w.Balance != 0 {
		t.Errorf("Balance = %f, want 0", w.Balance)
	}

	// Second call returns the same wallet
	w2, err := repo.GetOrCreate(ctx, 1)
	if err != nil {
		t.Fatalf("GetOrCreate second: %v", err)
	}
	if w2.ID != w.ID {
		t.Errorf("ID mismatch: %d vs %d", w2.ID, w.ID)
	}
}

func TestWalletRepo_GetByAccountID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	repo.GetOrCreate(ctx, 1)

	got, err := repo.GetByAccountID(ctx, 1)
	if err != nil {
		t.Fatalf("GetByAccountID: %v", err)
	}
	if got == nil || got.AccountID != 1 {
		t.Errorf("got %+v", got)
	}

	// Not found
	got, err = repo.GetByAccountID(ctx, 999)
	if err != nil {
		t.Fatalf("GetByAccountID not found: %v", err)
	}
	if got != nil {
		t.Error("expected nil")
	}
}

func TestWalletRepo_Credit(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	repo.GetOrCreate(ctx, 1)

	tx, err := repo.Credit(ctx, 1, 100.0, entity.TxTypeTopup, "test topup", "test", "ref-1", "")
	if err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if tx.Amount != 100.0 {
		t.Errorf("tx.Amount = %f, want 100", tx.Amount)
	}
	if tx.Type != entity.TxTypeTopup {
		t.Errorf("tx.Type = %q, want topup", tx.Type)
	}

	// Verify wallet balance
	w, _ := repo.GetByAccountID(ctx, 1)
	if w.Balance != 100.0 {
		t.Errorf("Balance = %f, want 100", w.Balance)
	}
	if w.LifetimeTopup != 100.0 {
		t.Errorf("LifetimeTopup = %f, want 100", w.LifetimeTopup)
	}
}

func TestWalletRepo_Debit(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	repo.GetOrCreate(ctx, 1)
	repo.Credit(ctx, 1, 200.0, entity.TxTypeTopup, "seed", "", "", "")

	tx, err := repo.Debit(ctx, 1, 50.0, entity.TxTypeSubscription, "monthly sub", "sub", "sub-1", "lurus_api")
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if tx.Amount != -50.0 {
		t.Errorf("tx.Amount = %f, want -50", tx.Amount)
	}

	w, _ := repo.GetByAccountID(ctx, 1)
	if w.Balance != 150.0 {
		t.Errorf("Balance = %f, want 150", w.Balance)
	}
	if w.LifetimeSpend != 50.0 {
		t.Errorf("LifetimeSpend = %f, want 50", w.LifetimeSpend)
	}
}

func TestWalletRepo_Debit_InsufficientFunds(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	repo.GetOrCreate(ctx, 1) // balance = 0

	_, err := repo.Debit(ctx, 1, 10.0, entity.TxTypeSubscription, "fail", "", "", "")
	if err == nil {
		t.Fatal("expected error for insufficient funds")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Errorf("error = %q, want containing 'insufficient balance'", err.Error())
	}
}

func TestWalletRepo_ListTransactions(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	repo.GetOrCreate(ctx, 1)
	repo.Credit(ctx, 1, 100.0, entity.TxTypeTopup, "first", "", "", "")
	repo.Credit(ctx, 1, 50.0, entity.TxTypeBonus, "bonus", "", "", "")

	list, total, err := repo.ListTransactions(ctx, 1, 1, 10)
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}
}

func TestWalletRepo_PaymentOrderCRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	order := &entity.PaymentOrder{
		AccountID:     1,
		OrderNo:       "LO20260227000001",
		OrderType:     "topup",
		AmountCNY:     99.0,
		PaymentMethod: "stripe",
		ExternalID:    "pi_123",
	}
	if err := repo.CreatePaymentOrder(ctx, order); err != nil {
		t.Fatalf("CreatePaymentOrder: %v", err)
	}

	got, err := repo.GetPaymentOrderByNo(ctx, "LO20260227000001")
	if err != nil {
		t.Fatalf("GetByNo: %v", err)
	}
	if got == nil || got.AmountCNY != 99.0 {
		t.Errorf("got %+v", got)
	}

	// Update status
	got.Status = entity.OrderStatusPaid
	if err := repo.UpdatePaymentOrder(ctx, got); err != nil {
		t.Fatalf("UpdatePaymentOrder: %v", err)
	}
	updated, _ := repo.GetPaymentOrderByNo(ctx, "LO20260227000001")
	if updated.Status != entity.OrderStatusPaid {
		t.Errorf("Status = %q, want paid", updated.Status)
	}

	// Not found
	got, _ = repo.GetPaymentOrderByNo(ctx, "NONEXISTENT")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestWalletRepo_GetPaymentOrderByExternalID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	repo.CreatePaymentOrder(ctx, &entity.PaymentOrder{
		AccountID: 1, OrderNo: "LO001", OrderType: "topup",
		AmountCNY: 50.0, ExternalID: "ext-abc",
	})

	got, err := repo.GetPaymentOrderByExternalID(ctx, "ext-abc")
	if err != nil {
		t.Fatalf("GetByExternalID: %v", err)
	}
	if got == nil || got.OrderNo != "LO001" {
		t.Errorf("got %+v", got)
	}

	got, _ = repo.GetPaymentOrderByExternalID(ctx, "nonexistent")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestWalletRepo_ListOrders(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		repo.CreatePaymentOrder(ctx, &entity.PaymentOrder{
			AccountID: 1, OrderNo: fmt.Sprintf("LO-test-%d", i+1),
			OrderType: "topup", AmountCNY: float64((i + 1) * 10),
		})
	}

	list, total, err := repo.ListOrders(ctx, 1, 1, 10)
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(list) != 3 {
		t.Errorf("len = %d, want 3", len(list))
	}
}

func TestWalletRepo_RedemptionCodeCRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	rc := &entity.RedemptionCode{
		Code: "PROMO-001", RewardType: "credits", RewardValue: 10.0, MaxUses: 1,
	}
	if err := repo.CreateRedemptionCode(ctx, rc); err != nil {
		t.Fatalf("CreateRedemptionCode: %v", err)
	}

	got, err := repo.GetRedemptionCode(ctx, "PROMO-001")
	if err != nil {
		t.Fatalf("GetRedemptionCode: %v", err)
	}
	if got == nil || got.RewardValue != 10.0 {
		t.Errorf("got %+v", got)
	}

	// Update
	got.UsedCount = 1
	if err := repo.UpdateRedemptionCode(ctx, got); err != nil {
		t.Fatalf("UpdateRedemptionCode: %v", err)
	}
	updated, _ := repo.GetRedemptionCode(ctx, "PROMO-001")
	if updated.UsedCount != 1 {
		t.Errorf("UsedCount = %d, want 1", updated.UsedCount)
	}

	// Not found
	got, _ = repo.GetRedemptionCode(ctx, "NONEXISTENT")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestWalletRepo_BulkCreate(t *testing.T) {
	db := setupTestDB(t)
	repo := NewWalletRepo(db)
	ctx := context.Background()

	codes := []entity.RedemptionCode{
		{Code: "BULK-001", RewardType: "credits", RewardValue: 5.0, MaxUses: 1},
		{Code: "BULK-002", RewardType: "credits", RewardValue: 5.0, MaxUses: 1},
		{Code: "BULK-003", RewardType: "credits", RewardValue: 5.0, MaxUses: 1},
	}
	if err := repo.BulkCreate(ctx, codes); err != nil {
		t.Fatalf("BulkCreate: %v", err)
	}

	// Verify all codes created
	for _, code := range []string{"BULK-001", "BULK-002", "BULK-003"} {
		got, err := repo.GetRedemptionCode(ctx, code)
		if err != nil {
			t.Fatalf("GetRedemptionCode(%s): %v", code, err)
		}
		if got == nil {
			t.Errorf("code %s not found", code)
		}
	}

	// Empty slice - should not error
	if err := repo.BulkCreate(ctx, nil); err != nil {
		t.Fatalf("BulkCreate nil: %v", err)
	}
}
