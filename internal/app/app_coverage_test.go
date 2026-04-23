package app

// app_coverage_test.go — targeted tests to push internal/app coverage from 79.5% to ≥ 90%.
// Uses existing in-memory mock pattern from mock_test.go.
// No production files are modified.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// ─── shared test helpers ─────────────────────────────────────────────────────

func newBasicVIPConfigs() []entity.VIPLevelConfig {
	return []entity.VIPLevelConfig{
		{Level: 0, Name: "Standard", MinSpendCNY: 0},
		{Level: 1, Name: "Silver", MinSpendCNY: 100},
		{Level: 2, Name: "Gold", MinSpendCNY: 500},
	}
}

// errVIPStore is a vipStore that injects errors on demand.
type errVIPStore struct {
	*mockVIPStore
	listConfigsErr error
	getOrCreateErr error
	updateErr      error
}

func (m *errVIPStore) ListConfigs(ctx context.Context) ([]entity.VIPLevelConfig, error) {
	if m.listConfigsErr != nil {
		return nil, m.listConfigsErr
	}
	return m.mockVIPStore.ListConfigs(ctx)
}

func (m *errVIPStore) GetOrCreate(ctx context.Context, accountID int64) (*entity.AccountVIP, error) {
	if m.getOrCreateErr != nil {
		return nil, m.getOrCreateErr
	}
	return m.mockVIPStore.GetOrCreate(ctx, accountID)
}

func (m *errVIPStore) Update(ctx context.Context, v *entity.AccountVIP) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return m.mockVIPStore.Update(ctx, v)
}

// ─── mock types not present in other test files ───────────────────────────────

// mockRefundPublisher2 implements RefundPublisher (named 2 to avoid conflict with
// mockPublisher in coverage_fix_test.go).
type mockRefundPublisher2 struct {
	mu     sync.Mutex
	events []*event.IdentityEvent
}

func (m *mockRefundPublisher2) Publish(_ context.Context, ev *event.IdentityEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

// mockReferralStats implements referralStatsStore.
type mockReferralStats struct {
	total    int
	rewarded float64
	err      error
}

func (m *mockReferralStats) GetReferralStats(_ context.Context, _ int64) (int, float64, error) {
	return m.total, m.rewarded, m.err
}

// mockRewardEvents implements rewardEventStore.
type mockRewardEvents struct {
	mu      sync.Mutex
	events  []*entity.ReferralRewardEvent
	created bool
	err     error
}

func (m *mockRewardEvents) CreateRewardEvent(_ context.Context, ev *entity.ReferralRewardEvent) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	m.events = append(m.events, ev)
	return m.created, nil
}

// newTestAdminSettingStore creates an in-memory store with pre-seeded settings.
func newTestAdminSettingStore(settings ...entity.AdminSetting) *mockAdminSettingStore {
	return &mockAdminSettingStore{settings: settings}
}

// mockReconWalletStore extends mockWalletStore (pointer embed) with reconciliation
// query methods that can return configurable results.  Named distinctly from the
// value-embed version in reconciliation_worker_test.go.
type mockReconWalletStore struct {
	*mockWalletStore
	expireOrdersCalled  atomic.Int32
	expirePreAuthCalled atomic.Int32
	orphans             []entity.PaidOrderWithoutCredit
	orphansErr          error
	staleOrders         []entity.PaymentOrder
	staleErr            error
}

func (m *mockReconWalletStore) ExpireStalePendingOrders(_ context.Context, _ time.Duration) (int64, error) {
	m.expireOrdersCalled.Add(1)
	return 0, nil
}

func (m *mockReconWalletStore) ExpireStalePreAuths(_ context.Context) (int64, error) {
	m.expirePreAuthCalled.Add(1)
	return 0, nil
}

func (m *mockReconWalletStore) FindPaidTopupOrdersWithoutCredit(_ context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	return m.orphans, m.orphansErr
}

func (m *mockReconWalletStore) FindStalePendingOrders(_ context.Context, _ time.Duration) ([]entity.PaymentOrder, error) {
	return m.staleOrders, m.staleErr
}

func newTestReconWorker(rs *mockReconWalletStore) *ReconciliationWorker {
	vipSvc := NewVIPService(newMockVIPStore(nil), rs.mockWalletStore)
	walletSvc := NewWalletService(rs, vipSvc)
	return NewReconciliationWorker(walletSvc, nil) // payments=nil
}

// ─── AccountService ───────────────────────────────────────────────────────────

func TestAccountService_SetOnAccountCreatedHook_Fires(t *testing.T) {
	svc := NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil))
	called := false
	svc.SetOnAccountCreatedHook(func(_ context.Context, _ *entity.Account) {
		called = true
	})
	_, err := svc.UpsertByZitadelSub(context.Background(), "sub-hook-test", "hook@example.com", "Hook User", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub: %v", err)
	}
	// Hook fires in a goroutine — wait briefly.
	time.Sleep(20 * time.Millisecond)
	if !called {
		t.Fatal("expected onAccountCreated hook to be called")
	}
}

func TestAccountService_UpsertByZitadelSub_UpdatesDisplayName(t *testing.T) {
	store := newMockAccountStore()
	svc := NewAccountService(store, newMockWalletStore(), newMockVIPStore(nil))
	a, err := svc.UpsertByZitadelSub(context.Background(), "sub-upd", "user@example.com", "Old Name", "")
	if err != nil || a == nil {
		t.Fatalf("initial upsert: %v", err)
	}
	a2, err := svc.UpsertByZitadelSub(context.Background(), "sub-upd", "user@example.com", "New Name", "")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if a2.DisplayName != "New Name" {
		t.Errorf("expected 'New Name', got %q", a2.DisplayName)
	}
}

func TestAccountService_UpsertByZitadelSub_LinksExistingByEmail(t *testing.T) {
	store := newMockAccountStore()
	svc := NewAccountService(store, newMockWalletStore(), newMockVIPStore(nil))
	_ = store.Create(context.Background(), &entity.Account{Email: "existing@example.com", Status: entity.AccountStatusActive})
	a, err := svc.UpsertByZitadelSub(context.Background(), "new-sub-abc", "existing@example.com", "Display", "")
	if err != nil || a == nil {
		t.Fatalf("upsert by email: %v", err)
	}
	if a.ZitadelSub != "new-sub-abc" {
		t.Errorf("expected ZitadelSub linked, got %q", a.ZitadelSub)
	}
}

func TestAccountService_UpsertByWechat_EmptyID_Errors(t *testing.T) {
	svc := NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil))
	_, err := svc.UpsertByWechat(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty wechatID")
	}
}

func TestAccountService_UpsertByWechat_CreatesNewAccount(t *testing.T) {
	svc := NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil))
	a, err := svc.UpsertByWechat(context.Background(), "wx_openid_123")
	if err != nil || a == nil {
		t.Fatalf("UpsertByWechat new user: %v", err)
	}
	if a.LurusID == "" {
		t.Error("expected LurusID to be set")
	}
}

func TestAccountService_UpsertByWechat_Idempotent(t *testing.T) {
	svc := NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil))
	a1, _ := svc.UpsertByWechat(context.Background(), "wx_openid_idem")
	a2, err := svc.UpsertByWechat(context.Background(), "wx_openid_idem")
	if err != nil || a2 == nil {
		t.Fatalf("second UpsertByWechat: %v", err)
	}
	if a2.ID != a1.ID {
		t.Errorf("expected same account ID; got %d, want %d", a2.ID, a1.ID)
	}
}

// ─── VIPService ───────────────────────────────────────────────────────────────

func TestVIPService_AdminSet_SetsLevelAndName(t *testing.T) {
	vipStore := newMockVIPStore(newBasicVIPConfigs())
	svc := NewVIPService(vipStore, newMockWalletStore())

	if err := svc.AdminSet(context.Background(), 1, 2); err != nil {
		t.Fatalf("AdminSet: %v", err)
	}
	v, _ := svc.Get(context.Background(), 1)
	if v.Level != 2 {
		t.Errorf("expected level 2, got %d", v.Level)
	}
	if v.LevelName != "Gold" {
		t.Errorf("expected 'Gold', got %q", v.LevelName)
	}
}

func TestVIPService_AdminSet_UnknownLevelFallsBackToStandard(t *testing.T) {
	svc := NewVIPService(newMockVIPStore(newBasicVIPConfigs()), newMockWalletStore())
	if err := svc.AdminSet(context.Background(), 1, 99); err != nil {
		t.Fatalf("AdminSet level 99: %v", err)
	}
	v, _ := svc.Get(context.Background(), 1)
	if v.LevelName != "Standard" {
		t.Errorf("expected 'Standard', got %q", v.LevelName)
	}
}

func TestVIPService_RecalculateFromWallet_NilWalletIsNoop(t *testing.T) {
	svc := NewVIPService(newMockVIPStore(newBasicVIPConfigs()), newMockWalletStore())
	if err := svc.RecalculateFromWallet(context.Background(), 9999); err != nil {
		t.Fatalf("RecalculateFromWallet with no wallet: %v", err)
	}
}

func TestVIPService_GrantYearlySub_SetsGrantAndLevel(t *testing.T) {
	svc := NewVIPService(newMockVIPStore(newBasicVIPConfigs()), newMockWalletStore())
	if err := svc.GrantYearlySub(context.Background(), 1, 2); err != nil {
		t.Fatalf("GrantYearlySub: %v", err)
	}
	v, _ := svc.Get(context.Background(), 1)
	if v.Level != 2 || v.YearlySubGrant != 2 {
		t.Errorf("expected level=2 yearlySubGrant=2, got level=%d grant=%d", v.Level, v.YearlySubGrant)
	}
}

// ─── WalletService ────────────────────────────────────────────────────────────

func TestWalletService_Topup_CreditErrorPropagates(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	ws.creditErr = errors.New("credit store error")
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))

	_, err := svc.Topup(context.Background(), 1, 50.0, "ORDER-ERR-1")
	if err == nil {
		t.Fatal("expected error from Topup when credit fails")
	}
}

func TestWalletService_Credit_SuccessUpdatesBalance(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))

	tx, err := svc.Credit(context.Background(), 1, 20.0, entity.TxTypeBonus, "admin bonus", "admin", "ref-1", "")
	if err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if tx.Amount != 20.0 {
		t.Errorf("expected tx amount 20.0, got %f", tx.Amount)
	}
}

func TestWalletService_Credit_ErrorPropagates(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	ws.creditErr = errors.New("credit fail")
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))

	_, err := svc.Credit(context.Background(), 1, 10.0, entity.TxTypeBonus, "desc", "ref", "id", "")
	if err == nil {
		t.Fatal("expected error from Credit when store fails")
	}
}

func TestWalletService_GetBillingSummary_NoWalletReturnsZero(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	summary, err := svc.GetBillingSummary(context.Background(), 9999)
	if err != nil {
		t.Fatalf("GetBillingSummary for missing wallet: %v", err)
	}
	if summary.Balance != 0 {
		t.Errorf("expected zero balance, got %f", summary.Balance)
	}
}

func TestWalletService_PreAuthorize_ZeroAmountErrors(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	_, err := svc.PreAuthorize(context.Background(), 1, 0, "prod", "ref", "desc", 0)
	if err == nil {
		t.Fatal("expected error for zero pre-auth amount")
	}
}

func TestWalletService_SettlePreAuth_NegativeAmountErrors(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	_, err := svc.SettlePreAuth(context.Background(), 1, -1.0)
	if err == nil {
		t.Fatal("expected error for negative settle amount")
	}
}

func TestWalletService_GetOrderByNo_WrongAccountIsError(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	o, _ := svc.CreateTopup(context.Background(), 1, 10.0, "stripe")
	_, err := svc.GetOrderByNo(context.Background(), 2, o.OrderNo)
	if err == nil {
		t.Fatal("expected error for wrong account ownership")
	}
}

// ─── SubscriptionService ─────────────────────────────────────────────────────

func newSubSvcForCoverage() (*SubscriptionService, *mockSubStore, *mockPlanStore) {
	subStore := newMockSubStore()
	planStore := newMockPlanStore()
	ents := NewEntitlementService(subStore, planStore, newMockCache())
	svc := NewSubscriptionService(subStore, planStore, ents, 3)
	return svc, subStore, planStore
}

func TestSubscriptionService_Activate_MissingPlanErrors(t *testing.T) {
	svc, _, _ := newSubSvcForCoverage()
	_, err := svc.Activate(context.Background(), 1, "prod-api", 999, "stripe", "")
	if err == nil {
		t.Fatal("expected error for missing plan")
	}
}

func TestSubscriptionService_Expire_NonActiveErrors(t *testing.T) {
	svc, subStore, planStore := newSubSvcForCoverage()
	_ = planStore.CreatePlan(context.Background(), &entity.ProductPlan{
		ProductID: "prod-api", BillingCycle: entity.BillingCycleMonthly, Status: 1,
	})
	sub, err := svc.Activate(context.Background(), 1, "prod-api", 1, "stripe", "")
	if err != nil || sub == nil {
		t.Fatalf("activate: %v", err)
	}
	s, _ := subStore.GetByID(context.Background(), sub.ID)
	s.Status = entity.SubStatusCancelled
	_ = subStore.Update(context.Background(), s)

	if err := svc.Expire(context.Background(), sub.ID); err == nil {
		t.Fatal("expected error for expiring a non-active subscription")
	}
}

func TestSubscriptionService_EndGrace_ActiveSubErrors(t *testing.T) {
	svc, _, planStore := newSubSvcForCoverage()
	_ = planStore.CreatePlan(context.Background(), &entity.ProductPlan{
		ProductID: "prod-api", BillingCycle: entity.BillingCycleMonthly, Status: 1,
	})
	sub, err := svc.Activate(context.Background(), 1, "prod-api", 1, "stripe", "")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := svc.EndGrace(context.Background(), sub.ID); err == nil {
		t.Fatal("expected error for ending grace on active sub")
	}
}

func TestSubscriptionService_Cancel_NoActiveErrors(t *testing.T) {
	svc, _, _ := newSubSvcForCoverage()
	if err := svc.Cancel(context.Background(), 99, "nonexistent-product"); err == nil {
		t.Fatal("expected error when no active subscription exists")
	}
}

func TestSubscriptionService_calculateExpiry_AllCycles(t *testing.T) {
	now := time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		cycle   string
		wantNil bool
		wantY   int
		wantM   time.Month
		wantD   int
	}{
		{entity.BillingCycleWeekly, false, 2024, 2, 7},
		{entity.BillingCycleMonthly, false, 2024, 2, 29},
		{entity.BillingCycleQuarterly, false, 2024, 4, 30},
		{entity.BillingCycleYearly, false, 2025, 1, 31},
		{entity.BillingCycleForever, true, 0, 0, 0},
		{entity.BillingCycleOneTime, true, 0, 0, 0},
		{"unknown_cycle_x", false, 2024, 2, 29},
	}
	for _, tc := range cases {
		exp := calculateExpiry(now, tc.cycle)
		if tc.wantNil {
			if exp != nil {
				t.Errorf("cycle %q: expected nil expiry, got %v", tc.cycle, exp)
			}
			continue
		}
		if exp == nil {
			t.Errorf("cycle %q: expected non-nil expiry", tc.cycle)
			continue
		}
		if exp.Year() != tc.wantY || exp.Month() != tc.wantM || exp.Day() != tc.wantD {
			t.Errorf("cycle %q: got %v, want %d-%02d-%02d",
				tc.cycle, exp.Format("2006-01-02"), tc.wantY, tc.wantM, tc.wantD)
		}
	}
}

// ─── ReferralService ─────────────────────────────────────────────────────────

func TestReferralService_WithRewardEvents_DedupReturnsNil(t *testing.T) {
	accounts := newMockAccountStore()
	_ = accounts.Create(context.Background(), &entity.Account{Email: "referrer@example.com", Status: entity.AccountStatusActive})
	events := &mockRewardEvents{created: false}
	svc := NewReferralService(accounts, newMockWalletStore()).WithRewardEvents(events)
	if err := svc.OnSignup(context.Background(), 99, 1); err != nil {
		t.Fatalf("OnSignup dedup: %v", err)
	}
}

func TestReferralService_WithRewardEvents_CreatesEvent(t *testing.T) {
	accounts := newMockAccountStore()
	_ = accounts.Create(context.Background(), &entity.Account{Email: "referrer@example.com", Status: entity.AccountStatusActive})
	events := &mockRewardEvents{created: true}
	svc := NewReferralService(accounts, newMockWalletStore()).WithRewardEvents(events)
	if err := svc.OnSignup(context.Background(), 99, 1); err != nil {
		t.Fatalf("OnSignup reward: %v", err)
	}
	if len(events.events) != 1 {
		t.Errorf("expected 1 reward event, got %d", len(events.events))
	}
}

func TestReferralService_WithRewardEvents_StoreErrorPropagates(t *testing.T) {
	accounts := newMockAccountStore()
	_ = accounts.Create(context.Background(), &entity.Account{Email: "referrer@example.com", Status: entity.AccountStatusActive})
	events := &mockRewardEvents{err: errors.New("store error")}
	svc := NewReferralService(accounts, newMockWalletStore()).WithRewardEvents(events)
	if err := svc.OnSignup(context.Background(), 99, 1); err == nil {
		t.Fatal("expected error when reward event store fails")
	}
}

func TestReferralService_OnRenewal_AfterWindowIsNoop(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore())
	if err := svc.OnRenewal(context.Background(), 1, 2, 100.0, 7); err != nil {
		t.Fatalf("OnRenewal after window: %v", err)
	}
}

func TestReferralService_OnRenewal_ZeroAmountIsNoop(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore())
	if err := svc.OnRenewal(context.Background(), 1, 2, 0.0, 1); err != nil {
		t.Fatalf("OnRenewal zero amount: %v", err)
	}
}

func TestReferralService_GetStats_WithStoreReturnsValues(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore()).WithStats(
		&mockReferralStats{total: 5, rewarded: 42.5},
	)
	refs, lb, err := svc.GetStats(context.Background(), 1)
	if err != nil || refs != 5 || lb != 42.5 {
		t.Fatalf("GetStats: refs=%d lb=%f err=%v", refs, lb, err)
	}
}

func TestReferralService_BulkGenerateCodes_CountZeroErrors(t *testing.T) {
	svc := NewReferralServiceWithCodes(newMockAccountStore(), newMockWalletStore(), newMockRedemptionCodeStore())
	_, err := svc.BulkGenerateCodes(context.Background(), "prod-api", "monthly", 30, nil, "", 0)
	if err == nil {
		t.Fatal("expected error for count=0")
	}
}

func TestReferralService_BulkGenerateCodes_CountOverLimitErrors(t *testing.T) {
	svc := NewReferralServiceWithCodes(newMockAccountStore(), newMockWalletStore(), newMockRedemptionCodeStore())
	_, err := svc.BulkGenerateCodes(context.Background(), "prod-api", "monthly", 30, nil, "", 1001)
	if err == nil {
		t.Fatal("expected error for count=1001")
	}
}

func TestReferralService_BulkGenerateCodes_NilStoreErrors(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore())
	_, err := svc.BulkGenerateCodes(context.Background(), "prod-api", "monthly", 30, nil, "", 5)
	if err == nil {
		t.Fatal("expected error when redemption store is nil")
	}
}

func TestReferralService_BulkGenerateCodes_BulkCreateErrorPropagates(t *testing.T) {
	rcStore := newMockRedemptionCodeStore()
	rcStore.err = errors.New("db error")
	svc := NewReferralServiceWithCodes(newMockAccountStore(), newMockWalletStore(), rcStore)
	_, err := svc.BulkGenerateCodes(context.Background(), "prod-api", "monthly", 30, nil, "", 5)
	if err == nil {
		t.Fatal("expected error from BulkCreate failure")
	}
}

// ─── RefundService ────────────────────────────────────────────────────────────

func newRefundSvcSetup() (*RefundService, *mockWalletStore, *mockRefundStore) {
	ws := newMockWalletStore()
	rs := newMockRefundStore()
	pub := &mockRefundPublisher2{}
	outbox := &mockEventOutbox{}
	return NewRefundService(rs, ws, pub, outbox), ws, rs
}

func makePaidOrderForRefund(ws *mockWalletStore) string {
	now := time.Now().UTC()
	orderNo := fmt.Sprintf("LO%d", now.UnixNano())
	o := &entity.PaymentOrder{
		AccountID: 1, OrderNo: orderNo, OrderType: "topup",
		AmountCNY: 50.0, Currency: "CNY", PaymentMethod: "stripe",
		Status: entity.OrderStatusPaid, CreatedAt: now,
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	return orderNo
}

func TestRefundService_RequestRefund_MissingOrderErrors(t *testing.T) {
	svc, _, _ := newRefundSvcSetup()
	_, err := svc.RequestRefund(context.Background(), 1, "NONEXISTENT", "test")
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

func TestRefundService_RequestRefund_WrongAccountErrors(t *testing.T) {
	svc, ws, _ := newRefundSvcSetup()
	orderNo := makePaidOrderForRefund(ws)
	_, err := svc.RequestRefund(context.Background(), 2, orderNo, "test")
	if err == nil {
		t.Fatal("expected error for wrong account")
	}
}

func TestRefundService_RequestRefund_NonPaidOrderErrors(t *testing.T) {
	svc, ws, _ := newRefundSvcSetup()
	o := &entity.PaymentOrder{
		AccountID: 1, OrderNo: "ORDER-PENDING", AmountCNY: 10.0,
		Status: entity.OrderStatusPending, CreatedAt: time.Now(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	_, err := svc.RequestRefund(context.Background(), 1, "ORDER-PENDING", "test")
	if err == nil {
		t.Fatal("expected error for non-paid order")
	}
}

func TestRefundService_RequestRefund_ExpiredWindowErrors(t *testing.T) {
	svc, ws, _ := newRefundSvcSetup()
	old := time.Now().UTC().AddDate(0, 0, -(refundWindowDays + 1))
	o := &entity.PaymentOrder{
		AccountID: 1, OrderNo: "ORDER-OLD", AmountCNY: 10.0,
		Status: entity.OrderStatusPaid, CreatedAt: old,
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	_, err := svc.RequestRefund(context.Background(), 1, "ORDER-OLD", "test")
	if err == nil {
		t.Fatal("expected error for expired refund window")
	}
}

func TestRefundService_RequestRefund_DuplicateErrors(t *testing.T) {
	svc, ws, _ := newRefundSvcSetup()
	orderNo := makePaidOrderForRefund(ws)
	if _, err := svc.RequestRefund(context.Background(), 1, orderNo, "first"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := svc.RequestRefund(context.Background(), 1, orderNo, "duplicate"); err == nil {
		t.Fatal("expected error for duplicate refund request")
	}
}

func TestRefundService_GetByNo_WrongAccountErrors(t *testing.T) {
	svc, ws, _ := newRefundSvcSetup()
	orderNo := makePaidOrderForRefund(ws)
	r, err := svc.RequestRefund(context.Background(), 1, orderNo, "test")
	if err != nil {
		t.Fatalf("request refund: %v", err)
	}
	_, err = svc.GetByNo(context.Background(), 2, r.RefundNo)
	if err == nil {
		t.Fatal("expected error for wrong account on GetByNo")
	}
}

func TestRefundService_Approve_RefundNotFoundErrors(t *testing.T) {
	svc, _, _ := newRefundSvcSetup()
	err := svc.Approve(context.Background(), "NONEXISTENT-LR", "admin1", "note")
	if err == nil {
		t.Fatal("expected error for missing refund")
	}
}

func TestRefundService_Approve_NonPendingErrors(t *testing.T) {
	svc, ws, rs := newRefundSvcSetup()
	ws.GetOrCreate(context.Background(), 1)
	orderNo := makePaidOrderForRefund(ws)
	r, _ := svc.RequestRefund(context.Background(), 1, orderNo, "test")
	_ = rs.UpdateStatus(context.Background(), r.RefundNo,
		string(entity.RefundStatusPending), string(entity.RefundStatusRejected), "", "admin", nil)
	if err := svc.Approve(context.Background(), r.RefundNo, "admin1", "note"); err == nil {
		t.Fatal("expected error for approving non-pending refund")
	}
}

func TestRefundService_Approve_OutboxReceivesEvent(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	rs := newMockRefundStore()
	outbox := &mockEventOutbox{}
	svc := NewRefundService(rs, ws, &mockRefundPublisher2{}, outbox)

	orderNo := makePaidOrderForRefund(ws)
	r, _ := svc.RequestRefund(context.Background(), 1, orderNo, "test")
	if err := svc.Approve(context.Background(), r.RefundNo, "admin", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(outbox.events) == 0 {
		t.Fatal("expected event in outbox after approval")
	}
}

func TestRefundService_Approve_NilOutboxFallsBackToPublisher(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	rs := newMockRefundStore()
	pub := &mockRefundPublisher2{}
	svc := NewRefundService(rs, ws, pub, nil) // nil outbox

	orderNo := makePaidOrderForRefund(ws)
	r, _ := svc.RequestRefund(context.Background(), 1, orderNo, "test")
	if err := svc.Approve(context.Background(), r.RefundNo, "admin", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(pub.events) == 0 {
		t.Fatal("expected event published via fallback publisher")
	}
}

func TestRefundService_Reject_NonPendingErrors(t *testing.T) {
	svc, ws, _ := newRefundSvcSetup()
	ws.GetOrCreate(context.Background(), 1)
	orderNo := makePaidOrderForRefund(ws)
	r, _ := svc.RequestRefund(context.Background(), 1, orderNo, "test")
	// First approve to change state.
	_ = svc.Approve(context.Background(), r.RefundNo, "admin", "approved")
	// Reject should now fail (completed state, not pending).
	if err := svc.Reject(context.Background(), r.RefundNo, "admin", "reject"); err == nil {
		t.Fatal("expected error for rejecting non-pending refund")
	}
}

// ─── ReconciliationWorker ────────────────────────────────────────────────────

func TestReconWorker_SetOnAlertHook_FiresForOrphan(t *testing.T) {
	rs := &mockReconWalletStore{mockWalletStore: newMockWalletStore()}
	w := newTestReconWorker(rs)

	called := false
	w.SetOnAlertHook(func(_ context.Context, _ *entity.ReconciliationIssue) {
		called = true
	})

	amt := 99.0
	rs.orphans = []entity.PaidOrderWithoutCredit{
		{OrderNo: "TEST-ORD", AccountID: 1, AmountCNY: amt, PaymentMethod: "stripe"},
	}
	w.checkPaidOrdersIntegrity(context.Background())
	if !called {
		t.Fatal("expected alert hook to be called for orphan order")
	}
}

func TestReconWorker_checkPaidOrdersIntegrity_NoOrphansIsNoop(t *testing.T) {
	rs := &mockReconWalletStore{mockWalletStore: newMockWalletStore()}
	w := newTestReconWorker(rs)
	w.checkPaidOrdersIntegrity(context.Background())
}

func TestReconWorker_checkPaidOrdersIntegrity_StoreErrorIsHandled(t *testing.T) {
	rs := &mockReconWalletStore{
		mockWalletStore: newMockWalletStore(),
		orphansErr:      errors.New("db error"),
	}
	w := newTestReconWorker(rs)
	w.checkPaidOrdersIntegrity(context.Background())
}

func TestReconWorker_verifyStalePendingOrders_NilPaymentsReturnsEarly(t *testing.T) {
	rs := &mockReconWalletStore{
		mockWalletStore: newMockWalletStore(),
		staleOrders:     []entity.PaymentOrder{{OrderNo: "STALE-1", PaymentMethod: "stripe"}},
	}
	w := newTestReconWorker(rs)
	w.verifyStalePendingOrders(context.Background())
}

func TestReconWorker_verifyStalePendingOrders_WithRegistryAndStaleError(t *testing.T) {
	rs := &mockReconWalletStore{
		mockWalletStore: newMockWalletStore(),
		staleErr:        errors.New("store error"),
	}
	vipSvc := NewVIPService(newMockVIPStore(nil), rs.mockWalletStore)
	walletSvc := NewWalletService(rs, vipSvc)
	reg := payment.NewRegistry()
	w := NewReconciliationWorker(walletSvc, reg)
	w.verifyStalePendingOrders(context.Background())
}

func TestReconWorker_verifyStalePendingOrders_WithRegistryEmptyResult(t *testing.T) {
	rs := &mockReconWalletStore{mockWalletStore: newMockWalletStore()}
	vipSvc := NewVIPService(newMockVIPStore(nil), rs.mockWalletStore)
	walletSvc := NewWalletService(rs, vipSvc)
	reg := payment.NewRegistry()
	w := NewReconciliationWorker(walletSvc, reg)
	w.verifyStalePendingOrders(context.Background())
}

func TestReconWorker_fireAlert_NoHookIsNoop(t *testing.T) {
	rs := &mockReconWalletStore{mockWalletStore: newMockWalletStore()}
	w := newTestReconWorker(rs)
	w.fireAlert(context.Background(), &entity.ReconciliationIssue{IssueType: "test"})
}

func TestReconWorker_Start_StopsOnContextCancel(t *testing.T) {
	rs := &mockReconWalletStore{mockWalletStore: newMockWalletStore()}
	w := newTestReconWorker(rs)
	w.interval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

// ─── InvoiceService uncovered paths ──────────────────────────────────────────

func TestInvoiceService_GetByNo_MissingErrors(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewInvoiceService(newMockInvoiceStore(), ws)
	_, err := svc.GetByNo(context.Background(), 1, "INV-NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for missing invoice")
	}
}

func TestInvoiceService_Generate_IdempotentOnSecondCall(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	orderNo := "ORDER-INV-IDEM"
	o := &entity.PaymentOrder{
		AccountID: 1, OrderNo: orderNo, AmountCNY: 20.0,
		OrderType: "topup", Status: entity.OrderStatusPaid, CreatedAt: time.Now(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	svc := NewInvoiceService(newMockInvoiceStore(), ws)

	if _, err := svc.Generate(context.Background(), 1, orderNo); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	inv2, err := svc.Generate(context.Background(), 1, orderNo)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if inv2 == nil {
		t.Fatal("expected existing invoice returned on second generate")
	}
}

// ─── AdminConfigService uncovered paths ─────────────────────────────────────
// (Additional coverage for paths not hit by admin_config_service_test.go)

func TestAdminConfigService_Get_NoCacheHitFallsBackToDB(t *testing.T) {
	// Do NOT call Load — force a cache miss, then verify DB fallback.
	store := newTestAdminSettingStore(entity.AdminSetting{Key: "missed_key", Value: "db_val"})
	svc := NewAdminConfigService(store)
	val, err := svc.Get(context.Background(), "missed_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "db_val" {
		t.Errorf("expected 'db_val', got %q", val)
	}
}

// ─── WalletService — pass-through reconciliation methods ────────────────────

func TestWalletService_ListReconciliationIssues_PassThrough(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	issues, total, err := svc.ListReconciliationIssues(context.Background(), "open", 1, 10)
	if err != nil || total != 0 || issues != nil {
		t.Fatalf("ListReconciliationIssues: issues=%v total=%d err=%v", issues, total, err)
	}
}

func TestWalletService_ResolveReconciliationIssue_PassThrough(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	if err := svc.ResolveReconciliationIssue(context.Background(), 1, "resolved", "manual fix"); err != nil {
		t.Fatalf("ResolveReconciliationIssue: %v", err)
	}
}

// TestWalletService_GetCheckoutStatus_* tests exist in wallet_checkout_preauth_test.go.

func TestWalletService_CreateCheckoutSession_IdempotencyKey(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	// First call creates.
	o1, err := svc.CreateCheckoutSession(context.Background(), 1, 10.0, "stripe", "svc-a", "idem-key-1", 0)
	if err != nil || o1 == nil {
		t.Fatalf("first CreateCheckoutSession: %v", err)
	}
	// Second call with same key returns existing.
	o2, err := svc.CreateCheckoutSession(context.Background(), 1, 10.0, "stripe", "svc-a", "idem-key-1", 0)
	if err != nil || o2 == nil {
		t.Fatalf("second CreateCheckoutSession: %v", err)
	}
	if o2.OrderNo != o1.OrderNo {
		t.Errorf("expected idempotent return of same order, got different: %s vs %s", o2.OrderNo, o1.OrderNo)
	}
}

func TestWalletService_CreateCheckoutSession_ZeroAmountErrors(t *testing.T) {
	ws := newMockWalletStore()
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws))
	_, err := svc.CreateCheckoutSession(context.Background(), 1, 0, "stripe", "svc", "", 0)
	if err == nil {
		t.Fatal("expected error for zero amount checkout session")
	}
}

// ─── SubscriptionService — Activate with existing subscription ───────────────

func TestSubscriptionService_Activate_ExpiresExistingFirst(t *testing.T) {
	svc, subStore, planStore := newSubSvcForCoverage()
	_ = planStore.CreatePlan(context.Background(), &entity.ProductPlan{
		ProductID: "prod-api", BillingCycle: entity.BillingCycleMonthly, Status: 1,
	})
	// First activation.
	sub1, err := svc.Activate(context.Background(), 1, "prod-api", 1, "stripe", "")
	if err != nil || sub1 == nil {
		t.Fatalf("first activate: %v", err)
	}
	// Second activation should expire the first.
	sub2, err := svc.Activate(context.Background(), 1, "prod-api", 1, "stripe", "")
	if err != nil || sub2 == nil {
		t.Fatalf("second activate: %v", err)
	}
	old, _ := subStore.GetByID(context.Background(), sub1.ID)
	if old == nil || old.Status != entity.SubStatusExpired {
		t.Errorf("expected first sub to be expired, got %q", old.Status)
	}
}

func TestSubscriptionService_Expire_ThenEndGrace_FullCycle(t *testing.T) {
	svc, _, planStore := newSubSvcForCoverage()
	_ = planStore.CreatePlan(context.Background(), &entity.ProductPlan{
		ProductID: "prod-cycle", BillingCycle: entity.BillingCycleMonthly, Status: 1,
	})
	sub, err := svc.Activate(context.Background(), 1, "prod-cycle", 1, "stripe", "")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := svc.Expire(context.Background(), sub.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if err := svc.EndGrace(context.Background(), sub.ID); err != nil {
		t.Fatalf("end grace: %v", err)
	}
}

// ─── VIPService — RecalculateFromWallet with wallet and spend tiers ──────────

func TestVIPService_RecalculateFromWallet_SpendTierUpgrade(t *testing.T) {
	vipStore := newMockVIPStore(newBasicVIPConfigs())
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)

	// Give account 1 a wallet with 500 CNY lifetime topup.
	w, _ := ws.GetOrCreate(context.Background(), 1)
	w.LifetimeTopup = 500.0
	ws.mu.Lock()
	ws.wallets[1] = w
	ws.mu.Unlock()

	if err := svc.RecalculateFromWallet(context.Background(), 1); err != nil {
		t.Fatalf("RecalculateFromWallet: %v", err)
	}
	v, _ := svc.Get(context.Background(), 1)
	if v.Level != 2 {
		t.Errorf("expected level 2 (Gold), got %d", v.Level)
	}
}

// ─── overview computeDiscount ────────────────────────────────────────────────

func TestComputeDiscount_Tiers(t *testing.T) {
	cases := []struct {
		balance  float64
		wantTier string
	}{
		{0, "none"},
		{50, "none"},
		{100, "silver_holder"},
		{500, "gold_holder"},
		{2000, "diamond_holder"},
		{5000, "diamond_holder"},
	}
	for _, tc := range cases {
		rate, tier := computeDiscount(tc.balance)
		_ = rate
		if tier != tc.wantTier {
			t.Errorf("computeDiscount(%.0f): tier=%q, want %q", tc.balance, tier, tc.wantTier)
		}
	}
}

// ─── Registration — simple paths without redis ────────────────────────────────

func TestRegistrationService_CheckUsernameAvailable_Taken(t *testing.T) {
	store := newMockAccountStore()
	_ = store.Create(context.Background(), &entity.Account{Username: "taken_user", Email: "u@example.com", Status: entity.AccountStatusActive})
	// Build a minimal RegistrationService (no redis/zitadel/email needed for this path).
	svc := &RegistrationService{accounts: store}
	avail, err := svc.CheckUsernameAvailable(context.Background(), "taken_user")
	if err != nil {
		t.Fatalf("CheckUsernameAvailable: %v", err)
	}
	if avail {
		t.Error("expected username to be taken")
	}
}

func TestRegistrationService_CheckUsernameAvailable_Free(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	avail, err := svc.CheckUsernameAvailable(context.Background(), "free_user")
	if err != nil {
		t.Fatalf("CheckUsernameAvailable: %v", err)
	}
	if !avail {
		t.Error("expected username to be available")
	}
}

func TestRegistrationService_CheckEmailAvailable_InvalidFormat(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	_, err := svc.CheckEmailAvailable(context.Background(), "not-an-email")
	if err == nil {
		t.Fatal("expected error for invalid email format")
	}
}

func TestRegistrationService_CheckEmailAvailable_Taken(t *testing.T) {
	store := newMockAccountStore()
	_ = store.Create(context.Background(), &entity.Account{Email: "used@example.com", Status: entity.AccountStatusActive})
	svc := &RegistrationService{accounts: store}
	avail, err := svc.CheckEmailAvailable(context.Background(), "used@example.com")
	if err != nil {
		t.Fatalf("CheckEmailAvailable: %v", err)
	}
	if avail {
		t.Error("expected email to be taken")
	}
}

// ─── Refund — subCancel integration path ────────────────────────────────────

// mockSubCanceller implements subscriptionCanceller.
type mockSubCanceller struct {
	called bool
}

func (m *mockSubCanceller) Cancel(_ context.Context, _ int64, _ string) error {
	m.called = true
	return nil
}

func TestRefundService_Approve_SubscriptionCancelCalled(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	rs := newMockRefundStore()
	outbox := &mockEventOutbox{}
	svc := NewRefundService(rs, ws, &mockRefundPublisher2{}, outbox)
	canceller := &mockSubCanceller{}
	svc.WithSubscriptionCanceller(canceller)

	// Create a subscription-type paid order.
	o := &entity.PaymentOrder{
		AccountID: 1, OrderNo: "SUB-ORDER-1", OrderType: "subscription",
		AmountCNY: 99.0, Currency: "CNY", PaymentMethod: "stripe",
		Status: entity.OrderStatusPaid, CreatedAt: time.Now(), ProductID: "prod-api",
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)

	r, err := svc.RequestRefund(context.Background(), 1, "SUB-ORDER-1", "cancel request")
	if err != nil {
		t.Fatalf("RequestRefund: %v", err)
	}
	if err := svc.Approve(context.Background(), r.RefundNo, "admin", "approved"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !canceller.called {
		t.Fatal("expected subscription canceller to be called")
	}
}

// ─── VIPService error paths ───────────────────────────────────────────────────

func TestVIPService_RecalculateFromWallet_ListConfigsError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore:   newMockVIPStore(nil),
		listConfigsErr: errors.New("db error"),
	}
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewVIPService(vs, ws)
	if err := svc.RecalculateFromWallet(context.Background(), 1); err == nil {
		t.Fatal("expected error when ListConfigs fails")
	}
}

func TestVIPService_RecalculateFromWallet_GetOrCreateError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore:   newMockVIPStore(newBasicVIPConfigs()),
		getOrCreateErr: errors.New("db error"),
	}
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewVIPService(vs, ws)
	if err := svc.RecalculateFromWallet(context.Background(), 1); err == nil {
		t.Fatal("expected error when GetOrCreate fails")
	}
}

func TestVIPService_RecalculateFromWallet_UpdateError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore: newMockVIPStore(newBasicVIPConfigs()),
		updateErr:    errors.New("db error"),
	}
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	svc := NewVIPService(vs, ws)
	if err := svc.RecalculateFromWallet(context.Background(), 1); err == nil {
		t.Fatal("expected error when Update fails")
	}
}

func TestVIPService_GrantYearlySub_ListConfigsError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore:   newMockVIPStore(nil),
		listConfigsErr: errors.New("db error"),
	}
	svc := NewVIPService(vs, newMockWalletStore())
	if err := svc.GrantYearlySub(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error when ListConfigs fails in GrantYearlySub")
	}
}

func TestVIPService_GrantYearlySub_GetOrCreateError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore:   newMockVIPStore(newBasicVIPConfigs()),
		getOrCreateErr: errors.New("db error"),
	}
	svc := NewVIPService(vs, newMockWalletStore())
	if err := svc.GrantYearlySub(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error when GetOrCreate fails in GrantYearlySub")
	}
}

func TestVIPService_GrantYearlySub_UpdateError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore: newMockVIPStore(newBasicVIPConfigs()),
		updateErr:    errors.New("db error"),
	}
	svc := NewVIPService(vs, newMockWalletStore())
	if err := svc.GrantYearlySub(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error when Update fails in GrantYearlySub")
	}
}

func TestVIPService_AdminSet_ListConfigsError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore:   newMockVIPStore(nil),
		listConfigsErr: errors.New("db error"),
	}
	svc := NewVIPService(vs, newMockWalletStore())
	if err := svc.AdminSet(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error when ListConfigs fails in AdminSet")
	}
}

func TestVIPService_AdminSet_GetOrCreateError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore:   newMockVIPStore(newBasicVIPConfigs()),
		getOrCreateErr: errors.New("db error"),
	}
	svc := NewVIPService(vs, newMockWalletStore())
	if err := svc.AdminSet(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error when GetOrCreate fails in AdminSet")
	}
}

func TestVIPService_AdminSet_UpdateError(t *testing.T) {
	vs := &errVIPStore{
		mockVIPStore: newMockVIPStore(newBasicVIPConfigs()),
		updateErr:    errors.New("db error"),
	}
	svc := NewVIPService(vs, newMockWalletStore())
	if err := svc.AdminSet(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error when Update fails in AdminSet")
	}
}

// ─── SubscriptionService — Cancel error path ─────────────────────────────────

// errSubStore wraps mockSubStore and injects Update errors.
type errSubStore struct {
	*mockSubStore
	updateErr error
}

func (m *errSubStore) Update(ctx context.Context, s *entity.Subscription) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return m.mockSubStore.Update(ctx, s)
}

func TestSubscriptionService_Cancel_UpdateError(t *testing.T) {
	subStore := &errSubStore{
		mockSubStore: newMockSubStore(),
		updateErr:    errors.New("db error"),
	}
	planStore := newMockPlanStore()
	ents := NewEntitlementService(subStore, planStore, newMockCache())
	svc := NewSubscriptionService(subStore, planStore, ents, 3)

	_ = planStore.CreatePlan(context.Background(), &entity.ProductPlan{
		ProductID: "prod-err", BillingCycle: entity.BillingCycleMonthly, Status: 1,
	})
	// Insert an active sub directly.
	active := &entity.Subscription{
		AccountID: 1, ProductID: "prod-err", Status: entity.SubStatusActive,
	}
	_ = subStore.mockSubStore.Create(context.Background(), active)

	if err := svc.Cancel(context.Background(), 1, "prod-err"); err == nil {
		t.Fatal("expected error when update fails during Cancel")
	}
}

func TestSubscriptionService_Expire_UpdateError(t *testing.T) {
	subStore := &errSubStore{
		mockSubStore: newMockSubStore(),
	}
	planStore := newMockPlanStore()
	ents := NewEntitlementService(subStore, planStore, newMockCache())
	svc := NewSubscriptionService(subStore, planStore, ents, 3)

	// Insert active sub directly.
	active := &entity.Subscription{
		AccountID: 1, ProductID: "prod", Status: entity.SubStatusActive,
	}
	_ = subStore.mockSubStore.Create(context.Background(), active)

	// Now inject Update error.
	subStore.updateErr = errors.New("db error")
	if err := svc.Expire(context.Background(), 1); err == nil {
		t.Fatal("expected error when update fails during Expire")
	}
}

// ─── Registration — simple non-redis paths ────────────────────────────────────

func TestRegistrationService_ForgotPassword_EmptyIdentifierErrors(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	_, err := svc.ForgotPassword(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty identifier")
	}
}

func TestRegistrationService_ForgotPassword_UnknownAccountReturnsNoHint(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	result, err := svc.ForgotPassword(context.Background(), "unknown@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must not reveal whether account exists.
	if result == nil || result.Message == "" {
		t.Fatal("expected a result message")
	}
}

func TestRegistrationService_ForgotPassword_AccountNoZitadelSubReturnsNoHint(t *testing.T) {
	store := newMockAccountStore()
	_ = store.Create(context.Background(), &entity.Account{
		Email:  "nozitadel@example.com",
		Status: entity.AccountStatusActive,
		// ZitadelSub is intentionally empty
	})
	svc := &RegistrationService{accounts: store}
	result, err := svc.ForgotPassword(context.Background(), "nozitadel@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result for no-sub account")
	}
}

func TestRegistrationService_ResetPassword_ShortPasswordErrors(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	err := svc.ResetPassword(context.Background(), "user@example.com", "123456", "short")
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestRegistrationService_ResetPassword_NoAccountErrors(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	err := svc.ResetPassword(context.Background(), "nonexistent@example.com", "123456", "longenoughpassword")
	if err == nil {
		t.Fatal("expected error for unknown account")
	}
}

func TestRegistrationService_SendPhoneVerificationCode_InvalidPhoneErrors(t *testing.T) {
	svc := &RegistrationService{accounts: newMockAccountStore()}
	if err := svc.SendPhoneVerificationCode(context.Background(), 1, "not-a-phone"); err == nil {
		t.Fatal("expected error for invalid phone")
	}
}

// ─── Invoice GetByNo — wrong account ─────────────────────────────────────────

func TestInvoiceService_GetByNo_WrongAccountErrors(t *testing.T) {
	ws := newMockWalletStore()
	ws.GetOrCreate(context.Background(), 1)
	orderNo := "ORDER-INV-IDOR"
	o := &entity.PaymentOrder{
		AccountID: 1, OrderNo: orderNo, OrderType: "topup",
		AmountCNY: 10.0, Status: entity.OrderStatusPaid, CreatedAt: time.Now(),
	}
	_ = ws.CreatePaymentOrder(context.Background(), o)
	svc := NewInvoiceService(newMockInvoiceStore(), ws)
	inv, _ := svc.Generate(context.Background(), 1, orderNo)
	if inv == nil {
		t.Fatal("generate failed")
	}
	// Account 2 should not see account 1's invoice.
	_, err := svc.GetByNo(context.Background(), 2, inv.InvoiceNo)
	if err == nil {
		t.Fatal("expected IDOR error for wrong account")
	}
}

// ─── WalletService — GetBillingSummary error paths ───────────────────────────

// errWalletStore injects errors into specific wallet store methods.
type errWalletStore struct {
	*mockWalletStore
	countActivePreAuthsErr error
	countPendingOrdersErr  error
}

func (m *errWalletStore) CountActivePreAuths(ctx context.Context, accountID int64) (int64, error) {
	if m.countActivePreAuthsErr != nil {
		return 0, m.countActivePreAuthsErr
	}
	return m.mockWalletStore.CountActivePreAuths(ctx, accountID)
}

func (m *errWalletStore) CountPendingOrders(ctx context.Context, accountID int64) (int64, error) {
	if m.countPendingOrdersErr != nil {
		return 0, m.countPendingOrdersErr
	}
	return m.mockWalletStore.CountPendingOrders(ctx, accountID)
}

func TestWalletService_GetBillingSummary_CountPreAuthsError(t *testing.T) {
	ws := &errWalletStore{
		mockWalletStore:        newMockWalletStore(),
		countActivePreAuthsErr: errors.New("db error"),
	}
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws.mockWalletStore))
	_, err := svc.GetBillingSummary(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error when CountActivePreAuths fails")
	}
}

func TestWalletService_GetBillingSummary_CountPendingOrdersError(t *testing.T) {
	ws := &errWalletStore{
		mockWalletStore:       newMockWalletStore(),
		countPendingOrdersErr: errors.New("db error"),
	}
	ws.GetOrCreate(context.Background(), 1)
	svc := NewWalletService(ws, NewVIPService(newMockVIPStore(nil), ws.mockWalletStore))
	_, err := svc.GetBillingSummary(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error when CountPendingOrders fails")
	}
}

// ─── verifyStalePendingOrders — unknown provider method (no-op) ──────────────

func TestReconWorker_verifyStalePendingOrders_UnknownProvider(t *testing.T) {
	rs := &mockReconWalletStore{
		mockWalletStore: newMockWalletStore(),
		staleOrders: []entity.PaymentOrder{
			{OrderNo: "STALE-UNKNOWN", PaymentMethod: "unknown_method", AccountID: 1, AmountCNY: 10},
		},
	}
	vipSvc := NewVIPService(newMockVIPStore(nil), rs.mockWalletStore)
	walletSvc := NewWalletService(rs, vipSvc)
	reg := payment.NewRegistry()
	w := NewReconciliationWorker(walletSvc, reg)
	// Unknown payment method → providerName="" → continue (no panic).
	w.verifyStalePendingOrders(context.Background())
}
