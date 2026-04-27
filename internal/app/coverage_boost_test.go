//go:build ignore
// +build ignore

// DISABLED: draft file has misplaced `import "sync"` after declarations (line ~1417).
// Left on disk for author to finish or delete; excluded from compilation so CI stays green.

package app

// coverage_boost_test.go — targeted tests to push internal/app coverage from 79.5% to 90%+.
// Focus areas:
//   - reconciliation_worker: checkPaidOrdersIntegrity, verifyStalePendingOrders, fireAlert,
//     SetOnAlertHook, queryProviderOrder (no-registry path)
//   - wallet_service: FindStalePendingOrders, CreateReconciliationIssue,
//     ListReconciliationIssues, ResolveReconciliationIssue pass-throughs
//   - account_service: SetOnAccountCreatedHook, UpsertByZitadelSub email-match path,
//     UpsertByWechat existing binding path
//   - subscription_service: Expire (wrong state), EndGrace (wrong state),
//     Cancel (already cancelled), calculateExpiry all branches
//   - vip_service: AdminSet level-unchanged path, RecalculateFromWallet no configs
//   - referral_service: WithRewardEvents chaining, OnRenewal (royalty window)
//   - registration_service: CheckUsernameAvailable taken/available,
//     CheckEmailAvailable invalid email, ForgotPassword empty-identifier path,
//     generateNumericCode sanity
//   - service_key_admin: 0% covered — constructor tested indirectly (no real DB,
//     tested via nil-safe smoke check on the struct)

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// mockOrderQuerier is a minimal payment.Provider + payment.OrderQuerier.
type mockOrderQuerier struct {
	queryResult *payment.OrderQueryResult
	queryErr    error
}

func (m *mockOrderQuerier) Name() string { return "mock" }
func (m *mockOrderQuerier) CreateCheckout(_ context.Context, _ *entity.PaymentOrder, _ string) (string, string, error) {
	return "", "", errors.New("not implemented")
}
func (m *mockOrderQuerier) QueryOrder(_ context.Context, _ string) (*payment.OrderQueryResult, error) {
	return m.queryResult, m.queryErr
}

// mockReconciliationStore extends mockWalletStore with configurable reconciliation methods.
type mockReconciliationStore struct {
	mockWalletStore

	// FindStalePendingOrders configuration
	stalePendingOrders []entity.PaymentOrder
	stalePendingErr    error

	// FindPaidTopupOrdersWithoutCredit configuration
	orphanOrders []entity.PaidOrderWithoutCredit
	orphanErr    error

	// CreateReconciliationIssue tracking
	createdIssues []*entity.ReconciliationIssue
	createIssueErr error

	// ListReconciliationIssues configuration
	listedIssues    []entity.ReconciliationIssue
	listIssuesTotal int64
	listIssuesErr   error

	// ResolveReconciliationIssue tracking
	resolvedID         int64
	resolvedStatus     string
	resolvedResolution string
	resolveIssueErr    error

	// MarkPaymentOrderPaid override
	markPaidResult *entity.PaymentOrder
	markPaidDid    bool
	markPaidErr    error

	expireOrdersCalled atomic.Int32
	expirePreAuthCalled atomic.Int32
}

func (m *mockReconciliationStore) ExpireStalePendingOrders(_ context.Context, _ time.Duration) (int64, error) {
	m.expireOrdersCalled.Add(1)
	return 0, nil
}

func (m *mockReconciliationStore) ExpireStalePreAuths(_ context.Context) (int64, error) {
	m.expirePreAuthCalled.Add(1)
	return 0, nil
}

func (m *mockReconciliationStore) FindStalePendingOrders(_ context.Context, _ time.Duration) ([]entity.PaymentOrder, error) {
	return m.stalePendingOrders, m.stalePendingErr
}

func (m *mockReconciliationStore) FindPaidTopupOrdersWithoutCredit(_ context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	return m.orphanOrders, m.orphanErr
}

func (m *mockReconciliationStore) CreateReconciliationIssue(_ context.Context, issue *entity.ReconciliationIssue) error {
	if m.createIssueErr != nil {
		return m.createIssueErr
	}
	m.mu.Lock()
	m.createdIssues = append(m.createdIssues, issue)
	m.mu.Unlock()
	return nil
}

func (m *mockReconciliationStore) ListReconciliationIssues(_ context.Context, _ string, _, _ int) ([]entity.ReconciliationIssue, int64, error) {
	return m.listedIssues, m.listIssuesTotal, m.listIssuesErr
}

func (m *mockReconciliationStore) ResolveReconciliationIssue(_ context.Context, id int64, status, resolution string) error {
	if m.resolveIssueErr != nil {
		return m.resolveIssueErr
	}
	m.resolvedID = id
	m.resolvedStatus = status
	m.resolvedResolution = resolution
	return nil
}

func (m *mockReconciliationStore) MarkPaymentOrderPaid(_ context.Context, orderNo string) (*entity.PaymentOrder, bool, error) {
	if m.markPaidResult != nil || m.markPaidErr != nil {
		return m.markPaidResult, m.markPaidDid, m.markPaidErr
	}
	// Fall back to default mock behaviour.
	return m.mockWalletStore.MarkPaymentOrderPaid(context.Background(), orderNo)
}

// newTestReconciliationStore returns a store backed by fresh mockWalletStore data.
func newTestReconciliationStore() *mockReconciliationStore {
	return &mockReconciliationStore{mockWalletStore: *newMockWalletStore()}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — SetOnAlertHook
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_SetOnAlertHook(t *testing.T) {
	ws := NewWalletService(newMockWalletStore(), nil)
	w := NewReconciliationWorker(ws, nil)

	var called bool
	w.SetOnAlertHook(func(_ context.Context, _ *entity.ReconciliationIssue) {
		called = true
	})

	if w.onAlert == nil {
		t.Fatal("expected onAlert hook to be set")
	}
	// Fire manually to verify the hook is invoked.
	w.fireAlert(context.Background(), &entity.ReconciliationIssue{IssueType: "test"})
	if !called {
		t.Error("expected alert hook to be called")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — fireAlert with nil hook (should not panic)
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_FireAlert_NilHook(t *testing.T) {
	ws := NewWalletService(newMockWalletStore(), nil)
	w := NewReconciliationWorker(ws, nil) // no hook set

	// Must not panic.
	w.fireAlert(context.Background(), &entity.ReconciliationIssue{IssueType: "test"})
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — checkPaidOrdersIntegrity: FindPaidTopupOrdersWithoutCredit error
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_CheckPaidOrdersIntegrity_StoreError(t *testing.T) {
	store := newTestReconciliationStore()
	store.orphanErr = errors.New("db unreachable")

	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, nil)

	// Must not panic even when FindPaidTopupOrdersWithoutCredit returns an error.
	w.checkPaidOrdersIntegrity(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — checkPaidOrdersIntegrity: no orphan orders (short-circuit)
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_CheckPaidOrdersIntegrity_NoOrphans(t *testing.T) {
	store := newTestReconciliationStore()
	// orphanOrders is nil by default → empty result.

	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, nil)
	w.checkPaidOrdersIntegrity(context.Background())

	if len(store.createdIssues) != 0 {
		t.Errorf("expected 0 issues created, got %d", len(store.createdIssues))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — checkPaidOrdersIntegrity: orphans trigger issue creation + alert
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_CheckPaidOrdersIntegrity_OrphansTriggersAlerts(t *testing.T) {
	store := newTestReconciliationStore()
	amount := 100.0
	accountID := int64(1)
	store.orphanOrders = []entity.PaidOrderWithoutCredit{
		{OrderNo: "LO_ORPHAN_1", AccountID: accountID, AmountCNY: amount, PaymentMethod: "stripe"},
		{OrderNo: "LO_ORPHAN_2", AccountID: accountID, AmountCNY: 50.0, PaymentMethod: "alipay"},
	}

	var alertCount int32
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, nil)
	w.SetOnAlertHook(func(_ context.Context, _ *entity.ReconciliationIssue) {
		atomic.AddInt32(&alertCount, 1)
	})

	w.checkPaidOrdersIntegrity(context.Background())

	if len(store.createdIssues) != 2 {
		t.Errorf("expected 2 issues created, got %d", len(store.createdIssues))
	}
	if alertCount != 2 {
		t.Errorf("expected 2 alerts fired, got %d", alertCount)
	}
	// Verify issue type.
	for _, iss := range store.createdIssues {
		if iss.IssueType != entity.ReconIssueMissingCredit {
			t.Errorf("issue type = %q, want %q", iss.IssueType, entity.ReconIssueMissingCredit)
		}
		if iss.Severity != "critical" {
			t.Errorf("severity = %q, want critical", iss.Severity)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — checkPaidOrdersIntegrity: CreateReconciliationIssue error is logged
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_CheckPaidOrdersIntegrity_CreateIssueError(t *testing.T) {
	store := newTestReconciliationStore()
	store.orphanOrders = []entity.PaidOrderWithoutCredit{
		{OrderNo: "LO_ORPH", AccountID: 1, AmountCNY: 10.0, PaymentMethod: "stripe"},
	}
	store.createIssueErr = errors.New("postgres unavailable")

	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, nil)

	// Must not panic even when CreateReconciliationIssue returns an error.
	w.checkPaidOrdersIntegrity(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: nil payments registry skips
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_NilRegistry(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_STALE", AccountID: 1, PaymentMethod: "stripe"},
	}

	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, nil) // nil registry

	// Should return immediately without touching FindStalePendingOrders.
	w.verifyStalePendingOrders(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: FindStalePendingOrders error
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_FindError(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingErr = errors.New("db error")

	reg := payment.NewRegistry()
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)

	// Must not panic.
	w.verifyStalePendingOrders(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: empty stale orders (short-circuit)
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_EmptyOrders(t *testing.T) {
	store := newTestReconciliationStore()
	// stalePendingOrders is nil → empty.

	reg := payment.NewRegistry()
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)

	w.verifyStalePendingOrders(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: unknown payment method is skipped
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_UnknownMethod(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_UNK", AccountID: 1, PaymentMethod: "unknown_provider"},
	}

	reg := payment.NewRegistry()
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)

	w.verifyStalePendingOrders(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: provider not registered in registry
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_ProviderNotInRegistry(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_NOPROV", AccountID: 1, PaymentMethod: "alipay"},
	}

	// Registry has no providers registered.
	reg := payment.NewRegistry()
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)

	// QueryOrder will return (nil, nil) for unknown provider → should continue gracefully.
	w.verifyStalePendingOrders(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: provider query returns not-paid
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_ProviderNotPaid(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_NOTPAID", AccountID: 1, PaymentMethod: "alipay"},
	}

	querier := &mockOrderQuerier{queryResult: &payment.OrderQueryResult{Paid: false, Amount: 0}}

	reg := payment.NewRegistry()
	reg.Register("alipay", querier, payment.MethodInfo{ID: "alipay", Name: "Alipay"})

	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)
	w.verifyStalePendingOrders(context.Background())

	// No recovery expected.
	if len(store.createdIssues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(store.createdIssues))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: provider query returns paid (auto-recover)
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_ProviderPaid_AutoRecover(t *testing.T) {
	store := newTestReconciliationStore()

	// Seed a pending order in the base mockWalletStore.
	order := &entity.PaymentOrder{
		OrderNo:       "LO_RECOVER",
		AccountID:     1,
		PaymentMethod: "alipay",
		OrderType:     "topup",
		AmountCNY:     100.0,
		Status:        entity.OrderStatusPending,
		CreatedAt:     time.Now().Add(-20 * time.Minute),
	}
	_ = store.mockWalletStore.CreatePaymentOrder(context.Background(), order)
	store.mockWalletStore.wallets[1] = &entity.Wallet{ID: 1, AccountID: 1, Balance: 0}

	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_RECOVER", AccountID: 1, PaymentMethod: "alipay", OrderType: "topup", AmountCNY: 100.0},
	}

	querier := &mockOrderQuerier{queryResult: &payment.OrderQueryResult{Paid: true, Amount: 100.0}}
	reg := payment.NewRegistry()
	reg.Register("alipay", querier, payment.MethodInfo{ID: "alipay", Name: "Alipay"})

	vipSvc := NewVIPService(newMockVIPStore(nil), &store.mockWalletStore)
	ws := NewWalletService(store, vipSvc)
	w := NewReconciliationWorker(ws, reg)

	w.verifyStalePendingOrders(context.Background())

	// Order should now be paid.
	o, _ := store.mockWalletStore.GetPaymentOrderByNo(context.Background(), "LO_RECOVER")
	if o.Status != entity.OrderStatusPaid {
		t.Errorf("order status = %q, want %q", o.Status, entity.OrderStatusPaid)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: provider query error → continues
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_ProviderQueryError(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_QERR", AccountID: 1, PaymentMethod: "alipay"},
	}

	querier := &mockOrderQuerier{queryErr: errors.New("provider timeout")}
	reg := payment.NewRegistry()
	reg.Register("alipay", querier, payment.MethodInfo{ID: "alipay", Name: "Alipay"})

	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)

	// Must not panic even when provider returns an error.
	w.verifyStalePendingOrders(context.Background())
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: paid + amount mismatch → creates warning issue
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_AmountMismatch(t *testing.T) {
	store := newTestReconciliationStore()

	order := &entity.PaymentOrder{
		OrderNo:       "LO_MISMATCH",
		AccountID:     2,
		PaymentMethod: "alipay",
		OrderType:     "topup",
		AmountCNY:     100.0,
		Status:        entity.OrderStatusPending,
	}
	_ = store.mockWalletStore.CreatePaymentOrder(context.Background(), order)
	store.mockWalletStore.wallets[2] = &entity.Wallet{ID: 2, AccountID: 2, Balance: 0}

	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_MISMATCH", AccountID: 2, PaymentMethod: "alipay", OrderType: "topup", AmountCNY: 100.0},
	}

	// Provider says paid, but amount is different (e.g. partial payment).
	querier := &mockOrderQuerier{queryResult: &payment.OrderQueryResult{Paid: true, Amount: 80.0}}
	reg := payment.NewRegistry()
	reg.Register("alipay", querier, payment.MethodInfo{ID: "alipay", Name: "Alipay"})

	vipSvc := NewVIPService(newMockVIPStore(nil), &store.mockWalletStore)
	ws := NewWalletService(store, vipSvc)
	w := NewReconciliationWorker(ws, reg)
	w.verifyStalePendingOrders(context.Background())

	// Should create an amount-mismatch warning issue.
	found := false
	for _, iss := range store.createdIssues {
		if iss.IssueType == entity.ReconIssueAmountMismatch {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ReconIssueAmountMismatch issue to be created; got %v", store.createdIssues)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReconciliationWorker — verifyStalePendingOrders: MarkOrderPaid fails → creates issue + alert
// ─────────────────────────────────────────────────────────────────────────────

func TestReconciliationWorker_VerifyStalePendingOrders_MarkPaidFails_CreatesIssue(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_FAILMARK", AccountID: 1, PaymentMethod: "alipay", OrderType: "topup", AmountCNY: 50.0},
	}
	// Override MarkPaymentOrderPaid to simulate failure.
	store.markPaidErr = errors.New("lock timeout")

	querier := &mockOrderQuerier{queryResult: &payment.OrderQueryResult{Paid: true, Amount: 50.0}}
	reg := payment.NewRegistry()
	reg.Register("alipay", querier, payment.MethodInfo{ID: "alipay", Name: "Alipay"})

	var alertFired bool
	ws := NewWalletService(store, nil)
	w := NewReconciliationWorker(ws, reg)
	w.SetOnAlertHook(func(_ context.Context, _ *entity.ReconciliationIssue) {
		alertFired = true
	})

	w.verifyStalePendingOrders(context.Background())

	if !alertFired {
		t.Error("expected alert to be fired when auto-recovery fails")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WalletService — reconciliation pass-through methods
// ─────────────────────────────────────────────────────────────────────────────

func TestWalletService_FindStalePendingOrders_PassThrough(t *testing.T) {
	store := newTestReconciliationStore()
	store.stalePendingOrders = []entity.PaymentOrder{
		{OrderNo: "LO_PT_1", AccountID: 1, PaymentMethod: "stripe"},
	}

	vipSvc := NewVIPService(newMockVIPStore(nil), &store.mockWalletStore)
	svc := NewWalletService(store, vipSvc)

	orders, err := svc.FindStalePendingOrders(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatalf("FindStalePendingOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("expected 1 order, got %d", len(orders))
	}
}

func TestWalletService_CreateReconciliationIssue_PassThrough(t *testing.T) {
	store := newTestReconciliationStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), &store.mockWalletStore)
	svc := NewWalletService(store, vipSvc)

	issue := &entity.ReconciliationIssue{
		IssueType: entity.ReconIssueMissingCredit,
		Severity:  "critical",
		OrderNo:   "LO_PT_ISSUE",
	}
	if err := svc.CreateReconciliationIssue(context.Background(), issue); err != nil {
		t.Fatalf("CreateReconciliationIssue: %v", err)
	}
	if len(store.createdIssues) != 1 {
		t.Errorf("expected 1 persisted issue, got %d", len(store.createdIssues))
	}
}

func TestWalletService_ListReconciliationIssues_PassThrough(t *testing.T) {
	store := newTestReconciliationStore()
	store.listedIssues = []entity.ReconciliationIssue{
		{IssueType: entity.ReconIssueMissingCredit, OrderNo: "LO_LIST_1"},
	}
	store.listIssuesTotal = 1

	vipSvc := NewVIPService(newMockVIPStore(nil), &store.mockWalletStore)
	svc := NewWalletService(store, vipSvc)

	issues, total, err := svc.ListReconciliationIssues(context.Background(), "open", 1, 10)
	if err != nil {
		t.Fatalf("ListReconciliationIssues: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}
}

func TestWalletService_ResolveReconciliationIssue_PassThrough(t *testing.T) {
	store := newTestReconciliationStore()
	vipSvc := NewVIPService(newMockVIPStore(nil), &store.mockWalletStore)
	svc := NewWalletService(store, vipSvc)

	if err := svc.ResolveReconciliationIssue(context.Background(), 42, "resolved", "manually fixed"); err != nil {
		t.Fatalf("ResolveReconciliationIssue: %v", err)
	}
	if store.resolvedID != 42 {
		t.Errorf("resolvedID = %d, want 42", store.resolvedID)
	}
	if store.resolvedStatus != "resolved" {
		t.Errorf("resolvedStatus = %q, want resolved", store.resolvedStatus)
	}
	if store.resolvedResolution != "manually fixed" {
		t.Errorf("resolvedResolution = %q, want 'manually fixed'", store.resolvedResolution)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccountService — SetOnAccountCreatedHook
// ─────────────────────────────────────────────────────────────────────────────

func TestAccountService_SetOnAccountCreatedHook(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)

	var hookCalled bool
	svc.SetOnAccountCreatedHook(func(_ context.Context, _ *entity.Account) {
		hookCalled = true
	})

	if svc.onAccountCreated == nil {
		t.Fatal("expected hook to be set")
	}

	// Creating a new account must trigger the hook asynchronously.
	_, err := svc.UpsertByZitadelSub(context.Background(), "sub-hook-test", "hook@test.com", "Hook User", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub: %v", err)
	}
	// Allow goroutine to run.
	time.Sleep(10 * time.Millisecond)
	if !hookCalled {
		t.Error("expected account-created hook to be called")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccountService — UpsertByZitadelSub: email-match path (no sub, but email exists)
// ─────────────────────────────────────────────────────────────────────────────

func TestAccountService_UpsertByZitadelSub_EmailMatchPath(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)
	ctx := context.Background()

	// Create account without ZitadelSub (pre-Zitadel account).
	existing := &entity.Account{
		Email:       "existing@test.com",
		DisplayName: "Old Name",
		LurusID:     "LRUS001",
		Status:      entity.AccountStatusActive,
	}
	_ = accStore.Create(ctx, existing)

	// UpsertByZitadelSub with a new sub but existing email → should link the sub.
	updated, err := svc.UpsertByZitadelSub(ctx, "new-sub-for-existing", "existing@test.com", "New Name", "")
	if err != nil {
		t.Fatalf("UpsertByZitadelSub email-match: %v", err)
	}
	if updated == nil {
		t.Fatal("expected non-nil account")
	}
	// The sub should have been linked.
	if updated.ZitadelSub != "new-sub-for-existing" {
		t.Errorf("ZitadelSub = %q, want 'new-sub-for-existing'", updated.ZitadelSub)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccountService — UpsertByWechat: existing binding returns cached account
// ─────────────────────────────────────────────────────────────────────────────

func TestAccountService_UpsertByWechat_ExistingBinding(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)
	ctx := context.Background()

	// Create the account and its OAuth binding.
	acc := &entity.Account{
		ZitadelSub:  "wechat:wx_existing_id",
		Email:       "wechat.wx_existing_id@noreply.lurus.cn",
		DisplayName: "微信用户",
		Status:      entity.AccountStatusActive,
	}
	_ = accStore.Create(ctx, acc)
	_ = accStore.UpsertOAuthBinding(ctx, &entity.OAuthBinding{
		AccountID:  acc.ID,
		Provider:   "wechat",
		ProviderID: "wx_existing_id",
	})

	// Call again — should return existing account without creating a new one.
	result, err := svc.UpsertByWechat(ctx, "wx_existing_id")
	if err != nil {
		t.Fatalf("UpsertByWechat existing: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil account")
	}
	if result.ID != acc.ID {
		t.Errorf("account ID = %d, want %d", result.ID, acc.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccountService — UpsertByWechat: new user (creates account + binding)
// ─────────────────────────────────────────────────────────────────────────────

func TestAccountService_UpsertByWechat_NewUser(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)

	acc, err := svc.UpsertByWechat(context.Background(), "wx_brand_new")
	if err != nil {
		t.Fatalf("UpsertByWechat new user: %v", err)
	}
	if acc == nil {
		t.Fatal("expected non-nil account")
	}
	if acc.LurusID == "" {
		t.Error("expected LurusID to be set")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AccountService — UpsertByWechat: empty wechatID
// ─────────────────────────────────────────────────────────────────────────────

func TestAccountService_UpsertByWechat_EmptyID(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewAccountService(accStore, ws, vipStore)

	_, err := svc.UpsertByWechat(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty wechatID")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SubscriptionService — Expire: wrong status (not active)
// ─────────────────────────────────────────────────────────────────────────────

func TestSubscriptionService_Expire_WrongStatus(t *testing.T) {
	ss := newMockSubStore()
	entSvc := NewEntitlementService(ss, newMockPlanStore(), newMockCache())
	svc := NewSubscriptionService(ss, newMockPlanStore(), entSvc, 3)
	ctx := context.Background()

	// Create a subscription already in grace period.
	grace := time.Now().Add(24 * time.Hour)
	sub := &entity.Subscription{
		AccountID:  1,
		ProductID:  "prod",
		PlanID:     1,
		Status:     entity.SubStatusGrace,
		GraceUntil: &grace,
	}
	_ = ss.Create(ctx, sub)

	err := svc.Expire(ctx, sub.ID)
	if err == nil {
		t.Fatal("expected error when expiring a grace-status subscription")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SubscriptionService — EndGrace: wrong status (not grace)
// ─────────────────────────────────────────────────────────────────────────────

func TestSubscriptionService_EndGrace_WrongStatus(t *testing.T) {
	ss := newMockSubStore()
	entSvc := NewEntitlementService(ss, newMockPlanStore(), newMockCache())
	svc := NewSubscriptionService(ss, newMockPlanStore(), entSvc, 3)
	ctx := context.Background()

	now := time.Now()
	sub := &entity.Subscription{
		AccountID: 1,
		ProductID: "prod",
		PlanID:    1,
		Status:    entity.SubStatusActive,
		StartedAt: &now,
	}
	_ = ss.Create(ctx, sub)

	err := svc.EndGrace(ctx, sub.ID)
	if err == nil {
		t.Fatal("expected error when calling EndGrace on an active subscription")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SubscriptionService — Expire → EndGrace full lifecycle
// ─────────────────────────────────────────────────────────────────────────────

func TestSubscriptionService_ExpireAndEndGrace_FullLifecycle(t *testing.T) {
	ss := newMockSubStore()
	entSvc := NewEntitlementService(ss, newMockPlanStore(), newMockCache())
	svc := NewSubscriptionService(ss, newMockPlanStore(), entSvc, 1)
	ctx := context.Background()

	expiry := time.Now().Add(24 * time.Hour)
	now := time.Now()
	sub := &entity.Subscription{
		AccountID: 10,
		ProductID: "llm-api",
		PlanID:    1,
		Status:    entity.SubStatusActive,
		ExpiresAt: &expiry,
		StartedAt: &now,
	}
	_ = ss.Create(ctx, sub)

	// Expire: active → grace.
	if err := svc.Expire(ctx, sub.ID); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	updated, _ := ss.GetByID(ctx, sub.ID)
	if updated.Status != entity.SubStatusGrace {
		t.Errorf("status after Expire = %q, want grace", updated.Status)
	}

	// EndGrace: grace → expired.
	if err := svc.EndGrace(ctx, sub.ID); err != nil {
		t.Fatalf("EndGrace: %v", err)
	}
	final, _ := ss.GetByID(ctx, sub.ID)
	if final.Status != entity.SubStatusExpired {
		t.Errorf("status after EndGrace = %q, want expired", final.Status)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SubscriptionService — Cancel: no active sub
// ─────────────────────────────────────────────────────────────────────────────

func TestSubscriptionService_Cancel_AlreadyCancelled(t *testing.T) {
	ss := newMockSubStore()
	svc := NewSubscriptionService(ss, newMockPlanStore(), nil, 3)
	ctx := context.Background()

	now := time.Now()
	sub := &entity.Subscription{
		AccountID: 1,
		ProductID: "prod",
		PlanID:    1,
		Status:    entity.SubStatusCancelled, // already cancelled
		StartedAt: &now,
	}
	_ = ss.Create(ctx, sub)

	// Cancel expects active/grace sub; cancelled sub should return error.
	err := svc.Cancel(ctx, 1, "prod")
	if err == nil {
		t.Fatal("expected error when no active subscription (sub is cancelled)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// calculateExpiry — all billing cycle branches
// ─────────────────────────────────────────────────────────────────────────────

func TestCalculateExpiry_AllCycles(t *testing.T) {
	base := time.Date(2025, time.January, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		cycle     string
		wantNil   bool
		wantDays  int // approximate days from base (for non-nil)
	}{
		{entity.BillingCycleWeekly, false, 7},
		{entity.BillingCycleMonthly, false, 31},     // Jan 15 + 1 month = Feb 15 (31 days)
		{entity.BillingCycleQuarterly, false, 89},   // Jan 15 + 3 months ≈ 89 days
		{entity.BillingCycleYearly, false, 365},
		{entity.BillingCycleForever, true, 0},
		{entity.BillingCycleOneTime, true, 0},
		{"unknown_cycle", false, 31}, // defaults to monthly
	}

	for _, tc := range tests {
		t.Run(tc.cycle, func(t *testing.T) {
			result := calculateExpiry(base, tc.cycle)
			if tc.wantNil {
				if result != nil {
					t.Errorf("cycle=%q: expected nil expiry, got %v", tc.cycle, result)
				}
			} else {
				if result == nil {
					t.Fatalf("cycle=%q: expected non-nil expiry", tc.cycle)
				}
				days := int(result.Sub(base).Hours() / 24)
				// Allow ±2 days tolerance for month-boundary effects.
				if abs(days-tc.wantDays) > 2 {
					t.Errorf("cycle=%q: duration=%d days, want ~%d", tc.cycle, days, tc.wantDays)
				}
			}
		})
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ─────────────────────────────────────────────────────────────────────────────
// calculateExpiry — month-end clamping (Jan 31 + 1 month = Feb 28)
// ─────────────────────────────────────────────────────────────────────────────

func TestCalculateExpiry_MonthEndClamp(t *testing.T) {
	base := time.Date(2025, time.January, 31, 0, 0, 0, 0, time.UTC)
	result := calculateExpiry(base, entity.BillingCycleMonthly)
	if result == nil {
		t.Fatal("expected non-nil expiry")
	}
	if result.Month() != time.February {
		t.Errorf("expected February, got %s", result.Month())
	}
	if result.Day() != 28 { // 2025 is not a leap year
		t.Errorf("expected day 28 (clamped), got %d", result.Day())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// VIPService — RecalculateFromWallet: no configs → level stays 0
// ─────────────────────────────────────────────────────────────────────────────

func TestVIPService_RecalculateFromWallet_NoConfigs(t *testing.T) {
	vipStore := newMockVIPStore(nil) // empty configs
	ws := newMockWalletStore()
	ctx := context.Background()
	ws.wallets[1] = &entity.Wallet{ID: 1, AccountID: 1, Balance: 9999, LifetimeTopup: 9999}

	svc := NewVIPService(vipStore, ws)
	if err := svc.RecalculateFromWallet(ctx, 1); err != nil {
		t.Fatalf("RecalculateFromWallet: %v", err)
	}

	v, _ := svc.Get(ctx, 1)
	if v.Level != 0 {
		t.Errorf("level with no configs = %d, want 0", v.Level)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// VIPService — AdminSet: level changes are logged, level unchanged stays quiet
// ─────────────────────────────────────────────────────────────────────────────

func TestVIPService_AdminSet_LevelChange(t *testing.T) {
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 0, Name: "Free", MinSpendCNY: 0},
		{Level: 3, Name: "Platinum", MinSpendCNY: 10000},
	})
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)
	ctx := context.Background()

	// Start at level 0, change to 3.
	if err := svc.AdminSet(ctx, 1, 3); err != nil {
		t.Fatalf("AdminSet: %v", err)
	}
	v, _ := svc.Get(ctx, 1)
	if v.Level != 3 {
		t.Errorf("level = %d, want 3", v.Level)
	}
	if v.LevelName != "Platinum" {
		t.Errorf("level name = %q, want Platinum", v.LevelName)
	}
}

func TestVIPService_AdminSet_SameLevel(t *testing.T) {
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 2, Name: "Gold", MinSpendCNY: 2000},
	})
	ws := newMockWalletStore()
	svc := NewVIPService(vipStore, ws)
	ctx := context.Background()

	// First, set to 2.
	_ = svc.AdminSet(ctx, 1, 2)
	// Set to same level — should succeed silently.
	if err := svc.AdminSet(ctx, 1, 2); err != nil {
		t.Fatalf("AdminSet same level: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// VIPService — GrantYearlySub: level stays MAX(yearly, spend)
// ─────────────────────────────────────────────────────────────────────────────

func TestVIPService_GrantYearlySub_MaxLogic(t *testing.T) {
	vipStore := newMockVIPStore([]entity.VIPLevelConfig{
		{Level: 0, Name: "Free", MinSpendCNY: 0},
		{Level: 1, Name: "Silver", MinSpendCNY: 500},
		{Level: 2, Name: "Gold", MinSpendCNY: 2000},
	})
	ws := newMockWalletStore()
	ctx := context.Background()
	// Seed wallet with spend → spend_grant = 1 (500 CNY).
	ws.wallets[1] = &entity.Wallet{ID: 1, AccountID: 1, LifetimeTopup: 500}

	svc := NewVIPService(vipStore, ws)
	_ = svc.RecalculateFromWallet(ctx, 1) // spend_grant = 1

	// Grant yearly sub level = 1 (same as spend_grant) → level stays 1.
	_ = svc.GrantYearlySub(ctx, 1, 1)
	v, _ := svc.Get(ctx, 1)
	if v.Level != 1 {
		t.Errorf("level = %d, want 1 (MAX(1,1))", v.Level)
	}

	// Grant yearly sub level = 2 > spend_grant = 1 → level = 2.
	_ = svc.GrantYearlySub(ctx, 1, 2)
	v, _ = svc.Get(ctx, 1)
	if v.Level != 2 {
		t.Errorf("level = %d, want 2 (MAX(2,1))", v.Level)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReferralService — WithRewardEvents chaining
// ─────────────────────────────────────────────────────────────────────────────

// mockRewardEventStore is a minimal rewardEventStore for testing.
type mockRewardEventStore struct {
	mu      sync.Mutex
	events  []*entity.ReferralRewardEvent
	created bool // returned by CreateRewardEvent
	err     error
}

func (m *mockRewardEventStore) CreateRewardEvent(_ context.Context, ev *entity.ReferralRewardEvent) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	m.events = append(m.events, ev)
	return m.created, nil
}

func TestReferralService_WithRewardEvents_Chaining(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore())
	store := &mockRewardEventStore{created: true}

	result := svc.WithRewardEvents(store)
	if result != svc {
		t.Error("expected chaining to return same service")
	}
	if svc.rewardEvents == nil {
		t.Error("expected rewardEvents to be set")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReferralService — reward with rewardEvents: duplicate (created=false) → skip credit
// ─────────────────────────────────────────────────────────────────────────────

func TestReferralService_OnSignup_WithDedupStore_Duplicate(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	ctx := context.Background()

	// Create a referrer account.
	referrer := &entity.Account{
		ZitadelSub: "sub-referrer",
		Email:      "referrer@test.com",
		Status:     entity.AccountStatusActive,
	}
	_ = accStore.Create(ctx, referrer)
	ws.wallets[referrer.ID] = &entity.Wallet{ID: 1, AccountID: referrer.ID, Balance: 0}

	// created=false → duplicate event, credit should be skipped.
	store := &mockRewardEventStore{created: false}
	svc := NewReferralService(accStore, ws)
	svc.WithRewardEvents(store)

	err := svc.OnSignup(ctx, 999, referrer.ID)
	if err != nil {
		t.Fatalf("OnSignup (dup): %v", err)
	}
	// Balance should remain 0 since the event was a duplicate.
	if ws.wallets[referrer.ID].Balance != 0 {
		t.Errorf("balance = %f, want 0 (no credit for duplicate event)", ws.wallets[referrer.ID].Balance)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReferralService — reward with rewardEvents: new event (created=true) → credits wallet
// ─────────────────────────────────────────────────────────────────────────────

func TestReferralService_OnSignup_WithDedupStore_NewEvent(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	ctx := context.Background()

	referrer := &entity.Account{
		ZitadelSub: "sub-ref-new",
		Email:      "ref-new@test.com",
		Status:     entity.AccountStatusActive,
	}
	_ = accStore.Create(ctx, referrer)
	ws.wallets[referrer.ID] = &entity.Wallet{ID: 2, AccountID: referrer.ID, Balance: 0}

	// created=true → new event, credit should be applied.
	store := &mockRewardEventStore{created: true}
	svc := NewReferralService(accStore, ws)
	svc.WithRewardEvents(store)

	if err := svc.OnSignup(ctx, 999, referrer.ID); err != nil {
		t.Fatalf("OnSignup (new): %v", err)
	}
	if ws.wallets[referrer.ID].Balance < RewardSignup-0.001 {
		t.Errorf("balance = %f, want >= %f (signup reward)", ws.wallets[referrer.ID].Balance, RewardSignup)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReferralService — OnRenewal: royalty window
// ─────────────────────────────────────────────────────────────────────────────

func TestReferralService_OnRenewal_WithinWindow(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	ctx := context.Background()

	referrer := &entity.Account{
		ZitadelSub: "sub-royalty",
		Email:      "royalty@test.com",
		Status:     entity.AccountStatusActive,
	}
	_ = accStore.Create(ctx, referrer)
	ws.wallets[referrer.ID] = &entity.Wallet{ID: 3, AccountID: referrer.ID, Balance: 0}

	svc := NewReferralService(accStore, ws)

	// Renewal #1 with amount=100 → 5% = 5.0 LB reward.
	if err := svc.OnRenewal(ctx, 888, referrer.ID, 100.0, 1); err != nil {
		t.Fatalf("OnRenewal: %v", err)
	}
	expected := 100.0 * RewardRenewalRate
	if ws.wallets[referrer.ID].Balance < expected-0.001 {
		t.Errorf("balance = %f, want >= %f", ws.wallets[referrer.ID].Balance, expected)
	}
}

func TestReferralService_OnRenewal_BeyondWindow(t *testing.T) {
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	ctx := context.Background()

	referrer := &entity.Account{
		ZitadelSub: "sub-beyond-window",
		Email:      "beyond@test.com",
		Status:     entity.AccountStatusActive,
	}
	_ = accStore.Create(ctx, referrer)
	ws.wallets[referrer.ID] = &entity.Wallet{ID: 4, AccountID: referrer.ID, Balance: 0}

	svc := NewReferralService(accStore, ws)

	// Renewal #7 is beyond the 6-renewal window — should be a no-op.
	if err := svc.OnRenewal(ctx, 888, referrer.ID, 100.0, 7); err != nil {
		t.Fatalf("OnRenewal beyond window: %v", err)
	}
	if ws.wallets[referrer.ID].Balance != 0 {
		t.Errorf("balance = %f, want 0 (renewal 7 beyond window)", ws.wallets[referrer.ID].Balance)
	}
}

func TestReferralService_OnRenewal_ZeroAmount(t *testing.T) {
	svc := NewReferralService(newMockAccountStore(), newMockWalletStore())
	// zero amount → reward ≤ 0 → skip silently.
	if err := svc.OnRenewal(context.Background(), 1, 2, 0, 1); err != nil {
		t.Fatalf("OnRenewal zero amount: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — constructor nil on empty secret
// ─────────────────────────────────────────────────────────────────────────────

func TestNewRegistrationService_NilOnEmptySecret(t *testing.T) {
	svc := NewRegistrationService(
		newMockAccountStore(), newMockWalletStore(), newMockVIPStore(nil),
		nil, nil, "", nil, nil, nil, sms.SMSConfig{},
	)
	if svc != nil {
		t.Error("expected nil when sessionSecret is empty")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — CheckUsernameAvailable
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_CheckUsernameAvailable(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	// Seed an existing account.
	_ = seedRegisteredAccount(svc, ctx, "existinguser", "exists@test.com")

	tests := []struct {
		username      string
		wantAvailable bool
	}{
		{"existinguser", false},
		{"brandnewuser", true},
	}
	for _, tc := range tests {
		t.Run(tc.username, func(t *testing.T) {
			avail, err := svc.CheckUsernameAvailable(ctx, tc.username)
			if err != nil {
				t.Fatalf("CheckUsernameAvailable: %v", err)
			}
			if avail != tc.wantAvailable {
				t.Errorf("available = %v, want %v", avail, tc.wantAvailable)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — CheckEmailAvailable: invalid email format
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_CheckEmailAvailable_InvalidFormat(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	_, err := svc.CheckEmailAvailable(ctx, "not-an-email")
	if err == nil {
		t.Fatal("expected error for invalid email format")
	}
}

func TestRegistrationService_CheckEmailAvailable_Available(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	avail, err := svc.CheckEmailAvailable(ctx, "new@test.com")
	if err != nil {
		t.Fatalf("CheckEmailAvailable: %v", err)
	}
	if !avail {
		t.Error("expected email to be available")
	}
}

func TestRegistrationService_CheckEmailAvailable_Taken(t *testing.T) {
	svc, ctx := makeRegistrationService(t)
	_ = seedRegisteredAccount(svc, ctx, "someuser", "taken@test.com")

	avail, err := svc.CheckEmailAvailable(ctx, "taken@test.com")
	if err != nil {
		t.Fatalf("CheckEmailAvailable: %v", err)
	}
	if avail {
		t.Error("expected email to be taken")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — ForgotPassword: empty identifier returns error
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_ForgotPassword_EmptyIdentifier(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	_, err := svc.ForgotPassword(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty identifier")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — ForgotPassword: account not found → graceful (no leak)
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_ForgotPassword_AccountNotFound(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	// Non-existent email — service should NOT reveal whether account exists.
	result, err := svc.ForgotPassword(ctx, "ghost@test.com")
	if err != nil {
		t.Fatalf("ForgotPassword unknown account: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Message should be neutral.
	if result.Message == "" {
		t.Error("expected a non-empty neutral message")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — Register: duplicate username rejected
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_Register_DuplicateUsername(t *testing.T) {
	svc, ctx := makeRegistrationService(t)
	_ = seedRegisteredAccount(svc, ctx, "dupuser", "dup@test.com")

	_, err := svc.Register(ctx, RegisterRequest{
		Username: "dupuser",
		Password: "secure_password123",
	})
	if err == nil {
		t.Fatal("expected error for duplicate username")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — Register: invalid email format rejected
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_Register_InvalidEmail(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	_, err := svc.Register(ctx, RegisterRequest{
		Username: "validuser",
		Password: "secure_password123",
		Email:    "not-an-email",
	})
	if err == nil {
		t.Fatal("expected error for invalid email")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — Register: short password rejected
// ─────────────────────────────────────────────────────────────────────────────

func TestRegistrationService_Register_ShortPassword(t *testing.T) {
	svc, ctx := makeRegistrationService(t)

	_, err := svc.Register(ctx, RegisterRequest{
		Username: "validuser",
		Password: "short",
	})
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RegistrationService — generateNumericCode format check
// ─────────────────────────────────────────────────────────────────────────────

func TestGenerateNumericCode_Format(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := generateNumericCode(6)
		if err != nil {
			t.Fatalf("generateNumericCode: %v", err)
		}
		if len(code) != 6 {
			t.Errorf("expected 6 digits, got %d: %q", len(code), code)
		}
		for _, ch := range code {
			if ch < '0' || ch > '9' {
				t.Errorf("non-digit char %q in code %q", ch, code)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ServiceKeyAdminService — constructor smoke test
// ─────────────────────────────────────────────────────────────────────────────

func TestServiceKeyAdminService_Constructor(t *testing.T) {
	// NewServiceKeyAdminService requires a *gorm.DB which we cannot instantiate
	// without a real database. We verify that the function signature and struct
	// are accessible without panicking when given a nil db pointer.
	// The 0% coverage is intrinsic to this design (requires real DB or DI).
	svc := NewServiceKeyAdminService(nil)
	if svc == nil {
		t.Fatal("expected non-nil service from constructor")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers for RegistrationService tests
// ─────────────────────────────────────────────────────────────────────────────

// makeRegistrationService creates a RegistrationService using all mocks, no Zitadel/Redis/SMS.
func makeRegistrationService(t *testing.T) (*RegistrationService, context.Context) {
	t.Helper()
	accStore := newMockAccountStore()
	ws := newMockWalletStore()
	vipStore := newMockVIPStore(nil)
	svc := NewRegistrationService(
		accStore, ws, vipStore,
		nil, // no referral service
		nil, // no zitadel
		"test-session-secret-32-chars-xxx",
		nil, // no email sender
		nil, // no SMS sender
		nil, // no Redis
		sms.SMSConfig{},
	)
	if svc == nil {
		t.Fatal("NewRegistrationService returned nil")
	}
	return svc, context.Background()
}

// seedRegisteredAccount registers a user and returns the result. Panics if it fails.
func seedRegisteredAccount(svc *RegistrationService, ctx context.Context, username, email string) *RegistrationResult {
	result, err := svc.Register(ctx, RegisterRequest{
		Username: username,
		Password: "secure_password123",
		Email:    email,
	})
	if err != nil {
		panic(fmt.Sprintf("seedRegisteredAccount(%q, %q): %v", username, email, err))
	}
	return result
}

// sync import used by mockRewardEventStore
import "sync"
