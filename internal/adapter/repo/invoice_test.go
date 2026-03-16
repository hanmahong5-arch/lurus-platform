package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestInvoiceRepo_CreateAndGetByOrderNo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewInvoiceRepo(db)
	ctx := context.Background()

	inv := &entity.Invoice{
		InvoiceNo: "INV-001", AccountID: 1, OrderNo: "LO-001",
		IssueDate: time.Now(), TotalCNY: 99.0, Currency: "CNY",
		LineItems: json.RawMessage(`[]`),
	}
	if err := repo.Create(ctx, inv); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByOrderNo(ctx, "LO-001")
	if err != nil {
		t.Fatalf("GetByOrderNo: %v", err)
	}
	if got == nil || got.InvoiceNo != "INV-001" {
		t.Errorf("got %+v", got)
	}

	// Not found
	got, err = repo.GetByOrderNo(ctx, "NONEXISTENT")
	if err != nil {
		t.Fatalf("GetByOrderNo not found: %v", err)
	}
	if got != nil {
		t.Error("expected nil")
	}
}

func TestInvoiceRepo_GetByInvoiceNo(t *testing.T) {
	db := setupTestDB(t)
	repo := NewInvoiceRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Invoice{
		InvoiceNo: "INV-002", AccountID: 1, OrderNo: "LO-002",
		IssueDate: time.Now(), TotalCNY: 50.0, LineItems: json.RawMessage(`[]`),
	})

	got, err := repo.GetByInvoiceNo(ctx, "INV-002")
	if err != nil {
		t.Fatalf("GetByInvoiceNo: %v", err)
	}
	if got == nil || got.TotalCNY != 50.0 {
		t.Errorf("got %+v", got)
	}

	got, _ = repo.GetByInvoiceNo(ctx, "NONEXISTENT")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestInvoiceRepo_ListByAccount(t *testing.T) {
	db := setupTestDB(t)
	repo := NewInvoiceRepo(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		repo.Create(ctx, &entity.Invoice{
			InvoiceNo: "INV-L" + string(rune('A'+i)),
			AccountID: 1, OrderNo: "LO-L" + string(rune('A'+i)),
			IssueDate: time.Now(), TotalCNY: float64(i * 10),
			LineItems: json.RawMessage(`[]`),
		})
	}

	list, total, err := repo.ListByAccount(ctx, 1, 1, 3)
	if err != nil {
		t.Fatalf("ListByAccount: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(list) != 3 {
		t.Errorf("len = %d, want 3", len(list))
	}

	// Different account
	list, total, _ = repo.ListByAccount(ctx, 999, 1, 10)
	if total != 0 || len(list) != 0 {
		t.Errorf("other account: total=%d len=%d", total, len(list))
	}
}

func TestInvoiceRepo_AdminList(t *testing.T) {
	db := setupTestDB(t)
	repo := NewInvoiceRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Invoice{
		InvoiceNo: "INV-A1", AccountID: 1, OrderNo: "LO-A1",
		IssueDate: time.Now(), TotalCNY: 10.0, LineItems: json.RawMessage(`[]`),
	})
	repo.Create(ctx, &entity.Invoice{
		InvoiceNo: "INV-A2", AccountID: 2, OrderNo: "LO-A2",
		IssueDate: time.Now(), TotalCNY: 20.0, LineItems: json.RawMessage(`[]`),
	})

	// All accounts
	list, total, err := repo.AdminList(ctx, 0, 1, 10)
	if err != nil {
		t.Fatalf("AdminList all: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(list) != 2 {
		t.Errorf("len = %d, want 2", len(list))
	}

	// Filter by account
	list, total, _ = repo.AdminList(ctx, 1, 1, 10)
	if total != 1 || len(list) != 1 {
		t.Errorf("filtered: total=%d len=%d, want 1/1", total, len(list))
	}
}
