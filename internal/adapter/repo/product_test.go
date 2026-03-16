package repo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestProductRepo_CreateAndGetByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewProductRepo(db)
	ctx := context.Background()

	p := &entity.Product{
		ID: "lurus_api", Name: "Lurus API", Description: "LLM gateway",
		BillingModel: "hybrid", Status: 1,
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, "lurus_api")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil || got.Name != "Lurus API" {
		t.Errorf("got %+v", got)
	}
}

func TestProductRepo_GetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewProductRepo(db)

	got, err := repo.GetByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Error("expected nil")
	}
}

func TestProductRepo_ListActive(t *testing.T) {
	db := setupTestDB(t)
	repo := NewProductRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Product{ID: "p1", Name: "Active", BillingModel: "free", Status: 1})
	// Use -1 instead of 0 because GORM omits zero-value fields during INSERT,
	// which falls back to the database default (1). Any non-1 value is inactive.
	repo.Create(ctx, &entity.Product{ID: "p2", Name: "Inactive", BillingModel: "free", Status: -1})

	list, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(list) != 1 || list[0].ID != "p1" {
		t.Errorf("list = %+v, want 1 active product", list)
	}
}

func TestProductRepo_Update(t *testing.T) {
	db := setupTestDB(t)
	repo := NewProductRepo(db)
	ctx := context.Background()

	p := &entity.Product{ID: "upd", Name: "Before", BillingModel: "free", Status: 1}
	repo.Create(ctx, p)

	p.Name = "After"
	if err := repo.Update(ctx, p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := repo.GetByID(ctx, "upd")
	if got.Name != "After" {
		t.Errorf("Name = %q, want After", got.Name)
	}
}

func TestProductRepo_PlanCRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := NewProductRepo(db)
	ctx := context.Background()

	repo.Create(ctx, &entity.Product{ID: "lurus_api", Name: "API", BillingModel: "hybrid", Status: 1})

	plan := &entity.ProductPlan{
		ProductID: "lurus_api", Code: "pro", Name: "Pro Plan",
		BillingCycle: "monthly", PriceCNY: 99.0, Status: 1,
		Features: json.RawMessage(`{"max_rpm":1000}`),
	}
	if err := repo.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID == 0 {
		t.Fatal("expected non-zero plan ID")
	}

	// GetPlanByID
	got, err := repo.GetPlanByID(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if got == nil || got.Code != "pro" {
		t.Errorf("got %+v", got)
	}

	// GetPlanByCode
	got, err = repo.GetPlanByCode(ctx, "lurus_api", "pro")
	if err != nil {
		t.Fatalf("GetPlanByCode: %v", err)
	}
	if got == nil || got.Name != "Pro Plan" {
		t.Errorf("got %+v", got)
	}

	// GetPlanByCode not found
	got, _ = repo.GetPlanByCode(ctx, "lurus_api", "nonexistent")
	if got != nil {
		t.Error("expected nil for non-existent plan code")
	}

	// ListPlans
	repo.CreatePlan(ctx, &entity.ProductPlan{
		ProductID: "lurus_api", Code: "free", Name: "Free Plan",
		BillingCycle: "forever", Status: 1,
		Features: json.RawMessage(`{}`),
	})
	plans, err := repo.ListPlans(ctx, "lurus_api")
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 2 {
		t.Errorf("plans count = %d, want 2", len(plans))
	}

	// UpdatePlan
	got, _ = repo.GetPlanByID(ctx, plan.ID)
	got.PriceCNY = 129.0
	if err := repo.UpdatePlan(ctx, got); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	updated, _ := repo.GetPlanByID(ctx, plan.ID)
	if updated.PriceCNY != 129.0 {
		t.Errorf("PriceCNY = %.2f, want 129.00", updated.PriceCNY)
	}
}

func TestProductRepo_GetPlanByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewProductRepo(db)

	got, err := repo.GetPlanByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Error("expected nil")
	}
}
