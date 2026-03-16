package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// mockOverviewCache implements overviewCache for testing.
type mockOverviewCache struct {
	data map[string][]byte
}

func newMockOverviewCache() *mockOverviewCache {
	return &mockOverviewCache{data: make(map[string][]byte)}
}

func (m *mockOverviewCache) Get(_ context.Context, accountID int64, productID string) ([]byte, error) {
	k := overviewCacheKey(accountID, productID)
	v, ok := m.data[k]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *mockOverviewCache) Set(_ context.Context, accountID int64, productID string, data []byte) error {
	m.data[overviewCacheKey(accountID, productID)] = data
	return nil
}

func (m *mockOverviewCache) Invalidate(_ context.Context, accountID int64, productID string) error {
	delete(m.data, overviewCacheKey(accountID, productID))
	return nil
}

func overviewCacheKey(accountID int64, productID string) string {
	return string(rune(accountID)) + ":" + productID
}

func makeOverviewService() (*OverviewService, *mockAccountStore, *mockOverviewCache) {
	as := newMockAccountStore()
	ws := newMockWalletStore()
	vs := NewVIPService(newMockVIPStore(nil), ws)
	ss := NewSubscriptionService(newMockSubStore(), newMockPlanStore(), NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache()), 3)
	ps := newMockPlanStore()
	oc := newMockOverviewCache()
	svc := NewOverviewService(as, vs, ws, ss, ps, oc)
	return svc, as, oc
}

func TestOverviewService_Get_CacheMiss(t *testing.T) {
	svc, as, _ := makeOverviewService()
	ctx := context.Background()

	// Seed an account
	_ = as.Create(ctx, &entity.Account{DisplayName: "Alice", ZitadelSub: "sub-1", LurusID: "LU0000001"})

	ov, err := svc.Get(ctx, 1, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ov.Account.DisplayName != "Alice" {
		t.Errorf("DisplayName=%q, want Alice", ov.Account.DisplayName)
	}
	if ov.TopupURL != topupURL {
		t.Errorf("TopupURL=%q, want %q", ov.TopupURL, topupURL)
	}
}

func TestOverviewService_Get_CacheHit(t *testing.T) {
	svc, as, oc := makeOverviewService()
	ctx := context.Background()

	_ = as.Create(ctx, &entity.Account{DisplayName: "Bob", ZitadelSub: "sub-2", LurusID: "LU0000002"})

	// Warm the cache by calling Get
	_, _ = svc.Get(ctx, 1, "")

	// Cache should now be populated
	if len(oc.data) == 0 {
		t.Error("expected cache to be populated after first Get")
	}

	// Second call should use cache (no error even if underlying data is fine)
	ov, err := svc.Get(ctx, 1, "")
	if err != nil {
		t.Fatalf("cached Get: %v", err)
	}
	if ov.Account.DisplayName != "Bob" {
		t.Errorf("DisplayName=%q, want Bob", ov.Account.DisplayName)
	}
}

func TestOverviewService_Get_AccountNotFound(t *testing.T) {
	svc, _, _ := makeOverviewService()

	_, err := svc.Get(context.Background(), 999, "")
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestOverviewService_Get_WithProductID(t *testing.T) {
	svc, as, _ := makeOverviewService()
	ctx := context.Background()

	_ = as.Create(ctx, &entity.Account{DisplayName: "Charlie", ZitadelSub: "sub-3", LurusID: "LU0000003"})

	// No active subscription → Subscription should be nil
	ov, err := svc.Get(ctx, 1, "llm-api")
	if err != nil {
		t.Fatalf("Get with productID: %v", err)
	}
	if ov.Subscription != nil {
		t.Error("expected nil Subscription when no active sub exists")
	}
}

// ── error-returning stores for compute error-path coverage ────────────────────

type errAccountStoreOv struct{ mockAccountStore }

func (s *errAccountStoreOv) GetByID(_ context.Context, _ int64) (*entity.Account, error) {
	return nil, fmt.Errorf("account db error")
}

type errVIPStoreOv struct{ mockVIPStore }

func (s *errVIPStoreOv) GetOrCreate(_ context.Context, _ int64) (*entity.AccountVIP, error) {
	return nil, fmt.Errorf("vip db error")
}

type errWalletStoreOv struct{ mockWalletStore }

func (s *errWalletStoreOv) GetOrCreate(_ context.Context, _ int64) (*entity.Wallet, error) {
	return nil, fmt.Errorf("wallet db error")
}

type errSubStoreOv struct{ mockSubStore }

func (s *errSubStoreOv) GetActive(_ context.Context, _ int64, _ string) (*entity.Subscription, error) {
	return nil, fmt.Errorf("sub db error")
}

func TestOverviewService_compute_GetByIDError(t *testing.T) {
	as := &errAccountStoreOv{*newMockAccountStore()}
	ws := newMockWalletStore()
	vs := NewVIPService(newMockVIPStore(nil), ws)
	ss := NewSubscriptionService(newMockSubStore(), newMockPlanStore(), NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache()), 3)
	ps := newMockPlanStore()
	oc := newMockOverviewCache()
	svc := NewOverviewService(as, vs, ws, ss, ps, oc)

	_, err := svc.Get(context.Background(), 1, "")
	if err == nil {
		t.Fatal("expected error when GetByID fails")
	}
}

func TestOverviewService_compute_VIPError(t *testing.T) {
	as := newMockAccountStore()
	ctx := context.Background()
	_ = as.Create(ctx, &entity.Account{DisplayName: "VIPErr", ZitadelSub: "sub-ve", LurusID: "LU0000010"})
	ws := newMockWalletStore()
	vs := NewVIPService(&errVIPStoreOv{*newMockVIPStore(nil)}, ws)
	ss := NewSubscriptionService(newMockSubStore(), newMockPlanStore(), NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache()), 3)
	ps := newMockPlanStore()
	oc := newMockOverviewCache()
	svc := NewOverviewService(as, vs, ws, ss, ps, oc)

	_, err := svc.Get(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error when VIP.Get fails")
	}
}

func TestOverviewService_compute_WalletError(t *testing.T) {
	as := newMockAccountStore()
	ctx := context.Background()
	_ = as.Create(ctx, &entity.Account{DisplayName: "WalletErr", ZitadelSub: "sub-we", LurusID: "LU0000011"})
	errWs := &errWalletStoreOv{*newMockWalletStore()}
	vs := NewVIPService(newMockVIPStore(nil), newMockWalletStore())
	ss := NewSubscriptionService(newMockSubStore(), newMockPlanStore(), NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache()), 3)
	ps := newMockPlanStore()
	oc := newMockOverviewCache()
	svc := NewOverviewService(as, vs, errWs, ss, ps, oc)

	_, err := svc.Get(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error when wallet.GetOrCreate fails")
	}
}

func TestOverviewService_compute_SubError(t *testing.T) {
	as := newMockAccountStore()
	ctx := context.Background()
	_ = as.Create(ctx, &entity.Account{DisplayName: "SubErr", ZitadelSub: "sub-sube", LurusID: "LU0000012"})
	ws := newMockWalletStore()
	vs := NewVIPService(newMockVIPStore(nil), ws)
	errSubs := &errSubStoreOv{*newMockSubStore()}
	ss := NewSubscriptionService(errSubs, newMockPlanStore(), NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache()), 3)
	ps := newMockPlanStore()
	oc := newMockOverviewCache()
	svc := NewOverviewService(as, vs, ws, ss, ps, oc)

	// non-empty productID triggers GetActive
	_, err := svc.Get(ctx, 1, "lurus_api")
	if err == nil {
		t.Fatal("expected error when SubscriptionService.GetActive fails")
	}
}

func TestOverviewService_compute_SubscriptionFound(t *testing.T) {
	as := newMockAccountStore()
	ctx := context.Background()
	_ = as.Create(ctx, &entity.Account{DisplayName: "SubFound", ZitadelSub: "sub-sf", LurusID: "LU0000013"})
	ws := newMockWalletStore()
	vs := NewVIPService(newMockVIPStore(nil), ws)

	// Seed a plan and an active subscription
	ps := newMockPlanStore()
	_ = ps.CreatePlan(ctx, &entity.ProductPlan{ProductID: "lurus_api", Code: "basic", PriceCNY: 29.9, Status: 1})

	subStore := newMockSubStore()
	exp := time.Now().Add(30 * 24 * time.Hour)
	_ = subStore.Create(ctx, &entity.Subscription{
		AccountID: 1, ProductID: "lurus_api", PlanID: 1,
		Status: entity.SubStatusActive, ExpiresAt: &exp,
	})

	ss := NewSubscriptionService(subStore, ps, NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache()), 3)
	oc := newMockOverviewCache()
	svc := NewOverviewService(as, vs, ws, ss, ps, oc)

	ov, err := svc.Get(ctx, 1, "lurus_api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ov.Subscription == nil {
		t.Fatal("expected Subscription to be non-nil")
	}
	if ov.Subscription.PlanCode != "basic" {
		t.Errorf("PlanCode=%q, want basic", ov.Subscription.PlanCode)
	}
	if ov.Subscription.ProductID != "lurus_api" {
		t.Errorf("ProductID=%q, want lurus_api", ov.Subscription.ProductID)
	}
}

