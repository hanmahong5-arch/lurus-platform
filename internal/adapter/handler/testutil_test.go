package handler

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------- mock account store ----------

type mockAccountStore struct {
	mu     sync.Mutex
	byID   map[int64]*entity.Account
	bySub  map[string]*entity.Account
	nextID int64
}

func newMockAccountStore() *mockAccountStore {
	return &mockAccountStore{byID: make(map[int64]*entity.Account), bySub: make(map[string]*entity.Account), nextID: 1}
}

func (m *mockAccountStore) Create(_ context.Context, a *entity.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = m.nextID
	m.nextID++
	cp := *a
	m.byID[a.ID] = &cp
	if a.ZitadelSub != "" {
		m.bySub[a.ZitadelSub] = &cp
	}
	return nil
}

func (m *mockAccountStore) Update(_ context.Context, a *entity.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *a
	m.byID[a.ID] = &cp
	return nil
}

func (m *mockAccountStore) GetByID(_ context.Context, id int64) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *mockAccountStore) GetByEmail(_ context.Context, _ string) (*entity.Account, error) {
	return nil, nil
}

func (m *mockAccountStore) GetByZitadelSub(_ context.Context, sub string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.bySub[sub]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *mockAccountStore) GetByLurusID(_ context.Context, _ string) (*entity.Account, error) {
	return nil, nil
}

func (m *mockAccountStore) GetByAffCode(_ context.Context, code string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.byID {
		if a.AffCode == code {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockAccountStore) GetByUsername(_ context.Context, username string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.byID {
		if strings.EqualFold(a.Username, username) && username != "" {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockAccountStore) GetByPhone(_ context.Context, phone string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.byID {
		if a.Phone == phone && phone != "" {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockAccountStore) List(_ context.Context, _ string, _, _ int) ([]*entity.Account, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []*entity.Account
	for _, a := range m.byID {
		cp := *a
		all = append(all, &cp)
	}
	return all, int64(len(all)), nil
}

func (m *mockAccountStore) UpsertOAuthBinding(_ context.Context, _ *entity.OAuthBinding) error {
	return nil
}

func (m *mockAccountStore) GetByOAuthBinding(_ context.Context, _, _ string) (*entity.Account, error) {
	return nil, nil
}

// seed inserts a test account and returns a copy.
func (m *mockAccountStore) seed(a entity.Account) *entity.Account {
	_ = m.Create(context.Background(), &a)
	cp := a
	return &cp
}

// ---------- mock wallet store ----------

type mockWalletStore struct {
	mu     sync.Mutex
	byAcct map[int64]*entity.Wallet
	orders map[string]*entity.PaymentOrder
	codes  map[string]*entity.RedemptionCode
}

func newMockWalletStore() *mockWalletStore {
	return &mockWalletStore{
		byAcct: make(map[int64]*entity.Wallet),
		orders: make(map[string]*entity.PaymentOrder),
		codes:  make(map[string]*entity.RedemptionCode),
	}
}

func (m *mockWalletStore) GetOrCreate(_ context.Context, accountID int64) (*entity.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.byAcct[accountID]
	if !ok {
		w = &entity.Wallet{ID: accountID, AccountID: accountID}
		m.byAcct[accountID] = w
	}
	cp := *w
	return &cp, nil
}

func (m *mockWalletStore) GetByAccountID(_ context.Context, accountID int64) (*entity.Wallet, error) {
	return m.GetOrCreate(context.Background(), accountID)
}

func (m *mockWalletStore) Credit(_ context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.byAcct[accountID]
	if w == nil {
		w = &entity.Wallet{ID: accountID, AccountID: accountID}
		m.byAcct[accountID] = w
	}
	w.Balance += amount
	return &entity.WalletTransaction{Amount: amount, Type: txType, BalanceAfter: w.Balance}, nil
}

func (m *mockWalletStore) Debit(_ context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.byAcct[accountID]
	if w == nil {
		w = &entity.Wallet{ID: accountID, AccountID: accountID}
		m.byAcct[accountID] = w
	}
	if w.Balance < amount {
		return nil, fmt.Errorf("insufficient balance: have %.4f, need %.4f", w.Balance, amount)
	}
	w.Balance -= amount
	return &entity.WalletTransaction{Amount: -amount, Type: txType, BalanceAfter: w.Balance}, nil
}

func (m *mockWalletStore) ListTransactions(_ context.Context, _ int64, _, _ int) ([]entity.WalletTransaction, int64, error) {
	return nil, 0, nil
}

func (m *mockWalletStore) CreatePaymentOrder(_ context.Context, o *entity.PaymentOrder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders[o.OrderNo] = o
	return nil
}

func (m *mockWalletStore) UpdatePaymentOrder(_ context.Context, o *entity.PaymentOrder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders[o.OrderNo] = o
	return nil
}

func (m *mockWalletStore) GetPaymentOrderByNo(_ context.Context, orderNo string) (*entity.PaymentOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[orderNo]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (m *mockWalletStore) GetRedemptionCode(_ context.Context, code string) (*entity.RedemptionCode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codes[code]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (m *mockWalletStore) UpdateRedemptionCode(_ context.Context, rc *entity.RedemptionCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[rc.Code] = rc
	return nil
}

func (m *mockWalletStore) ListOrders(_ context.Context, _ int64, _, _ int) ([]entity.PaymentOrder, int64, error) {
	return nil, 0, nil
}

func (m *mockWalletStore) MarkPaymentOrderPaid(_ context.Context, orderNo string) (*entity.PaymentOrder, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[orderNo]
	if !ok {
		return nil, false, nil
	}
	if o.Status != entity.OrderStatusPending {
		cp := *o
		return &cp, false, nil
	}
	now := time.Now().UTC()
	o.Status = entity.OrderStatusPaid
	o.PaidAt = &now
	cp := *o
	return &cp, true, nil
}

func (m *mockWalletStore) RedeemCode(_ context.Context, _ int64, code string) (*entity.WalletTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc, ok := m.codes[code]
	if !ok {
		return nil, fmt.Errorf("invalid code")
	}
	_ = rc
	return &entity.WalletTransaction{Amount: 0}, nil
}

func (m *mockWalletStore) ExpireStalePendingOrders(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// ---------- mock vip store ----------

type mockVIPStore struct {
	mu   sync.Mutex
	data map[int64]*entity.AccountVIP
}

func newMockVIPStore() *mockVIPStore {
	return &mockVIPStore{data: make(map[int64]*entity.AccountVIP)}
}

func (m *mockVIPStore) GetOrCreate(_ context.Context, accountID int64) (*entity.AccountVIP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[accountID]
	if !ok {
		v = &entity.AccountVIP{AccountID: accountID}
		m.data[accountID] = v
	}
	cp := *v
	return &cp, nil
}

func (m *mockVIPStore) Update(_ context.Context, v *entity.AccountVIP) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *v
	m.data[v.AccountID] = &cp
	return nil
}

func (m *mockVIPStore) ListConfigs(_ context.Context) ([]entity.VIPLevelConfig, error) {
	return nil, nil
}

// ---------- mock subscription store ----------

type mockSubStore struct {
	mu     sync.Mutex
	active map[string]*entity.Subscription // key: "accountID:productID"
	byAcct map[int64][]entity.Subscription
}

func newMockSubStore() *mockSubStore {
	return &mockSubStore{
		active: make(map[string]*entity.Subscription),
		byAcct: make(map[int64][]entity.Subscription),
	}
}

func (m *mockSubStore) Create(_ context.Context, s *entity.Subscription) error                { return nil }
func (m *mockSubStore) Update(_ context.Context, s *entity.Subscription) error                { return nil }
func (m *mockSubStore) GetByID(_ context.Context, _ int64) (*entity.Subscription, error)      { return nil, nil }
func (m *mockSubStore) ListActiveExpired(_ context.Context) ([]entity.Subscription, error)    { return nil, nil }
func (m *mockSubStore) ListGraceExpired(_ context.Context) ([]entity.Subscription, error)     { return nil, nil }
func (m *mockSubStore) ListDueForRenewal(_ context.Context) ([]entity.Subscription, error)    { return nil, nil }
func (m *mockSubStore) UpdateRenewalState(_ context.Context, _ int64, _ int, _ *time.Time) error { return nil }
func (m *mockSubStore) UpsertEntitlement(_ context.Context, _ *entity.AccountEntitlement) error  { return nil }
func (m *mockSubStore) DeleteEntitlements(_ context.Context, _ int64, _ string) error            { return nil }

func (m *mockSubStore) GetActive(_ context.Context, accountID int64, productID string) (*entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := subKey(accountID, productID)
	s, ok := m.active[key]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *mockSubStore) ListByAccount(_ context.Context, accountID int64) ([]entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byAcct[accountID], nil
}

func (m *mockSubStore) GetEntitlements(_ context.Context, accountID int64, productID string) ([]entity.AccountEntitlement, error) {
	return nil, nil
}

func subKey(accountID int64, productID string) string {
	return fmt.Sprintf("%d:%s", accountID, productID)
}

// ---------- mock plan store ----------

type mockPlanStore struct {
	products map[string]*entity.Product
	plans    map[int64]*entity.ProductPlan
}

func newMockPlanStore() *mockPlanStore {
	return &mockPlanStore{
		products: make(map[string]*entity.Product),
		plans:    make(map[int64]*entity.ProductPlan),
	}
}

func (m *mockPlanStore) GetByID(_ context.Context, id string) (*entity.Product, error) {
	p, ok := m.products[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (m *mockPlanStore) ListActive(_ context.Context) ([]entity.Product, error) {
	var out []entity.Product
	for _, p := range m.products {
		out = append(out, *p)
	}
	return out, nil
}

func (m *mockPlanStore) ListPlans(_ context.Context, productID string) ([]entity.ProductPlan, error) {
	var out []entity.ProductPlan
	for _, p := range m.plans {
		if p.ProductID == productID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *mockPlanStore) GetPlanByID(_ context.Context, id int64) (*entity.ProductPlan, error) {
	p, ok := m.plans[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (m *mockPlanStore) Create(_ context.Context, p *entity.Product) error     { return nil }
func (m *mockPlanStore) Update(_ context.Context, p *entity.Product) error     { return nil }
func (m *mockPlanStore) CreatePlan(_ context.Context, p *entity.ProductPlan) error { return nil }
func (m *mockPlanStore) UpdatePlan(_ context.Context, p *entity.ProductPlan) error { return nil }

// ---------- mock entitlement cache ----------

type mockCache struct {
	data map[string]map[string]string
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]map[string]string)}
}

func (m *mockCache) Get(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return nil, nil // always miss
}

func (m *mockCache) Set(_ context.Context, _ int64, _ string, _ map[string]string) error {
	return nil
}

func (m *mockCache) Invalidate(_ context.Context, _ int64, _ string) error {
	return nil
}

// ---------- mock invoice store ----------

type mockInvoiceStore struct{}

func (m *mockInvoiceStore) Create(_ context.Context, _ *entity.Invoice) error                                    { return nil }
func (m *mockInvoiceStore) GetByOrderNo(_ context.Context, _ string) (*entity.Invoice, error)                    { return nil, nil }
func (m *mockInvoiceStore) GetByInvoiceNo(_ context.Context, _ string) (*entity.Invoice, error)                  { return nil, nil }
func (m *mockInvoiceStore) ListByAccount(_ context.Context, _ int64, _, _ int) ([]entity.Invoice, int64, error)  { return nil, 0, nil }
func (m *mockInvoiceStore) AdminList(_ context.Context, _ int64, _, _ int) ([]entity.Invoice, int64, error)      { return nil, 0, nil }

// ---------- mock refund store ----------

type mockRefundStore struct{}

func (m *mockRefundStore) Create(_ context.Context, _ *entity.Refund) error                                       { return nil }
func (m *mockRefundStore) GetByRefundNo(_ context.Context, _ string) (*entity.Refund, error)                      { return nil, nil }
func (m *mockRefundStore) GetPendingByOrderNo(_ context.Context, _ string) (*entity.Refund, error)                { return nil, nil }
func (m *mockRefundStore) UpdateStatus(_ context.Context, _, _, _, _, _ string, _ *time.Time) error               { return nil }
func (m *mockRefundStore) MarkCompleted(_ context.Context, _ string, _ time.Time) error                           { return nil }
func (m *mockRefundStore) ListByAccount(_ context.Context, _ int64, _, _ int) ([]entity.Refund, int64, error)     { return nil, 0, nil }

// ---------- mock redemption code store ----------

type mockRedemptionCodeStore struct{}

func (m *mockRedemptionCodeStore) BulkCreate(_ context.Context, _ []entity.RedemptionCode) error { return nil }

// ---------- mock publisher ----------

type mockPublisher struct{}

func (m *mockPublisher) Publish(_ context.Context, _ *event.IdentityEvent) error { return nil }

// ---------- service builders ----------

func makeAccountService() *app.AccountService {
	return app.NewAccountService(newMockAccountStore(), newMockWalletStore(), newMockVIPStore())
}

func makeAccountServiceWith(as *mockAccountStore) *app.AccountService {
	return app.NewAccountService(as, newMockWalletStore(), newMockVIPStore())
}

func makeVIPService() *app.VIPService {
	return app.NewVIPService(newMockVIPStore(), newMockWalletStore())
}

func makeSubService() *app.SubscriptionService {
	return app.NewSubscriptionService(newMockSubStore(), newMockPlanStore(), makeEntitlementService(), 3)
}

func makeEntitlementService() *app.EntitlementService {
	return app.NewEntitlementService(newMockSubStore(), newMockPlanStore(), newMockCache())
}

func makeWalletService() *app.WalletService {
	return app.NewWalletService(newMockWalletStore(), makeVIPService())
}

func makeProductService() *app.ProductService {
	return app.NewProductService(newMockPlanStore())
}

func makeInvoiceService() *app.InvoiceService {
	return app.NewInvoiceService(&mockInvoiceStore{}, newMockWalletStore())
}

func makeRefundService() *app.RefundService {
	return app.NewRefundService(&mockRefundStore{}, newMockWalletStore(), &mockPublisher{}, nil)
}

func makeReferralService() *app.ReferralService {
	return app.NewReferralServiceWithCodes(newMockAccountStore(), newMockWalletStore(), &mockRedemptionCodeStore{})
}

// ---------- mock overview cache ----------

type mockOverviewCacheH struct{}

func (m *mockOverviewCacheH) Get(_ context.Context, _ int64, _ string) ([]byte, error) {
	return nil, nil
}
func (m *mockOverviewCacheH) Set(_ context.Context, _ int64, _ string, _ []byte) error {
	return nil
}
func (m *mockOverviewCacheH) Invalidate(_ context.Context, _ int64, _ string) error {
	return nil
}

// ---------- overview service builder ----------

func makeOverviewServiceH() *app.OverviewService {
	return app.NewOverviewService(
		newMockAccountStore(),
		makeVIPService(),
		newMockWalletStore(),
		makeSubService(),
		newMockPlanStore(),
		&mockOverviewCacheH{},
	)
}

func makeOverviewServiceWithAccounts(as *mockAccountStore) *app.OverviewService {
	return app.NewOverviewService(
		as,
		makeVIPService(),
		newMockWalletStore(),
		makeSubService(),
		newMockPlanStore(),
		&mockOverviewCacheH{},
	)
}

// ---------- test router helper ----------

func testRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	return r
}

// withAccountID returns middleware that injects account_id into context.
func withAccountID(id int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("account_id", id)
		c.Next()
	}
}

// ---------- error-injection stores for handler tests ----------

// errWalletH overrides wallet store methods to return errors, enabling handler error-path tests.
type errWalletH struct {
	mockWalletStore
}

// GetByAccountID fails unconditionally.
func (s *errWalletH) GetByAccountID(_ context.Context, _ int64) (*entity.Wallet, error) {
	return nil, fmt.Errorf("db error")
}

// GetOrCreate also fails so WalletService.GetWallet errors regardless of which method it calls.
func (s *errWalletH) GetOrCreate(_ context.Context, _ int64) (*entity.Wallet, error) {
	return nil, fmt.Errorf("db error")
}

// ListOrders fails to test the ListOrders error path.
func (s *errWalletH) ListOrders(_ context.Context, _ int64, _, _ int) ([]entity.PaymentOrder, int64, error) {
	return nil, 0, fmt.Errorf("db error")
}

// errSubStoreH overrides ListByAccount to return errors.
type errSubStoreH struct {
	mockSubStore
}

func (s *errSubStoreH) ListByAccount(_ context.Context, _ int64) ([]entity.Subscription, error) {
	return nil, fmt.Errorf("db error")
}

// emailAwareAccountStore extends mockAccountStore with a working GetByEmail lookup.
type emailAwareAccountStore struct {
	mockAccountStore
	byEmail map[string]*entity.Account
}

func newEmailAwareAccountStore() *emailAwareAccountStore {
	return &emailAwareAccountStore{
		mockAccountStore: *newMockAccountStore(),
		byEmail:          make(map[string]*entity.Account),
	}
}

func (s *emailAwareAccountStore) GetByEmail(_ context.Context, email string) (*entity.Account, error) {
	a, ok := s.byEmail[email]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (s *emailAwareAccountStore) seedEmail(a entity.Account) *entity.Account {
	cp := s.seed(a)
	if cp.Email != "" {
		s.byEmail[cp.Email] = cp
	}
	return cp
}

// errEmailAccountStore returns an error for GetByEmail.
type errEmailAccountStore struct {
	mockAccountStore
}

func (s *errEmailAccountStore) GetByEmail(_ context.Context, _ string) (*entity.Account, error) {
	return nil, fmt.Errorf("db error")
}

// errAccountStoreH returns an error for GetByID (used to trigger overview/account error paths).
type errAccountStoreH struct {
	mockAccountStore
}

func (s *errAccountStoreH) GetByID(_ context.Context, _ int64) (*entity.Account, error) {
	return nil, fmt.Errorf("db error")
}

// errEntSubStore returns an error for GetEntitlements.
type errEntSubStore struct {
	mockSubStore
}

func (s *errEntSubStore) GetEntitlements(_ context.Context, _ int64, _ string) ([]entity.AccountEntitlement, error) {
	return nil, fmt.Errorf("db error")
}

// errGetActiveSubStore returns an error for GetActive.
type errGetActiveSubStore struct {
	mockSubStore
}

func (s *errGetActiveSubStore) GetActive(_ context.Context, _ int64, _ string) (*entity.Subscription, error) {
	return nil, fmt.Errorf("db error")
}

// errGetWalletH allows Credit but fails on GetOrCreate (used to test GetWallet error paths).
// WalletService.GetWallet calls GetOrCreate; Credit uses the store's Credit method directly.
type errGetWalletH struct {
	mockWalletStore
}

func (s *errGetWalletH) GetOrCreate(_ context.Context, _ int64) (*entity.Wallet, error) {
	return nil, fmt.Errorf("db error")
}

func (s *errGetWalletH) GetByAccountID(_ context.Context, _ int64) (*entity.Wallet, error) {
	return nil, fmt.Errorf("db error")
}

// errOAuthBindingStoreH returns an error from GetByOAuthBinding.
type errOAuthBindingStoreH struct{ mockAccountStore }

func (s *errOAuthBindingStoreH) GetByOAuthBinding(_ context.Context, _, _ string) (*entity.Account, error) {
	return nil, fmt.Errorf("db error")
}

// oauthAwareAccountStore returns a seeded account for GetByOAuthBinding lookups.
type oauthAwareAccountStore struct {
	mockAccountStore
	oauthAccount *entity.Account
}

func (s *oauthAwareAccountStore) GetByOAuthBinding(_ context.Context, _, _ string) (*entity.Account, error) {
	if s.oauthAccount == nil {
		return nil, nil
	}
	cp := *s.oauthAccount
	return &cp, nil
}
