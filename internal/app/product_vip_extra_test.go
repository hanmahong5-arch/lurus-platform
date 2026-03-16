package app

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── ProductService tests ──────────────────────────────────────────────────────

func makeProductService() (*ProductService, *mockPlanStore) {
	ps := newMockPlanStore()
	return NewProductService(ps), ps
}

func TestProductService_CreateAndGet(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	p := &entity.Product{ID: "quant", Name: "Quant Trading", Status: 1}
	if err := svc.CreateProduct(ctx, p); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	got, err := svc.GetByID(ctx, "quant")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil || got.Name != "Quant Trading" {
		t.Errorf("GetByID returned %+v", got)
	}
}

func TestProductService_UpdateProduct(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	p := &entity.Product{ID: "mail", Name: "Webmail", Status: 1}
	_ = svc.CreateProduct(ctx, p)
	p.Name = "Lurus Mail"
	if err := svc.UpdateProduct(ctx, p); err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	got, _ := svc.GetByID(ctx, "mail")
	if got.Name != "Lurus Mail" {
		t.Errorf("Name=%q, want Lurus Mail", got.Name)
	}
}

func TestProductService_ListActive(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	_ = svc.CreateProduct(ctx, &entity.Product{ID: "p1", Name: "P1", Status: 1})
	_ = svc.CreateProduct(ctx, &entity.Product{ID: "p2", Name: "P2", Status: 0}) // inactive
	list, err := svc.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("len(list)=%d, want 1 (only active)", len(list))
	}
}

func TestProductService_CreateAndListPlans(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	plan := &entity.ProductPlan{ProductID: "llm-api", Code: "pro", Status: 1}
	if err := svc.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	plans, err := svc.ListPlans(ctx, "llm-api")
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) == 0 {
		t.Error("expected at least one plan")
	}
}

func TestProductService_GetPlanByID(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	p := &entity.ProductPlan{ProductID: "llm-api", Code: "basic", Status: 1}
	_ = svc.CreatePlan(ctx, p)

	got, err := svc.GetPlanByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if got == nil || got.Code != "basic" {
		t.Errorf("GetPlanByID returned %+v", got)
	}
}

func TestProductService_UpdatePlan(t *testing.T) {
	svc, _ := makeProductService()
	ctx := context.Background()

	p := &entity.ProductPlan{ProductID: "llm-api", Code: "ent", Status: 1}
	_ = svc.CreatePlan(ctx, p)
	p.Code = "enterprise"
	if err := svc.UpdatePlan(ctx, p); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	got, _ := svc.GetPlanByID(ctx, p.ID)
	if got.Code != "enterprise" {
		t.Errorf("Code=%q, want enterprise", got.Code)
	}
}

// ── VIPService.AdminSet test ──────────────────────────────────────────────────

func TestVIPService_AdminSet(t *testing.T) {
	configs := defaultVIPConfigs()
	vipStore := newMockVIPStore(configs)
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)
	ctx := context.Background()

	if err := svc.AdminSet(ctx, 1, 3); err != nil {
		t.Fatalf("AdminSet: %v", err)
	}
	v, _ := svc.Get(ctx, 1)
	if v.Level != 3 {
		t.Errorf("Level=%d after AdminSet, want 3", v.Level)
	}
}

// ── ReferralService tests ─────────────────────────────────────────────────────

func makeReferralService() (*ReferralService, *mockAccountStore, *mockWalletStore) {
	as := newMockAccountStore()
	ws := newMockWalletStore()
	return NewReferralService(as, ws), as, ws
}

func TestReferralService_OnSignup(t *testing.T) {
	svc, as, _ := makeReferralService()
	ctx := context.Background()

	// Create referrer account
	referrer := &entity.Account{Email: "ref@example.com", DisplayName: "Ref", Status: 1}
	_ = as.Create(ctx, referrer)

	if err := svc.OnSignup(ctx, 999, referrer.ID); err != nil {
		t.Fatalf("OnSignup: %v", err)
	}
}

func TestReferralService_OnFirstTopup(t *testing.T) {
	svc, as, _ := makeReferralService()
	ctx := context.Background()

	referrer := &entity.Account{Email: "ref2@example.com", DisplayName: "Ref2", Status: 1}
	_ = as.Create(ctx, referrer)

	if err := svc.OnFirstTopup(ctx, 998, referrer.ID); err != nil {
		t.Fatalf("OnFirstTopup: %v", err)
	}
}

func TestReferralService_OnFirstSubscription(t *testing.T) {
	svc, as, _ := makeReferralService()
	ctx := context.Background()

	referrer := &entity.Account{Email: "ref3@example.com", DisplayName: "Ref3", Status: 1}
	_ = as.Create(ctx, referrer)

	if err := svc.OnFirstSubscription(ctx, 997, referrer.ID); err != nil {
		t.Fatalf("OnFirstSubscription: %v", err)
	}
}

func TestReferralService_ReferrerNotFound(t *testing.T) {
	svc, _, _ := makeReferralService()
	err := svc.OnSignup(context.Background(), 1, 9999) // referrer doesn't exist
	if err == nil {
		t.Error("expected error when referrer not found")
	}
}

// ── WalletService.MarkOrderPaid (new order path) ──────────────────────────────

func TestWalletService_MarkOrderPaid_NewOrder(t *testing.T) {
	svc, ws := makeWalletService()
	ctx := context.Background()
	_, _ = ws.GetOrCreate(ctx, 2)

	ws.orders["LO-NEW"] = &entity.PaymentOrder{
		AccountID: 2, OrderNo: "LO-NEW", OrderType: "topup",
		AmountCNY: 50.0, Status: entity.OrderStatusPending,
	}
	o, err := svc.MarkOrderPaid(ctx, "LO-NEW")
	if err != nil {
		t.Fatalf("MarkOrderPaid error: %v", err)
	}
	if o.Status != entity.OrderStatusPaid {
		t.Errorf("Status=%q, want paid", o.Status)
	}
	w, _ := svc.GetWallet(ctx, 2)
	if w.Balance != 50.0 {
		t.Errorf("balance=%.2f after MarkOrderPaid, want 50.00", w.Balance)
	}
}
