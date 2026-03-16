package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestAnyToString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{true, "true"},
		{false, "false"},
		{float64(42), "42"},
		{float64(3.14), "3.14"},
		{float64(500000), "500000"},
		{float64(-1), "-1"},
		{float64(0), "0"},
	}
	for _, tc := range tests {
		got := anyToString(tc.input)
		if got != tc.want {
			t.Errorf("anyToString(%v)=%q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestInferValueType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"true", "boolean"},
		{"false", "boolean"},
		{"42", "integer"},
		{"-1", "integer"},
		{"0", "integer"},
		{"3.14", "decimal"},
		{"0.5", "decimal"},
		{"hello", "string"},
		{"pro", "string"},
		{"", "string"},
	}
	for _, tc := range tests {
		got := inferValueType(tc.input)
		if got != tc.want {
			t.Errorf("inferValueType(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAnyToString_LargeInt(t *testing.T) {
	got := anyToString(float64(5000000))
	if got != "5000000" {
		t.Errorf("anyToString(5000000)=%q, want \"5000000\"", got)
	}
}

// ── EntitlementService integration tests ─────────────────────────────────────

func makeEntitlementService() (*EntitlementService, *mockSubStore, *mockPlanStore) {
	sub := newMockSubStore()
	plan := newMockPlanStore()
	c := newMockCache()
	return NewEntitlementService(sub, plan, c), sub, plan
}

func TestEntitlementService_Get_DefaultsFreeWhenNoRows(t *testing.T) {
	svc, _, _ := makeEntitlementService()
	em, err := svc.Get(context.Background(), 1, "llm-api")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if em["plan_code"] != "free" {
		t.Errorf("plan_code=%q, want free", em["plan_code"])
	}
}

func TestEntitlementService_SyncFromSubscription(t *testing.T) {
	svc, subStore, planStore := makeEntitlementService()
	ctx := context.Background()

	// Seed plan with features
	features, _ := json.Marshal(map[string]any{
		"daily_quota":  float64(5000000),
		"model_group":  "pro",
		"real_money":   false,
	})
	plan := &entity.ProductPlan{
		ProductID: "llm-api", Code: "pro", Status: 1,
		BillingCycle: "monthly", Features: features,
	}
	_ = planStore.CreatePlan(ctx, plan)

	sub := &entity.Subscription{
		AccountID: 1, ProductID: "llm-api", PlanID: plan.ID,
		Status: entity.SubStatusActive,
	}
	_ = subStore.Create(ctx, sub)

	if err := svc.SyncFromSubscription(ctx, sub); err != nil {
		t.Fatalf("SyncFromSubscription error: %v", err)
	}

	em, _ := svc.Get(ctx, 1, "llm-api")
	if em["plan_code"] != "pro" {
		t.Errorf("plan_code=%q, want pro", em["plan_code"])
	}
	if em["daily_quota"] != "5000000" {
		t.Errorf("daily_quota=%q, want 5000000", em["daily_quota"])
	}
	if em["model_group"] != "pro" {
		t.Errorf("model_group=%q, want pro", em["model_group"])
	}
	if em["real_money"] != "false" {
		t.Errorf("real_money=%q, want false", em["real_money"])
	}
}

func TestEntitlementService_ResetToFree(t *testing.T) {
	svc, subStore, planStore := makeEntitlementService()
	ctx := context.Background()

	// First sync a pro subscription
	features, _ := json.Marshal(map[string]any{"daily_quota": float64(5000000), "model_group": "pro"})
	plan := &entity.ProductPlan{ProductID: "llm-api", Code: "pro", Status: 1, Features: features}
	_ = planStore.CreatePlan(ctx, plan)
	sub := &entity.Subscription{AccountID: 1, ProductID: "llm-api", PlanID: plan.ID, Status: entity.SubStatusActive}
	_ = subStore.Create(ctx, sub)
	_ = svc.SyncFromSubscription(ctx, sub)

	// Now reset to free
	if err := svc.ResetToFree(ctx, 1, "llm-api"); err != nil {
		t.Fatalf("ResetToFree error: %v", err)
	}

	em, _ := svc.Get(ctx, 1, "llm-api")
	if em["plan_code"] != "free" {
		t.Errorf("plan_code=%q after reset, want free", em["plan_code"])
	}
	// Pro-specific keys should be gone
	if _, ok := em["daily_quota"]; ok {
		t.Error("daily_quota should be removed after reset to free")
	}
}

func TestEntitlementService_CacheHit(t *testing.T) {
	_, subStore, planStore := makeEntitlementService()
	mockC := newMockCache()
	svc := NewEntitlementService(subStore, planStore, mockC)
	ctx := context.Background()

	// Pre-warm cache
	mockC.data["1:llm-api"] = map[string]string{"plan_code": "gold", "model_group": "gold"}

	em, err := svc.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if em["plan_code"] != "gold" {
		t.Errorf("plan_code=%q, want gold (cache hit)", em["plan_code"])
	}
}

// errDeleteSubStore returns an error from DeleteEntitlements to cover the ResetToFree error branch.
type errDeleteSubStore struct{ mockSubStore }

func (s *errDeleteSubStore) DeleteEntitlements(_ context.Context, _ int64, _ string) error {
	return fmt.Errorf("db delete error")
}

// TestEntitlementService_ResetToFree_DeleteError covers the DeleteEntitlements error branch.
func TestEntitlementService_ResetToFree_DeleteError(t *testing.T) {
	errSub := &errDeleteSubStore{*newMockSubStore()}
	svc := NewEntitlementService(errSub, newMockPlanStore(), newMockCache())

	err := svc.ResetToFree(context.Background(), 1, "llm-api")
	if err == nil {
		t.Fatal("expected error from DeleteEntitlements, got nil")
	}
}
