package app

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestProductService_GetByID_Found(t *testing.T) {
	svc, ps := makeProductService()
	ps.products["llm-api"] = &entity.Product{ID: "llm-api", Name: "LLM API"}

	p, err := svc.GetByID(context.Background(), "llm-api")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if p == nil || p.Name != "LLM API" {
		t.Errorf("expected LLM API, got %v", p)
	}
}

func TestProductService_GetByID_Miss(t *testing.T) {
	svc, _ := makeProductService()

	p, err := svc.GetByID(context.Background(), "nope")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil, got %v", p)
	}
}

func TestProductService_GetPlanByID_Found(t *testing.T) {
	svc, ps := makeProductService()
	ps.plans[1] = &entity.ProductPlan{ID: 1, ProductID: "llm-api", Code: "pro"}

	p, err := svc.GetPlanByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if p == nil || p.Code != "pro" {
		t.Errorf("expected pro plan, got %v", p)
	}
}

func TestProductService_GetPlanByID_Miss(t *testing.T) {
	svc, _ := makeProductService()

	p, err := svc.GetPlanByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil, got %v", p)
	}
}

func TestProductService_ListPlans_Multiple(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	_ = svc.CreatePlan(ctx, &entity.ProductPlan{ProductID: "llm-api", Code: "free", Status: 1})
	_ = svc.CreatePlan(ctx, &entity.ProductPlan{ProductID: "llm-api", Code: "pro", Status: 1})

	list, err := svc.ListPlans(ctx, "llm-api")
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 plans, got %d", len(list))
	}
}

func TestProductService_CreatePlan_OK(t *testing.T) {
	svc, _ := makeProductService()
	err := svc.CreatePlan(context.Background(), &entity.ProductPlan{ProductID: "llm-api", Code: "trial"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
}
