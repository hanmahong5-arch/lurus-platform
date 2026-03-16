package repo

import (
	"context"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestRefundRepo_CreateAndGetByRefundNo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefundRepo(db)
	ctx := context.Background()

	ref := &entity.Refund{
		RefundNo: "REF-001", AccountID: 1, OrderNo: "LO-001",
		AmountCNY: 50.0, Reason: "defective",
	}
	if err := repo.Create(ctx, ref); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByRefundNo(ctx, "REF-001")
	if err != nil {
		t.Fatalf("GetByRefundNo: %v", err)
	}
	if got == nil || got.AmountCNY != 50.0 {
		t.Errorf("got %+v", got)
	}
}

func TestRefundRepo_GetByRefundNo_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefundRepo(db)

	got, err := repo.GetByRefundNo(context.Background(), "NONEXISTENT")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Error("expected nil")
	}
}

func TestRefundRepo_GetPendingByOrderNo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefundRepo(db)
	ctx := context.Background()

	// Pending refund
	repo.Create(ctx, &entity.Refund{
		RefundNo: "REF-P1", AccountID: 1, OrderNo: "LO-P",
		AmountCNY: 30.0, Status: entity.RefundStatusPending,
	})

	got, err := repo.GetPendingByOrderNo(ctx, "LO-P")
	if err != nil {
		t.Fatalf("GetPendingByOrderNo: %v", err)
	}
	if got == nil || got.RefundNo != "REF-P1" {
		t.Errorf("got %+v", got)
	}

	// Completed refund (should NOT be returned)
	repo.Create(ctx, &entity.Refund{
		RefundNo: "REF-C1", AccountID: 1, OrderNo: "LO-C",
		AmountCNY: 20.0, Status: entity.RefundStatusCompleted,
	})
	got, _ = repo.GetPendingByOrderNo(ctx, "LO-C")
	if got != nil {
		t.Error("expected nil for completed refund")
	}

	// No refund
	got, _ = repo.GetPendingByOrderNo(ctx, "NONEXISTENT")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestRefundRepo_UpdateStatus(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefundRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Refund{
		RefundNo: "REF-U1", AccountID: 1, OrderNo: "LO-U",
		AmountCNY: 40.0, Status: entity.RefundStatusPending,
	})

	now := time.Now()
	err := repo.UpdateStatus(ctx, "REF-U1", string(entity.RefundStatusPending), string(entity.RefundStatusApproved), "looks good", "admin-1", &now)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := repo.GetByRefundNo(ctx, "REF-U1")
	if got.Status != entity.RefundStatusApproved {
		t.Errorf("Status = %q, want approved", got.Status)
	}
	if got.ReviewedBy != "admin-1" {
		t.Errorf("ReviewedBy = %q, want admin-1", got.ReviewedBy)
	}
}

func TestRefundRepo_MarkCompleted(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefundRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Refund{
		RefundNo: "REF-M1", AccountID: 1, OrderNo: "LO-M",
		AmountCNY: 25.0, Status: entity.RefundStatusApproved,
	})

	now := time.Now()
	if err := repo.MarkCompleted(ctx, "REF-M1", now); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	got, _ := repo.GetByRefundNo(ctx, "REF-M1")
	if got.Status != entity.RefundStatusCompleted {
		t.Errorf("Status = %q, want completed", got.Status)
	}
}

func TestRefundRepo_ListByAccount(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRefundRepo(db)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		repo.Create(ctx, &entity.Refund{
			RefundNo:  "REF-L" + string(rune('A'+i)),
			AccountID: 1, OrderNo: "LO-L" + string(rune('A'+i)),
			AmountCNY: float64(i * 10),
		})
	}

	list, total, err := repo.ListByAccount(ctx, 1, 1, 2)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}
}
