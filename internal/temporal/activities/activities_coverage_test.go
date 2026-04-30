package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── Minimal store stubs ────────────────────────────────────────────────────────
// These implement the unexported interfaces in internal/app/interfaces.go.
// They are defined in the same package (activities) so they compile alongside
// the production code without any import-cycle issues.

// cstubSubscriptionStore satisfies app's internal subscriptionStore interface.
type cstubSubscriptionStore struct {
	subs          map[int64]*entity.Subscription
	nextID        int64
	createErr     error
	updateErr     error
	getByIDErr    error
	activeExpired []entity.Subscription
	graceExpired  []entity.Subscription
	listActErr    error
	listGrcErr    error
}

func newCStubSubStore() *cstubSubscriptionStore {
	return &cstubSubscriptionStore{subs: make(map[int64]*entity.Subscription), nextID: 1}
}

func (s *cstubSubscriptionStore) Create(_ context.Context, sub *entity.Subscription) error {
	if s.createErr != nil {
		return s.createErr
	}
	sub.ID = s.nextID
	s.nextID++
	cp := *sub
	s.subs[cp.ID] = &cp
	return nil
}

func (s *cstubSubscriptionStore) Update(_ context.Context, sub *entity.Subscription) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	cp := *sub
	s.subs[cp.ID] = &cp
	return nil
}

func (s *cstubSubscriptionStore) GetByID(_ context.Context, id int64) (*entity.Subscription, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	sub, ok := s.subs[id]
	if !ok {
		return nil, nil
	}
	cp := *sub
	return &cp, nil
}

func (s *cstubSubscriptionStore) GetActive(_ context.Context, accountID int64, productID string) (*entity.Subscription, error) {
	for _, sub := range s.subs {
		if sub.AccountID == accountID && sub.ProductID == productID &&
			(sub.Status == entity.SubStatusActive || sub.Status == entity.SubStatusGrace) {
			cp := *sub
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *cstubSubscriptionStore) ListByAccount(_ context.Context, _ int64) ([]entity.Subscription, error) {
	return nil, nil
}

func (s *cstubSubscriptionStore) ListActiveExpired(_ context.Context) ([]entity.Subscription, error) {
	if s.listActErr != nil {
		return nil, s.listActErr
	}
	return s.activeExpired, nil
}

func (s *cstubSubscriptionStore) ListGraceExpired(_ context.Context) ([]entity.Subscription, error) {
	if s.listGrcErr != nil {
		return nil, s.listGrcErr
	}
	return s.graceExpired, nil
}

func (s *cstubSubscriptionStore) ListDueForRenewal(_ context.Context) ([]entity.Subscription, error) {
	return nil, nil
}

func (s *cstubSubscriptionStore) UpdateRenewalState(_ context.Context, subID int64, attempts int, nextAt *time.Time) error {
	sub, ok := s.subs[subID]
	if !ok {
		return fmt.Errorf("sub %d not found", subID)
	}
	sub.RenewalAttempts = attempts
	sub.NextRenewalAt = nextAt
	return nil
}

func (s *cstubSubscriptionStore) UpsertEntitlement(_ context.Context, _ *entity.AccountEntitlement) error {
	return nil
}

func (s *cstubSubscriptionStore) GetEntitlements(_ context.Context, _ int64, _ string) ([]entity.AccountEntitlement, error) {
	return nil, nil
}

func (s *cstubSubscriptionStore) DeleteEntitlements(_ context.Context, _ int64, _ string) error {
	return nil
}

// cstubPlanStore satisfies app's internal planStore interface.
type cstubPlanStore struct {
	plans   map[int64]*entity.ProductPlan
	planErr error
}

func newCStubPlanStore(plans ...*entity.ProductPlan) *cstubPlanStore {
	s := &cstubPlanStore{plans: make(map[int64]*entity.ProductPlan)}
	for _, p := range plans {
		s.plans[p.ID] = p
	}
	return s
}

func (s *cstubPlanStore) GetPlanByID(_ context.Context, id int64) (*entity.ProductPlan, error) {
	if s.planErr != nil {
		return nil, s.planErr
	}
	p, ok := s.plans[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *cstubPlanStore) ListActive(_ context.Context) ([]entity.Product, error)              { return nil, nil }
func (s *cstubPlanStore) ListPlans(_ context.Context, _ string) ([]entity.ProductPlan, error)  { return nil, nil }
func (s *cstubPlanStore) GetByID(_ context.Context, _ string) (*entity.Product, error)         { return nil, nil }
func (s *cstubPlanStore) Create(_ context.Context, _ *entity.Product) error                    { return nil }
func (s *cstubPlanStore) Update(_ context.Context, _ *entity.Product) error                    { return nil }
func (s *cstubPlanStore) CreatePlan(_ context.Context, _ *entity.ProductPlan) error             { return nil }
func (s *cstubPlanStore) UpdatePlan(_ context.Context, _ *entity.ProductPlan) error             { return nil }

// cstubVIPStore satisfies app's internal vipStore interface.
type cstubVIPStore struct{}

func (s *cstubVIPStore) GetOrCreate(_ context.Context, accountID int64) (*entity.AccountVIP, error) {
	return &entity.AccountVIP{AccountID: accountID}, nil
}
func (s *cstubVIPStore) Update(_ context.Context, _ *entity.AccountVIP) error { return nil }
func (s *cstubVIPStore) ListConfigs(_ context.Context) ([]entity.VIPLevelConfig, error) {
	return nil, nil
}

// cstubWalletStore satisfies app's internal walletStore interface.
type cstubWalletStore struct {
	txs         []entity.WalletTransaction
	debitErr    error
	creditErr   error
	balance     float64
	markPaidErr error
	orders      map[string]*entity.PaymentOrder
	expireCount int64
	expireErr   error
}

func newCStubWalletStore() *cstubWalletStore {
	return &cstubWalletStore{orders: make(map[string]*entity.PaymentOrder)}
}

func (s *cstubWalletStore) GetOrCreate(_ context.Context, accountID int64) (*entity.Wallet, error) {
	return &entity.Wallet{AccountID: accountID, Balance: s.balance}, nil
}

func (s *cstubWalletStore) GetByAccountID(_ context.Context, accountID int64) (*entity.Wallet, error) {
	return &entity.Wallet{AccountID: accountID, Balance: s.balance}, nil
}

func (s *cstubWalletStore) Credit(_ context.Context, accountID int64, amount float64, txType, _, _, _, _ string) (*entity.WalletTransaction, error) {
	if s.creditErr != nil {
		return nil, s.creditErr
	}
	tx := &entity.WalletTransaction{ID: int64(len(s.txs) + 1), AccountID: accountID, Amount: amount, Type: txType}
	s.txs = append(s.txs, *tx)
	return tx, nil
}

func (s *cstubWalletStore) Debit(_ context.Context, accountID int64, amount float64, txType, _, _, _, _ string) (*entity.WalletTransaction, error) {
	if s.debitErr != nil {
		return nil, s.debitErr
	}
	tx := &entity.WalletTransaction{ID: int64(len(s.txs) + 1), AccountID: accountID, Amount: -amount, Type: txType}
	s.txs = append(s.txs, *tx)
	return tx, nil
}

func (s *cstubWalletStore) ListTransactions(_ context.Context, _ int64, _, _ int) ([]entity.WalletTransaction, int64, error) {
	return s.txs, int64(len(s.txs)), nil
}

func (s *cstubWalletStore) CreatePaymentOrder(_ context.Context, o *entity.PaymentOrder) error {
	s.orders[o.OrderNo] = o
	return nil
}

func (s *cstubWalletStore) UpdatePaymentOrder(_ context.Context, o *entity.PaymentOrder) error {
	s.orders[o.OrderNo] = o
	return nil
}

func (s *cstubWalletStore) GetPaymentOrderByNo(_ context.Context, orderNo string) (*entity.PaymentOrder, error) {
	o, ok := s.orders[orderNo]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (s *cstubWalletStore) GetRedemptionCode(_ context.Context, _ string) (*entity.RedemptionCode, error) {
	return nil, nil
}

func (s *cstubWalletStore) UpdateRedemptionCode(_ context.Context, _ *entity.RedemptionCode) error {
	return nil
}

func (s *cstubWalletStore) ListOrders(_ context.Context, _ int64, _, _ int) ([]entity.PaymentOrder, int64, error) {
	return nil, 0, nil
}

func (s *cstubWalletStore) MarkPaymentOrderPaid(_ context.Context, orderNo string) (*entity.PaymentOrder, bool, error) {
	if s.markPaidErr != nil {
		return nil, false, s.markPaidErr
	}
	o, ok := s.orders[orderNo]
	if !ok {
		return nil, false, nil
	}
	if o.Status == entity.OrderStatusPaid {
		cp := *o
		return &cp, false, nil
	}
	now := time.Now().UTC()
	o.Status = entity.OrderStatusPaid
	o.PaidAt = &now
	cp := *o
	return &cp, true, nil
}

func (s *cstubWalletStore) RedeemCode(_ context.Context, _ int64, _ string) (*entity.WalletTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *cstubWalletStore) GetPendingOrderByIdempotencyKey(_ context.Context, _ string) (*entity.PaymentOrder, error) {
	return nil, nil
}

func (s *cstubWalletStore) ExpireStalePendingOrders(_ context.Context, _ time.Duration) (int64, error) {
	if s.expireErr != nil {
		return 0, s.expireErr
	}
	return s.expireCount, nil
}

func (s *cstubWalletStore) CreatePreAuth(_ context.Context, _ *entity.WalletPreAuthorization) error {
	return nil
}

func (s *cstubWalletStore) GetPreAuthByID(_ context.Context, _ int64) (*entity.WalletPreAuthorization, error) {
	return nil, nil
}

func (s *cstubWalletStore) GetPreAuthByReference(_ context.Context, _, _ string) (*entity.WalletPreAuthorization, error) {
	return nil, nil
}

func (s *cstubWalletStore) SettlePreAuth(_ context.Context, _ int64, _ float64) (*entity.WalletPreAuthorization, error) {
	return nil, errors.New("not implemented")
}

func (s *cstubWalletStore) ReleasePreAuth(_ context.Context, _ int64) (*entity.WalletPreAuthorization, error) {
	return nil, errors.New("not implemented")
}

func (s *cstubWalletStore) ExpireStalePreAuths(_ context.Context) (int64, error) {
	if s.expireErr != nil {
		return 0, s.expireErr
	}
	return s.expireCount, nil
}

func (s *cstubWalletStore) CountActivePreAuths(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (s *cstubWalletStore) CountPendingOrders(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (s *cstubWalletStore) FindStalePendingOrders(_ context.Context, _ time.Duration) ([]entity.PaymentOrder, error) {
	return nil, nil
}

func (s *cstubWalletStore) FindPaidTopupOrdersWithoutCredit(_ context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	return nil, nil
}

func (s *cstubWalletStore) CreateReconciliationIssue(_ context.Context, _ *entity.ReconciliationIssue) error {
	return nil
}

func (s *cstubWalletStore) ListReconciliationIssues(_ context.Context, _ string, _, _ int) ([]entity.ReconciliationIssue, int64, error) {
	return nil, 0, nil
}

func (s *cstubWalletStore) ResolveReconciliationIssue(_ context.Context, _ int64, _, _ string) error {
	return nil
}

// cstubEntitlementCache satisfies app's internal entitlementCache interface (no-op).
type cstubEntitlementCache struct{}

func (c *cstubEntitlementCache) Get(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return nil, nil
}
func (c *cstubEntitlementCache) Set(_ context.Context, _ int64, _ string, _ map[string]string) error {
	return nil
}
func (c *cstubEntitlementCache) Invalidate(_ context.Context, _ int64, _ string) error {
	return nil
}

// cstubAccountStore satisfies app's internal accountStore interface.
type cstubAccountStore struct {
	byID   map[int64]*entity.Account
	getErr error
}

func newCStubAccountStore() *cstubAccountStore {
	return &cstubAccountStore{byID: make(map[int64]*entity.Account)}
}

func (s *cstubAccountStore) Create(_ context.Context, a *entity.Account) error {
	s.byID[a.ID] = a
	return nil
}

func (s *cstubAccountStore) Update(_ context.Context, a *entity.Account) error {
	s.byID[a.ID] = a
	return nil
}

func (s *cstubAccountStore) GetByID(_ context.Context, id int64) (*entity.Account, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	a, ok := s.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (s *cstubAccountStore) GetByEmail(_ context.Context, _ string) (*entity.Account, error)          { return nil, nil }
func (s *cstubAccountStore) GetByZitadelSub(_ context.Context, _ string) (*entity.Account, error)     { return nil, nil }
func (s *cstubAccountStore) GetByLurusID(_ context.Context, _ string) (*entity.Account, error)        { return nil, nil }
func (s *cstubAccountStore) GetByAffCode(_ context.Context, _ string) (*entity.Account, error)        { return nil, nil }
func (s *cstubAccountStore) GetByPhone(_ context.Context, _ string) (*entity.Account, error)          { return nil, nil }
func (s *cstubAccountStore) GetByUsername(_ context.Context, _ string) (*entity.Account, error)       { return nil, nil }
func (s *cstubAccountStore) GetByOAuthBinding(_ context.Context, _, _ string) (*entity.Account, error) { return nil, nil }
func (s *cstubAccountStore) SetNewAPIUserID(_ context.Context, _ int64, _ int) error               { return nil }
func (s *cstubAccountStore) ListWithoutNewAPIUser(_ context.Context, _ int) ([]*entity.Account, error) { return nil, nil }
func (s *cstubAccountStore) List(_ context.Context, _ string, _, _ int) ([]*entity.Account, int64, error) {
	return nil, 0, nil
}
func (s *cstubAccountStore) UpsertOAuthBinding(_ context.Context, _ *entity.OAuthBinding) error { return nil }

// ── Helpers ────────────────────────────────────────────────────────────────────

// makePlan creates a ProductPlan with valid JSON features for the given BillingCycle.
func makePlan(id int64, code, cycle string, priceCNY float64) *entity.ProductPlan {
	features, _ := json.Marshal(map[string]any{"plan_code": code})
	return &entity.ProductPlan{
		ID:           id,
		Code:         code,
		BillingCycle: cycle,
		PriceCNY:     priceCNY,
		Features:     features,
	}
}

// buildCSubscriptionService wires a SubscriptionService with the given stubs.
func buildCSubscriptionService(subStore *cstubSubscriptionStore, planStore *cstubPlanStore) *app.SubscriptionService {
	entSvc := app.NewEntitlementService(subStore, planStore, &cstubEntitlementCache{})
	return app.NewSubscriptionService(subStore, planStore, entSvc, 3)
}

// buildCWalletService wires a WalletService with the given stub.
func buildCWalletService(ws *cstubWalletStore) *app.WalletService {
	vipSvc := app.NewVIPService(&cstubVIPStore{}, ws)
	return app.NewWalletService(ws, vipSvc)
}

// buildCAccountService wires an AccountService with the given stubs.
func buildCAccountService(as *cstubAccountStore) *app.AccountService {
	return app.NewAccountService(as, newCStubWalletStore(), &cstubVIPStore{})
}

// ── SubscriptionActivities tests ──────────────────────────────────────────────

func TestCoverage_SubscriptionActivities_Activate_MonthlyPlan(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore(makePlan(1, "basic-monthly", entity.BillingCycleMonthly, 29.9))
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	out, err := a.Activate(context.Background(), ActivateInput{
		AccountID: 10, ProductID: "api", PlanID: 1, PaymentMethod: "wallet",
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if out.SubscriptionID == 0 {
		t.Error("SubscriptionID should be non-zero")
	}
	// Monthly plan must produce a non-empty ExpiresAt
	if out.ExpiresAt == "" {
		t.Error("ExpiresAt should be set for monthly plan")
	}
	_, parseErr := time.Parse("2006-01-02T15:04:05Z07:00", out.ExpiresAt)
	if parseErr != nil {
		t.Errorf("ExpiresAt %q is not valid RFC3339: %v", out.ExpiresAt, parseErr)
	}
}

func TestCoverage_SubscriptionActivities_Activate_ForeverPlan(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore(makePlan(2, "forever", entity.BillingCycleForever, 0))
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	out, err := a.Activate(context.Background(), ActivateInput{
		AccountID: 11, ProductID: "api", PlanID: 2,
	})
	if err != nil {
		t.Fatalf("Activate forever plan: %v", err)
	}
	// Forever plan has no expiry
	if out.ExpiresAt != "" {
		t.Errorf("ExpiresAt should be empty for forever plan, got %q", out.ExpiresAt)
	}
}

func TestCoverage_SubscriptionActivities_Activate_YearlyPlan(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore(makePlan(3, "pro-yearly", entity.BillingCycleYearly, 299.0))
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	out, err := a.Activate(context.Background(), ActivateInput{
		AccountID: 12, ProductID: "lucrum", PlanID: 3, PaymentMethod: "stripe",
	})
	if err != nil {
		t.Fatalf("Activate yearly plan: %v", err)
	}
	if out.ExpiresAt == "" {
		t.Error("ExpiresAt should be set for yearly plan")
	}
}

func TestCoverage_SubscriptionActivities_Activate_PlanNotFound(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore() // empty
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	_, err := a.Activate(context.Background(), ActivateInput{
		AccountID: 1, ProductID: "api", PlanID: 999,
	})
	if err == nil {
		t.Fatal("expected error for missing plan")
	}
}

func TestCoverage_SubscriptionActivities_Activate_CreateError(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.createErr = errors.New("db write failure")
	planStore := newCStubPlanStore(makePlan(1, "monthly", entity.BillingCycleMonthly, 9.9))
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	_, err := a.Activate(context.Background(), ActivateInput{
		AccountID: 1, ProductID: "api", PlanID: 1,
	})
	if err == nil {
		t.Fatal("expected error on create failure")
	}
}

func TestCoverage_SubscriptionActivities_GetSubscription_Found(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.subs[5] = &entity.Subscription{ID: 5, AccountID: 1, ProductID: "lucrum", Status: entity.SubStatusActive}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	result, err := a.GetSubscription(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if result == nil || result.ID != 5 {
		t.Errorf("expected ID=5, got %v", result)
	}
}

func TestCoverage_SubscriptionActivities_GetSubscription_NotFound(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	result, err := a.GetSubscription(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for missing subscription")
	}
}

func TestCoverage_SubscriptionActivities_GetSubscription_DBError(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.getByIDErr = errors.New("db timeout")
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	_, err := a.GetSubscription(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error from DB failure")
	}
}

func TestCoverage_SubscriptionActivities_ResetRenewalState_Success(t *testing.T) {
	subStore := newCStubSubStore()
	sub := &entity.Subscription{ID: 7, RenewalAttempts: 4}
	subStore.subs[7] = sub
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	if err := a.ResetRenewalState(context.Background(), 7); err != nil {
		t.Fatalf("ResetRenewalState: %v", err)
	}
	if subStore.subs[7].RenewalAttempts != 0 {
		t.Errorf("RenewalAttempts = %d, want 0", subStore.subs[7].RenewalAttempts)
	}
}

func TestCoverage_SubscriptionActivities_ResetRenewalState_NotFound(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	// UpdateRenewalState on missing ID returns error from the stub
	err := a.ResetRenewalState(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for missing subscription")
	}
}

func TestCoverage_SubscriptionActivities_Expire_Success(t *testing.T) {
	subStore := newCStubSubStore()
	past := time.Now().UTC().Add(-1 * time.Hour)
	subStore.subs[8] = &entity.Subscription{
		ID: 8, AccountID: 1, ProductID: "lucrum",
		Status: entity.SubStatusActive, ExpiresAt: &past,
	}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	if err := a.Expire(context.Background(), 8); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if subStore.subs[8].Status != entity.SubStatusGrace {
		t.Errorf("status = %q, want %q", subStore.subs[8].Status, entity.SubStatusGrace)
	}
}

func TestCoverage_SubscriptionActivities_Expire_NotFound(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	err := a.Expire(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for missing subscription")
	}
}

func TestCoverage_SubscriptionActivities_Expire_WrongStatus(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.subs[9] = &entity.Subscription{ID: 9, Status: entity.SubStatusGrace}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	err := a.Expire(context.Background(), 9)
	if err == nil {
		t.Fatal("expected error expiring grace-status subscription")
	}
}

func TestCoverage_SubscriptionActivities_EndGrace_Success(t *testing.T) {
	subStore := newCStubSubStore()
	past := time.Now().UTC().Add(-1 * time.Hour)
	subStore.subs[10] = &entity.Subscription{
		ID: 10, AccountID: 2, ProductID: "api",
		Status: entity.SubStatusGrace, GraceUntil: &past,
	}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	if err := a.EndGrace(context.Background(), 10); err != nil {
		t.Fatalf("EndGrace: %v", err)
	}
	if subStore.subs[10].Status != entity.SubStatusExpired {
		t.Errorf("status = %q, want %q", subStore.subs[10].Status, entity.SubStatusExpired)
	}
}

func TestCoverage_SubscriptionActivities_EndGrace_NotFound(t *testing.T) {
	subStore := newCStubSubStore()
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	err := a.EndGrace(context.Background(), 9999)
	if err == nil {
		t.Fatal("expected error for missing subscription")
	}
}

func TestCoverage_SubscriptionActivities_EndGrace_WrongStatus(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.subs[11] = &entity.Subscription{ID: 11, Status: entity.SubStatusActive}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	err := a.EndGrace(context.Background(), 11)
	if err == nil {
		t.Fatal("expected error ending grace on active subscription")
	}
}

func TestCoverage_SubscriptionActivities_ListActiveExpired_Success(t *testing.T) {
	subStore := newCStubSubStore()
	past := time.Now().Add(-time.Hour)
	subStore.activeExpired = []entity.Subscription{
		{ID: 1, AccountID: 1, ProductID: "api", Status: entity.SubStatusActive, ExpiresAt: &past},
		{ID: 2, AccountID: 2, ProductID: "lucrum", Status: entity.SubStatusActive, ExpiresAt: &past},
	}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	results, err := a.ListActiveExpired(context.Background())
	if err != nil {
		t.Fatalf("ListActiveExpired: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("len = %d, want 2", len(results))
	}
}

func TestCoverage_SubscriptionActivities_ListActiveExpired_Empty(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.activeExpired = []entity.Subscription{}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	results, err := a.ListActiveExpired(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

func TestCoverage_SubscriptionActivities_ListActiveExpired_Error(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.listActErr = errors.New("query failed")
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	_, err := a.ListActiveExpired(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCoverage_SubscriptionActivities_ListGraceExpired_Success(t *testing.T) {
	subStore := newCStubSubStore()
	past := time.Now().Add(-time.Hour)
	subStore.graceExpired = []entity.Subscription{
		{ID: 3, AccountID: 3, ProductID: "switch", Status: entity.SubStatusGrace, GraceUntil: &past},
	}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	results, err := a.ListGraceExpired(context.Background())
	if err != nil {
		t.Fatalf("ListGraceExpired: %v", err)
	}
	if len(results) != 1 || results[0].ID != 3 {
		t.Errorf("unexpected results: %v", results)
	}
}

func TestCoverage_SubscriptionActivities_ListGraceExpired_Empty(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.graceExpired = []entity.Subscription{}
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	results, err := a.ListGraceExpired(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

func TestCoverage_SubscriptionActivities_ListGraceExpired_Error(t *testing.T) {
	subStore := newCStubSubStore()
	subStore.listGrcErr = errors.New("query timeout")
	planStore := newCStubPlanStore()
	a := &SubscriptionActivities{Subs: buildCSubscriptionService(subStore, planStore)}

	_, err := a.ListGraceExpired(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCoverage_ToSummaries_AllFields(t *testing.T) {
	subs := []entity.Subscription{
		{ID: 1, AccountID: 10, ProductID: "api", PlanID: 5, Status: entity.SubStatusActive, AutoRenew: true},
		{ID: 2, AccountID: 20, ProductID: "lucrum", PlanID: 6, Status: entity.SubStatusGrace, AutoRenew: false},
	}
	summaries := toSummaries(subs)
	if len(summaries) != 2 {
		t.Fatalf("len = %d, want 2", len(summaries))
	}
	for i, s := range summaries {
		if s.ID != subs[i].ID {
			t.Errorf("[%d] ID mismatch: got %d want %d", i, s.ID, subs[i].ID)
		}
		if s.AccountID != subs[i].AccountID {
			t.Errorf("[%d] AccountID mismatch", i)
		}
		if s.ProductID != subs[i].ProductID {
			t.Errorf("[%d] ProductID mismatch", i)
		}
		if s.PlanID != subs[i].PlanID {
			t.Errorf("[%d] PlanID mismatch", i)
		}
		if s.Status != subs[i].Status {
			t.Errorf("[%d] Status mismatch", i)
		}
		if s.AutoRenew != subs[i].AutoRenew {
			t.Errorf("[%d] AutoRenew mismatch", i)
		}
	}
}

func TestCoverage_ToSummaries_Empty(t *testing.T) {
	result := toSummaries(nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

// ── WalletActivities tests ─────────────────────────────────────────────────────

func TestCoverage_WalletActivities_Debit_Success(t *testing.T) {
	ws := newCStubWalletStore()
	ws.balance = 100.0
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	out, err := a.Debit(context.Background(), DebitInput{
		AccountID: 1, Amount: 29.9, TxType: "subscription_renewal",
		Desc: "test debit", RefType: "subscription", RefID: "sub:1", ProductID: "lucrum",
	})
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if out.TransactionID == 0 {
		t.Error("TransactionID should be non-zero on success")
	}
}

func TestCoverage_WalletActivities_Debit_Error(t *testing.T) {
	ws := newCStubWalletStore()
	ws.debitErr = errors.New("insufficient funds")
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	_, err := a.Debit(context.Background(), DebitInput{
		AccountID: 1, Amount: 50.0, TxType: "subscription_renewal",
	})
	if err == nil {
		t.Fatal("expected error on debit failure")
	}
}

func TestCoverage_WalletActivities_Credit_Success(t *testing.T) {
	ws := newCStubWalletStore()
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	err := a.Credit(context.Background(), CreditInput{
		AccountID: 1, Amount: 29.9, TxType: "subscription_renewal_refund",
		Desc: "refund", RefType: "subscription", RefID: "sub:1", ProductID: "lucrum",
	})
	if err != nil {
		t.Fatalf("Credit: %v", err)
	}
}

func TestCoverage_WalletActivities_Credit_Error(t *testing.T) {
	ws := newCStubWalletStore()
	ws.creditErr = errors.New("db write failed")
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	err := a.Credit(context.Background(), CreditInput{
		AccountID: 1, Amount: 10.0, TxType: "refund",
	})
	if err == nil {
		t.Fatal("expected error when credit fails")
	}
}

func TestCoverage_WalletActivities_MarkOrderPaid_WithPlanID(t *testing.T) {
	ws := newCStubWalletStore()
	planID := int64(10)
	ws.orders["LO001"] = &entity.PaymentOrder{
		OrderNo:       "LO001",
		AccountID:     5,
		OrderType:     "subscription",
		ProductID:     "lucrum",
		PlanID:        &planID,
		AmountCNY:     29.9,
		PaymentMethod: "stripe",
		ExternalID:    "pi_abc",
		Status:        entity.OrderStatusPending,
	}
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	out, err := a.MarkOrderPaid(context.Background(), "LO001")
	if err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}
	if out.OrderNo != "LO001" {
		t.Errorf("OrderNo = %q, want LO001", out.OrderNo)
	}
	if out.AccountID != 5 {
		t.Errorf("AccountID = %d, want 5", out.AccountID)
	}
	if out.PlanID != 10 {
		t.Errorf("PlanID = %d, want 10", out.PlanID)
	}
	if out.PaymentMethod != "stripe" {
		t.Errorf("PaymentMethod = %q, want stripe", out.PaymentMethod)
	}
	if out.ExternalID != "pi_abc" {
		t.Errorf("ExternalID = %q, want pi_abc", out.ExternalID)
	}
}

func TestCoverage_WalletActivities_MarkOrderPaid_NilPlanID(t *testing.T) {
	ws := newCStubWalletStore()
	ws.orders["LO002"] = &entity.PaymentOrder{
		OrderNo:   "LO002",
		AccountID: 6,
		OrderType: "topup",
		PlanID:    nil,
		AmountCNY: 100.0,
		Status:    entity.OrderStatusPending,
	}
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	out, err := a.MarkOrderPaid(context.Background(), "LO002")
	if err != nil {
		t.Fatalf("MarkOrderPaid: %v", err)
	}
	if out.PlanID != 0 {
		t.Errorf("PlanID = %d, want 0 for nil", out.PlanID)
	}
}

func TestCoverage_WalletActivities_MarkOrderPaid_Idempotent(t *testing.T) {
	ws := newCStubWalletStore()
	ws.orders["LO003"] = &entity.PaymentOrder{
		OrderNo:   "LO003",
		AccountID: 7,
		OrderType: "topup",
		AmountCNY: 50.0,
		Status:    entity.OrderStatusPaid, // already paid
	}
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	// Second call should not error — MarkOrderPaid is idempotent
	out, err := a.MarkOrderPaid(context.Background(), "LO003")
	if err != nil {
		t.Fatalf("idempotent MarkOrderPaid: %v", err)
	}
	if out.OrderNo != "LO003" {
		t.Errorf("OrderNo = %q, want LO003", out.OrderNo)
	}
}

func TestCoverage_WalletActivities_MarkOrderPaid_DBError(t *testing.T) {
	ws := newCStubWalletStore()
	ws.markPaidErr = errors.New("db constraint violation")
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	_, err := a.MarkOrderPaid(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error on DB failure")
	}
}

func TestCoverage_WalletActivities_ExpireStalePendingOrders_Success(t *testing.T) {
	ws := newCStubWalletStore()
	ws.expireCount = 5
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	count, err := a.ExpireStalePendingOrders(context.Background())
	if err != nil {
		t.Fatalf("ExpireStalePendingOrders: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestCoverage_WalletActivities_ExpireStalePendingOrders_ZeroCount(t *testing.T) {
	ws := newCStubWalletStore()
	ws.expireCount = 0
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	count, err := a.ExpireStalePendingOrders(context.Background())
	if err != nil {
		t.Fatalf("ExpireStalePendingOrders: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestCoverage_WalletActivities_ExpireStalePreAuths_Success(t *testing.T) {
	ws := newCStubWalletStore()
	ws.expireCount = 3
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	count, err := a.ExpireStalePreAuths(context.Background())
	if err != nil {
		t.Fatalf("ExpireStalePreAuths: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestCoverage_WalletActivities_ExpireStalePreAuths_ZeroCount(t *testing.T) {
	ws := newCStubWalletStore()
	a := &WalletActivities{Wallets: buildCWalletService(ws)}

	count, err := a.ExpireStalePreAuths(context.Background())
	if err != nil {
		t.Fatalf("ExpireStalePreAuths: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

// ── NotificationActivities tests ──────────────────────────────────────────────

func TestCoverage_NotificationActivities_SendExpiryReminder_Success(t *testing.T) {
	as := newCStubAccountStore()
	as.byID[42] = &entity.Account{ID: 42, Email: "user@example.com", DisplayName: "Test User"}
	mailer := &mockMailer{}
	a := &NotificationActivities{Mailer: mailer, Accounts: buildCAccountService(as)}

	err := a.SendExpiryReminder(context.Background(), SendReminderInput{
		AccountID: 42, SubscriptionID: 5, ProductID: "lucrum",
		DaysLeft: 3, ExpiresAt: "2026-04-22T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("SendExpiryReminder: %v", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(mailer.sent))
	}
	if mailer.sent[0].to != "user@example.com" {
		t.Errorf("to = %q, want user@example.com", mailer.sent[0].to)
	}
	// Verify subject contains days-left value
	expected := "3 day(s)"
	if !containsStr(mailer.sent[0].subject, expected) {
		t.Errorf("subject %q does not contain %q", mailer.sent[0].subject, expected)
	}
}

func TestCoverage_NotificationActivities_SendExpiryReminder_OneDayLeft(t *testing.T) {
	as := newCStubAccountStore()
	as.byID[1] = &entity.Account{ID: 1, Email: "urgent@example.com", DisplayName: "User"}
	mailer := &mockMailer{}
	a := &NotificationActivities{Mailer: mailer, Accounts: buildCAccountService(as)}

	err := a.SendExpiryReminder(context.Background(), SendReminderInput{
		AccountID: 1, SubscriptionID: 10, DaysLeft: 1,
	})
	if err != nil {
		t.Fatalf("SendExpiryReminder 1 day: %v", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(mailer.sent))
	}
}

func TestCoverage_NotificationActivities_SendExpiryReminder_NoEmail(t *testing.T) {
	as := newCStubAccountStore()
	as.byID[10] = &entity.Account{ID: 10, Email: ""} // no email
	mailer := &mockMailer{}
	a := &NotificationActivities{Mailer: mailer, Accounts: buildCAccountService(as)}

	err := a.SendExpiryReminder(context.Background(), SendReminderInput{
		AccountID: 10, SubscriptionID: 5, DaysLeft: 1,
	})
	if err != nil {
		t.Fatalf("expected nil for no-email account, got: %v", err)
	}
	if len(mailer.sent) != 0 {
		t.Errorf("expected no emails sent, got %d", len(mailer.sent))
	}
}

func TestCoverage_NotificationActivities_SendExpiryReminder_AccountNotFound(t *testing.T) {
	as := newCStubAccountStore() // empty
	mailer := &mockMailer{}
	a := &NotificationActivities{Mailer: mailer, Accounts: buildCAccountService(as)}

	err := a.SendExpiryReminder(context.Background(), SendReminderInput{
		AccountID: 999, SubscriptionID: 5, DaysLeft: 3,
	})
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestCoverage_NotificationActivities_SendExpiryReminder_DBError(t *testing.T) {
	as := newCStubAccountStore()
	as.getErr = errors.New("db unreachable")
	mailer := &mockMailer{}
	a := &NotificationActivities{Mailer: mailer, Accounts: buildCAccountService(as)}

	err := a.SendExpiryReminder(context.Background(), SendReminderInput{
		AccountID: 1, SubscriptionID: 1, DaysLeft: 3,
	})
	if err == nil {
		t.Fatal("expected error from account DB failure")
	}
}

func TestCoverage_NotificationActivities_SendExpiryReminder_MailerError(t *testing.T) {
	as := newCStubAccountStore()
	as.byID[5] = &entity.Account{ID: 5, Email: "user@example.com"}
	mailer := &mockMailer{err: errors.New("SMTP connection refused")}
	a := &NotificationActivities{Mailer: mailer, Accounts: buildCAccountService(as)}

	err := a.SendExpiryReminder(context.Background(), SendReminderInput{
		AccountID: 5, SubscriptionID: 1, DaysLeft: 7, ExpiresAt: "2026-04-30",
	})
	if err == nil {
		t.Fatal("expected error from mailer failure")
	}
}

// containsStr is a minimal substring check used in test assertions.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
