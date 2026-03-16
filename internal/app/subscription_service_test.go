package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// makeSubscriptionService builds a SubscriptionService wired with in-memory mocks.
func makeSubscriptionService() (*SubscriptionService, *mockSubStore, *mockPlanStore) {
	sub := newMockSubStore()
	plan := newMockPlanStore()
	cache := newMockCache()
	ents := NewEntitlementService(sub, plan, cache)
	return NewSubscriptionService(sub, plan, ents, 3), sub, plan
}

// seedPlan inserts a monthly plan with the given code into the mock plan store.
func seedPlan(t *testing.T, ctx context.Context, ps *mockPlanStore, productID, code string) *entity.ProductPlan {
	t.Helper()
	features, _ := json.Marshal(map[string]any{"plan_code": code})
	p := &entity.ProductPlan{
		ProductID: productID, Code: code, Status: 1,
		BillingCycle: entity.BillingCycleMonthly, Features: features,
	}
	if err := ps.CreatePlan(ctx, p); err != nil {
		t.Fatalf("seedPlan: %v", err)
	}
	return p
}

func TestSubscriptionService_Activate_NewSub(t *testing.T) {
	svc, _, ps := makeSubscriptionService()
	ctx := context.Background()
	plan := seedPlan(t, ctx, ps, "llm-api", "pro")

	sub, err := svc.Activate(ctx, 1, "llm-api", plan.ID, "wallet", "")
	if err != nil {
		t.Fatalf("Activate error: %v", err)
	}
	if sub.Status != entity.SubStatusActive {
		t.Errorf("Status=%q, want active", sub.Status)
	}
	if sub.ExpiresAt == nil {
		t.Error("ExpiresAt should be set for monthly plan")
	}
	if sub.PlanID != plan.ID {
		t.Errorf("PlanID=%d, want %d", sub.PlanID, plan.ID)
	}
}

func TestSubscriptionService_Activate_RenewExpiresPrevious(t *testing.T) {
	svc, ss, ps := makeSubscriptionService()
	ctx := context.Background()
	plan := seedPlan(t, ctx, ps, "llm-api", "pro")

	// First activation
	first, _ := svc.Activate(ctx, 1, "llm-api", plan.ID, "wallet", "")
	// Renew — should expire the first one
	_, err := svc.Activate(ctx, 1, "llm-api", plan.ID, "stripe", "sub_xyz")
	if err != nil {
		t.Fatalf("Activate (renew) error: %v", err)
	}
	// Old sub should be expired
	old, _ := ss.GetByID(ctx, first.ID)
	if old.Status != entity.SubStatusExpired {
		t.Errorf("old sub status=%q, want expired", old.Status)
	}
}

func TestSubscriptionService_Expire(t *testing.T) {
	svc, ss, ps := makeSubscriptionService()
	ctx := context.Background()
	plan := seedPlan(t, ctx, ps, "llm-api", "pro")

	sub, _ := svc.Activate(ctx, 1, "llm-api", plan.ID, "wallet", "")
	if err := svc.Expire(ctx, sub.ID); err != nil {
		t.Fatalf("Expire error: %v", err)
	}
	updated, _ := ss.GetByID(ctx, sub.ID)
	if updated.Status != entity.SubStatusGrace {
		t.Errorf("Status=%q after Expire, want grace", updated.Status)
	}
	if updated.GraceUntil == nil {
		t.Error("GraceUntil should be set after Expire")
	}
}

func TestSubscriptionService_EndGrace(t *testing.T) {
	svc, ss, ps := makeSubscriptionService()
	ctx := context.Background()
	plan := seedPlan(t, ctx, ps, "llm-api", "pro")

	sub, _ := svc.Activate(ctx, 1, "llm-api", plan.ID, "wallet", "")
	_ = svc.Expire(ctx, sub.ID)
	if err := svc.EndGrace(ctx, sub.ID); err != nil {
		t.Fatalf("EndGrace error: %v", err)
	}
	updated, _ := ss.GetByID(ctx, sub.ID)
	if updated.Status != entity.SubStatusExpired {
		t.Errorf("Status=%q after EndGrace, want expired", updated.Status)
	}
}

func TestSubscriptionService_Cancel(t *testing.T) {
	svc, ss, ps := makeSubscriptionService()
	ctx := context.Background()
	plan := seedPlan(t, ctx, ps, "llm-api", "pro")

	sub, _ := svc.Activate(ctx, 1, "llm-api", plan.ID, "wallet", "")
	if err := svc.Cancel(ctx, 1, "llm-api"); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}
	updated, _ := ss.GetByID(ctx, sub.ID)
	if updated.Status != entity.SubStatusCancelled {
		t.Errorf("Status=%q after Cancel, want cancelled", updated.Status)
	}
	if updated.AutoRenew {
		t.Error("AutoRenew should be false after Cancel")
	}
}

func TestSubscriptionService_GetActive_None(t *testing.T) {
	svc, _, _ := makeSubscriptionService()
	sub, err := svc.GetActive(context.Background(), 99, "llm-api")
	if err != nil {
		t.Fatalf("GetActive error: %v", err)
	}
	if sub != nil {
		t.Errorf("expected nil for account with no subscription, got %+v", sub)
	}
}

func TestSubscriptionService_ListByAccount(t *testing.T) {
	svc, _, ps := makeSubscriptionService()
	ctx := context.Background()
	plan := seedPlan(t, ctx, ps, "llm-api", "pro")

	_, _ = svc.Activate(ctx, 1, "llm-api", plan.ID, "wallet", "")
	subs, err := svc.ListByAccount(ctx, 1)
	if err != nil {
		t.Fatalf("ListByAccount error: %v", err)
	}
	// At least one active + any previously expired (from renew test isolation — here just 1)
	if len(subs) == 0 {
		t.Error("expected at least one subscription")
	}
}

// TestCalculateExpiry tests the billing cycle → expiry date logic.
func TestCalculateExpiry(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		cycle   string
		wantNil bool        // nil means perpetual (forever/one_time)
		wantY   int
		wantM   time.Month
		wantD   int
	}{
		{entity.BillingCycleForever, true, 0, 0, 0},
		{entity.BillingCycleOneTime, true, 0, 0, 0},
		{entity.BillingCycleWeekly, false, 2026, 1, 8},
		{entity.BillingCycleMonthly, false, 2026, 2, 1},
		{entity.BillingCycleQuarterly, false, 2026, 4, 1},
		{entity.BillingCycleYearly, false, 2027, 1, 1},
		{"unknown_cycle", false, 2026, 2, 1}, // defaults to monthly
	}

	for _, tc := range tests {
		got := calculateExpiry(base, tc.cycle)
		if tc.wantNil {
			if got != nil {
				t.Errorf("cycle=%q: expected nil expiry, got %v", tc.cycle, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("cycle=%q: expected non-nil expiry", tc.cycle)
			continue
		}
		y, m, d := got.Date()
		if y != tc.wantY || m != tc.wantM || d != tc.wantD {
			t.Errorf("cycle=%q: got %v, want %04d-%02d-%02d",
				tc.cycle, got, tc.wantY, tc.wantM, tc.wantD)
		}
	}
}

// TestCalculateExpiry_MonthBoundary checks month-end clamping edge cases.
func TestCalculateExpiry_MonthBoundary(t *testing.T) {
	tests := []struct {
		name      string
		from      time.Time
		cycle     string
		wantYear  int
		wantMonth time.Month
		wantDay   int
	}{
		{
			name:      "jan31+monthly → feb28 (no overflow to march)",
			from:      time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			cycle:     entity.BillingCycleMonthly,
			wantYear:  2026, wantMonth: time.February, wantDay: 28,
		},
		{
			name:      "jan31+monthly → feb29 (leap year)",
			from:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			cycle:     entity.BillingCycleMonthly,
			wantYear:  2024, wantMonth: time.February, wantDay: 29,
		},
		{
			name:      "mar31+quarterly → jun30",
			from:      time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
			cycle:     entity.BillingCycleQuarterly,
			wantYear:  2026, wantMonth: time.June, wantDay: 30,
		},
		{
			name:      "mid-month no clamping needed",
			from:      time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			cycle:     entity.BillingCycleMonthly,
			wantYear:  2026, wantMonth: time.February, wantDay: 15,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateExpiry(tc.from, tc.cycle)
			if got == nil {
				t.Fatal("expected non-nil expiry")
			}
			y, m, d := got.Date()
			if y != tc.wantYear || m != tc.wantMonth || d != tc.wantDay {
				t.Errorf("got %04d-%02d-%02d, want %04d-%02d-%02d",
					y, m, d, tc.wantYear, tc.wantMonth, tc.wantDay)
			}
		})
	}
}

// TestDaysInMonth verifies the month-length helper.
func TestDaysInMonth(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		want  int
	}{
		{2026, time.January, 31},
		{2026, time.February, 28},
		{2024, time.February, 29}, // leap year
		{2026, time.April, 30},
		{2026, time.December, 31},
	}
	for _, tc := range tests {
		got := daysInMonth(tc.year, tc.month)
		if got != tc.want {
			t.Errorf("daysInMonth(%d, %v)=%d, want %d", tc.year, tc.month, got, tc.want)
		}
	}
}

// TestGracePeriodDefault ensures the default grace period is applied when 0 is passed.
func TestGracePeriodDefault(t *testing.T) {
	svc := NewSubscriptionService(newMockSubStore(), newMockPlanStore(), nil, 0)
	if svc.gracePeriodDays != 3 {
		t.Errorf("gracePeriodDays=%d, want 3 (default)", svc.gracePeriodDays)
	}
}
