package app

// In-memory mock implementations of repo interfaces for testing.
// No network, no DB — all state lives in plain maps.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/event"
)

// ── accountStore mock ─────────────────────────────────────────────────────────

type mockAccountStore struct {
	mu       sync.Mutex
	byID     map[int64]*entity.Account
	byEmail  map[string]*entity.Account
	bySub    map[string]*entity.Account
	nextID   int64
	bindings []entity.OAuthBinding
}

func newMockAccountStore() *mockAccountStore {
	return &mockAccountStore{
		byID:   make(map[int64]*entity.Account),
		byEmail: make(map[string]*entity.Account),
		bySub:  make(map[string]*entity.Account),
		nextID: 1,
	}
}

func (m *mockAccountStore) Create(_ context.Context, a *entity.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = m.nextID
	m.nextID++
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	cp := *a
	m.byID[cp.ID] = &cp
	m.byEmail[cp.Email] = &cp
	if cp.ZitadelSub != "" {
		m.bySub[cp.ZitadelSub] = &cp
	}
	return nil
}

func (m *mockAccountStore) Update(_ context.Context, a *entity.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.UpdatedAt = time.Now()
	cp := *a
	m.byID[cp.ID] = &cp
	m.byEmail[cp.Email] = &cp
	if cp.ZitadelSub != "" {
		m.bySub[cp.ZitadelSub] = &cp
	}
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

func (m *mockAccountStore) GetByEmail(_ context.Context, email string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byEmail[email]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
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

func (m *mockAccountStore) GetByLurusID(_ context.Context, lurusID string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.byID {
		if a.LurusID == lurusID {
			cp := *a
			return &cp, nil
		}
	}
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
	var out []*entity.Account
	for _, a := range m.byID {
		cp := *a
		out = append(out, &cp)
	}
	return out, int64(len(out)), nil
}

func (m *mockAccountStore) UpsertOAuthBinding(_ context.Context, b *entity.OAuthBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bindings = append(m.bindings, *b)
	return nil
}

func (m *mockAccountStore) GetByOAuthBinding(_ context.Context, provider, providerID string) (*entity.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.bindings {
		if b.Provider == provider && b.ProviderID == providerID {
			a, ok := m.byID[b.AccountID]
			if !ok {
				return nil, nil
			}
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

// ── walletStore mock ──────────────────────────────────────────────────────────

type mockWalletStore struct {
	mu       sync.Mutex
	wallets  map[int64]*entity.Wallet
	creditErr error // if set, Credit() returns this error
	txs      []entity.WalletTransaction
	orders   map[string]*entity.PaymentOrder
	codes    map[string]*entity.RedemptionCode
	preauths map[int64]*entity.WalletPreAuthorization
	nextWID  int64
	nextPAID int64
}

func newMockWalletStore() *mockWalletStore {
	return &mockWalletStore{
		wallets:  make(map[int64]*entity.Wallet),
		orders:   make(map[string]*entity.PaymentOrder),
		codes:    make(map[string]*entity.RedemptionCode),
		preauths: make(map[int64]*entity.WalletPreAuthorization),
		nextWID:  1,
		nextPAID: 1,
	}
}

func (m *mockWalletStore) GetOrCreate(_ context.Context, accountID int64) (*entity.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.wallets[accountID]; ok {
		cp := *w
		return &cp, nil
	}
	w := &entity.Wallet{ID: m.nextWID, AccountID: accountID}
	m.nextWID++
	m.wallets[accountID] = w
	cp := *w
	return &cp, nil
}

func (m *mockWalletStore) GetByAccountID(_ context.Context, accountID int64) (*entity.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[accountID]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (m *mockWalletStore) Credit(_ context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.creditErr != nil {
		return nil, m.creditErr
	}
	w, ok := m.wallets[accountID]
	if !ok {
		w = &entity.Wallet{ID: m.nextWID, AccountID: accountID}
		m.nextWID++
		m.wallets[accountID] = w
	}
	w.Balance += amount
	if txType == entity.TxTypeTopup {
		w.LifetimeTopup += amount
	}
	tx := entity.WalletTransaction{
		ID: int64(len(m.txs) + 1), WalletID: w.ID, AccountID: accountID,
		Type: txType, Amount: amount, BalanceAfter: w.Balance,
		ProductID: productID, ReferenceType: refType, ReferenceID: refID, Description: desc,
	}
	m.txs = append(m.txs, tx)
	cp := tx
	return &cp, nil
}

func (m *mockWalletStore) Debit(_ context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[accountID]
	if !ok {
		return nil, fmt.Errorf("wallet not found for account %d", accountID)
	}
	if w.Balance-w.Frozen < amount {
		return nil, fmt.Errorf("insufficient available balance: have %.4f (%.4f balance - %.4f frozen), need %.4f",
			w.Balance-w.Frozen, w.Balance, w.Frozen, amount)
	}
	w.Balance -= amount
	w.LifetimeSpend += amount
	tx := entity.WalletTransaction{
		ID: int64(len(m.txs) + 1), WalletID: w.ID, AccountID: accountID,
		Type: txType, Amount: -amount, BalanceAfter: w.Balance,
		ProductID: productID, ReferenceType: refType, ReferenceID: refID, Description: desc,
	}
	m.txs = append(m.txs, tx)
	cp := tx
	return &cp, nil
}

func (m *mockWalletStore) ListTransactions(_ context.Context, accountID int64, _, _ int) ([]entity.WalletTransaction, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.WalletTransaction
	for _, tx := range m.txs {
		if tx.AccountID == accountID {
			out = append(out, tx)
		}
	}
	return out, int64(len(out)), nil
}

func (m *mockWalletStore) CreatePaymentOrder(_ context.Context, o *entity.PaymentOrder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *o
	m.orders[cp.OrderNo] = &cp
	return nil
}

func (m *mockWalletStore) UpdatePaymentOrder(_ context.Context, o *entity.PaymentOrder) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *o
	m.orders[cp.OrderNo] = &cp
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
	rc, ok := m.codes[code]
	if !ok {
		return nil, nil
	}
	cp := *rc
	return &cp, nil
}

func (m *mockWalletStore) UpdateRedemptionCode(_ context.Context, rc *entity.RedemptionCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *rc
	m.codes[cp.Code] = &cp
	return nil
}

func (m *mockWalletStore) ListOrders(_ context.Context, accountID int64, _, _ int) ([]entity.PaymentOrder, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.PaymentOrder
	for _, o := range m.orders {
		if o.AccountID == accountID {
			out = append(out, *o)
		}
	}
	return out, int64(len(out)), nil
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
		return &cp, false, nil // already non-pending, idempotent
	}
	now := time.Now().UTC()
	o.Status = entity.OrderStatusPaid
	o.PaidAt = &now
	cp := *o
	return &cp, true, nil
}

func (m *mockWalletStore) ExpireStalePendingOrders(_ context.Context, maxAge time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	var count int64
	for _, o := range m.orders {
		if o.Status == entity.OrderStatusPending && o.CreatedAt.Before(cutoff) {
			o.Status = "expired"
			count++
		}
	}
	return count, nil
}

func (m *mockWalletStore) RedeemCode(_ context.Context, accountID int64, code string) (*entity.WalletTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc, ok := m.codes[code]
	if !ok {
		return nil, fmt.Errorf("invalid code")
	}
	if rc.ExpiresAt != nil && rc.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("code has expired")
	}
	if rc.UsedCount >= rc.MaxUses {
		return nil, fmt.Errorf("code has reached its usage limit")
	}
	if rc.RewardType != "credits" {
		return nil, fmt.Errorf("unsupported reward type: %s", rc.RewardType)
	}
	w, ok := m.wallets[accountID]
	if !ok {
		w = &entity.Wallet{ID: m.nextWID, AccountID: accountID}
		m.nextWID++
		m.wallets[accountID] = w
	}
	w.Balance += rc.RewardValue
	rc.UsedCount++
	tx := entity.WalletTransaction{
		ID: int64(len(m.txs) + 1), WalletID: w.ID, AccountID: accountID,
		Type: entity.TxTypeRedemption, Amount: rc.RewardValue, BalanceAfter: w.Balance,
		ReferenceType: "redemption_code", ReferenceID: rc.Code,
		Description: fmt.Sprintf("Redeem code %s", rc.Code), ProductID: rc.ProductID,
	}
	m.txs = append(m.txs, tx)
	cp := tx
	return &cp, nil
}

func (m *mockWalletStore) GetPendingOrderByIdempotencyKey(_ context.Context, key string) (*entity.PaymentOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.orders {
		if o.IdempotencyKey == key && o.Status == entity.OrderStatusPending {
			cp := *o
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockWalletStore) CreatePreAuth(_ context.Context, pa *entity.WalletPreAuthorization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[pa.AccountID]
	if !ok {
		return fmt.Errorf("wallet not found for account %d", pa.AccountID)
	}
	if w.Balance-w.Frozen < pa.Amount {
		return fmt.Errorf("insufficient available balance")
	}
	w.Frozen += pa.Amount
	pa.ID = m.nextPAID
	m.nextPAID++
	pa.WalletID = w.ID
	pa.Status = entity.PreAuthStatusActive
	cp := *pa
	m.preauths[cp.ID] = &cp
	return nil
}

func (m *mockWalletStore) GetPreAuthByID(_ context.Context, id int64) (*entity.WalletPreAuthorization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pa, ok := m.preauths[id]
	if !ok {
		return nil, nil
	}
	cp := *pa
	return &cp, nil
}

func (m *mockWalletStore) GetPreAuthByReference(_ context.Context, productID, referenceID string) (*entity.WalletPreAuthorization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pa := range m.preauths {
		if pa.ProductID == productID && pa.ReferenceID == referenceID {
			cp := *pa
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockWalletStore) SettlePreAuth(_ context.Context, id int64, actualAmount float64) (*entity.WalletPreAuthorization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pa, ok := m.preauths[id]
	if !ok {
		return nil, fmt.Errorf("pre-auth %d not found", id)
	}
	if pa.Status != entity.PreAuthStatusActive {
		return nil, fmt.Errorf("pre-auth %d not active", id)
	}
	w, ok := m.wallets[pa.AccountID]
	if !ok {
		return nil, fmt.Errorf("wallet not found")
	}
	now := time.Now().UTC()
	pa.ActualAmount = &actualAmount
	pa.Status = entity.PreAuthStatusSettled
	pa.SettledAt = &now
	w.Frozen -= pa.Amount
	w.Balance -= actualAmount
	w.LifetimeSpend += actualAmount
	cp := *pa
	return &cp, nil
}

func (m *mockWalletStore) ReleasePreAuth(_ context.Context, id int64) (*entity.WalletPreAuthorization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pa, ok := m.preauths[id]
	if !ok {
		return nil, fmt.Errorf("pre-auth %d not found", id)
	}
	if pa.Status != entity.PreAuthStatusActive {
		return nil, fmt.Errorf("pre-auth %d not active", id)
	}
	w, ok := m.wallets[pa.AccountID]
	if !ok {
		return nil, fmt.Errorf("wallet not found")
	}
	pa.Status = entity.PreAuthStatusReleased
	w.Frozen -= pa.Amount
	cp := *pa
	return &cp, nil
}

func (m *mockWalletStore) ExpireStalePreAuths(_ context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var count int64
	for _, pa := range m.preauths {
		if pa.Status == entity.PreAuthStatusActive && pa.ExpiresAt.Before(now) {
			pa.Status = entity.PreAuthStatusExpired
			if w, ok := m.wallets[pa.AccountID]; ok {
				w.Frozen -= pa.Amount
			}
			count++
		}
	}
	return count, nil
}

func (m *mockWalletStore) CountActivePreAuths(_ context.Context, accountID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, pa := range m.preauths {
		if pa.AccountID == accountID && pa.Status == entity.PreAuthStatusActive {
			count++
		}
	}
	return count, nil
}

func (m *mockWalletStore) CountPendingOrders(_ context.Context, accountID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, o := range m.orders {
		if o.AccountID == accountID && o.Status == entity.OrderStatusPending {
			count++
		}
	}
	return count, nil
}

// ── Reconciliation stubs (satisfy interface, not exercised in unit tests) ─────

func (m *mockWalletStore) FindStalePendingOrders(_ context.Context, _ time.Duration) ([]entity.PaymentOrder, error) {
	return nil, nil
}

func (m *mockWalletStore) FindPaidTopupOrdersWithoutCredit(_ context.Context) ([]entity.PaidOrderWithoutCredit, error) {
	return nil, nil
}

func (m *mockWalletStore) CreateReconciliationIssue(_ context.Context, _ *entity.ReconciliationIssue) error {
	return nil
}

func (m *mockWalletStore) ListReconciliationIssues(_ context.Context, _ string, _, _ int) ([]entity.ReconciliationIssue, int64, error) {
	return nil, 0, nil
}

func (m *mockWalletStore) ResolveReconciliationIssue(_ context.Context, _ int64, _, _ string) error {
	return nil
}

// ── vipStore mock ─────────────────────────────────────────────────────────────

type mockVIPStore struct {
	mu      sync.Mutex
	viP     map[int64]*entity.AccountVIP
	configs []entity.VIPLevelConfig
}

func newMockVIPStore(configs []entity.VIPLevelConfig) *mockVIPStore {
	return &mockVIPStore{
		viP:     make(map[int64]*entity.AccountVIP),
		configs: configs,
	}
}

func (m *mockVIPStore) GetOrCreate(_ context.Context, accountID int64) (*entity.AccountVIP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.viP[accountID]
	if !ok {
		v = &entity.AccountVIP{AccountID: accountID}
		m.viP[accountID] = v
	}
	cp := *v
	return &cp, nil
}

func (m *mockVIPStore) Update(_ context.Context, v *entity.AccountVIP) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v.UpdatedAt = time.Now()
	cp := *v
	m.viP[v.AccountID] = &cp
	return nil
}

func (m *mockVIPStore) ListConfigs(_ context.Context) ([]entity.VIPLevelConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.configs, nil
}

// ── subscriptionStore mock ────────────────────────────────────────────────────

type mockSubStore struct {
	mu           sync.Mutex
	subs         map[int64]*entity.Subscription
	entitlements map[string]*entity.AccountEntitlement // key: accountID:productID:key
	nextID       int64
}

func newMockSubStore() *mockSubStore {
	return &mockSubStore{
		subs:         make(map[int64]*entity.Subscription),
		entitlements: make(map[string]*entity.AccountEntitlement),
		nextID:       1,
	}
}

func (m *mockSubStore) Create(_ context.Context, s *entity.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = m.nextID
	m.nextID++
	cp := *s
	m.subs[cp.ID] = &cp
	return nil
}

func (m *mockSubStore) Update(_ context.Context, s *entity.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.subs[cp.ID] = &cp
	return nil
}

func (m *mockSubStore) GetByID(_ context.Context, id int64) (*entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.subs[id]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *mockSubStore) GetActive(_ context.Context, accountID int64, productID string) (*entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subs {
		if s.AccountID == accountID && s.ProductID == productID &&
			(s.Status == entity.SubStatusActive || s.Status == entity.SubStatusGrace || s.Status == entity.SubStatusTrial) {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockSubStore) ListByAccount(_ context.Context, accountID int64) ([]entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Subscription
	for _, s := range m.subs {
		if s.AccountID == accountID {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (m *mockSubStore) UpsertEntitlement(_ context.Context, e *entity.AccountEntitlement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := fmt.Sprintf("%d:%s:%s", e.AccountID, e.ProductID, e.Key)
	cp := *e
	m.entitlements[k] = &cp
	return nil
}

func (m *mockSubStore) GetEntitlements(_ context.Context, accountID int64, productID string) ([]entity.AccountEntitlement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := fmt.Sprintf("%d:%s:", accountID, productID)
	var out []entity.AccountEntitlement
	for k, e := range m.entitlements {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (m *mockSubStore) ListDueForRenewal(_ context.Context) ([]entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(24 * time.Hour)
	var out []entity.Subscription
	for _, s := range m.subs {
		if !s.AutoRenew || s.Status != entity.SubStatusActive {
			continue
		}
		if s.ExpiresAt == nil || s.ExpiresAt.Before(now) || s.ExpiresAt.After(cutoff) {
			continue
		}
		if s.RenewalAttempts >= 3 {
			continue
		}
		if s.NextRenewalAt != nil && s.NextRenewalAt.After(now) {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

func (m *mockSubStore) UpdateRenewalState(_ context.Context, subID int64, attempts int, nextAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.subs[subID]
	if !ok {
		return fmt.Errorf("subscription %d not found", subID)
	}
	s.RenewalAttempts = attempts
	s.NextRenewalAt = nextAt
	return nil
}

func (m *mockSubStore) ListActiveExpired(_ context.Context) ([]entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []entity.Subscription
	for _, s := range m.subs {
		if s.Status == entity.SubStatusActive && s.ExpiresAt != nil && s.ExpiresAt.Before(now) {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (m *mockSubStore) ListGraceExpired(_ context.Context) ([]entity.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []entity.Subscription
	for _, s := range m.subs {
		if s.Status == entity.SubStatusGrace && s.GraceUntil != nil && s.GraceUntil.Before(now) {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (m *mockSubStore) DeleteEntitlements(_ context.Context, accountID int64, productID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := fmt.Sprintf("%d:%s:", accountID, productID)
	for k := range m.entitlements {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(m.entitlements, k)
		}
	}
	return nil
}

// ── invoiceStore mock ─────────────────────────────────────────────────────────

type mockInvoiceStore struct {
	mu        sync.Mutex
	byID      map[int64]*entity.Invoice
	byOrderNo map[string]*entity.Invoice
	byNo      map[string]*entity.Invoice
	nextID    int64
}

func newMockInvoiceStore() *mockInvoiceStore {
	return &mockInvoiceStore{
		byID:      make(map[int64]*entity.Invoice),
		byOrderNo: make(map[string]*entity.Invoice),
		byNo:      make(map[string]*entity.Invoice),
		nextID:    1,
	}
}

func (m *mockInvoiceStore) Create(_ context.Context, inv *entity.Invoice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv.ID = m.nextID
	m.nextID++
	inv.CreatedAt = time.Now()
	inv.UpdatedAt = time.Now()
	cp := *inv
	m.byID[cp.ID] = &cp
	m.byOrderNo[cp.OrderNo] = &cp
	m.byNo[cp.InvoiceNo] = &cp
	return nil
}

func (m *mockInvoiceStore) GetByOrderNo(_ context.Context, orderNo string) (*entity.Invoice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.byOrderNo[orderNo]
	if !ok {
		return nil, nil
	}
	cp := *inv
	return &cp, nil
}

func (m *mockInvoiceStore) GetByInvoiceNo(_ context.Context, invoiceNo string) (*entity.Invoice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv, ok := m.byNo[invoiceNo]
	if !ok {
		return nil, nil
	}
	cp := *inv
	return &cp, nil
}

func (m *mockInvoiceStore) ListByAccount(_ context.Context, accountID int64, _, _ int) ([]entity.Invoice, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Invoice
	for _, inv := range m.byID {
		if inv.AccountID == accountID {
			out = append(out, *inv)
		}
	}
	return out, int64(len(out)), nil
}

func (m *mockInvoiceStore) AdminList(_ context.Context, filterAccountID int64, _, _ int) ([]entity.Invoice, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Invoice
	for _, inv := range m.byID {
		if filterAccountID == 0 || inv.AccountID == filterAccountID {
			out = append(out, *inv)
		}
	}
	return out, int64(len(out)), nil
}

// ── refundStore mock ──────────────────────────────────────────────────────────

type mockRefundStore struct {
	mu     sync.Mutex
	byID   map[int64]*entity.Refund
	byNo   map[string]*entity.Refund
	nextID int64
}

func newMockRefundStore() *mockRefundStore {
	return &mockRefundStore{
		byID:   make(map[int64]*entity.Refund),
		byNo:   make(map[string]*entity.Refund),
		nextID: 1,
	}
}

func (m *mockRefundStore) Create(_ context.Context, r *entity.Refund) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ID = m.nextID
	m.nextID++
	r.CreatedAt = time.Now()
	r.UpdatedAt = time.Now()
	cp := *r
	m.byID[cp.ID] = &cp
	m.byNo[cp.RefundNo] = &cp
	return nil
}

func (m *mockRefundStore) GetByRefundNo(_ context.Context, refundNo string) (*entity.Refund, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.byNo[refundNo]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *mockRefundStore) GetPendingByOrderNo(_ context.Context, orderNo string) (*entity.Refund, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.byID {
		if r.OrderNo == orderNo &&
			(r.Status == entity.RefundStatusPending || r.Status == entity.RefundStatusApproved) {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockRefundStore) UpdateStatus(_ context.Context, refundNo, fromStatus, toStatus, reviewNote, reviewedBy string, reviewedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.byNo[refundNo]
	if !ok {
		return fmt.Errorf("refund %s not found", refundNo)
	}
	if string(r.Status) != fromStatus {
		return fmt.Errorf("refund %s: transition %s->%s failed (concurrent or wrong state)", refundNo, fromStatus, toStatus)
	}
	r.Status = entity.RefundStatus(toStatus)
	r.ReviewNote = reviewNote
	r.ReviewedBy = reviewedBy
	r.ReviewedAt = reviewedAt
	r.UpdatedAt = time.Now()
	return nil
}

func (m *mockRefundStore) MarkCompleted(_ context.Context, refundNo string, completedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.byNo[refundNo]
	if !ok {
		return fmt.Errorf("refund %s not found", refundNo)
	}
	r.Status = entity.RefundStatusCompleted
	r.CompletedAt = &completedAt
	r.UpdatedAt = time.Now()
	return nil
}

func (m *mockRefundStore) ListByAccount(_ context.Context, accountID int64, _, _ int) ([]entity.Refund, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Refund
	for _, r := range m.byID {
		if r.AccountID == accountID {
			out = append(out, *r)
		}
	}
	return out, int64(len(out)), nil
}

// ── eventOutbox mock ─────────────────────────────────────────────────────────

type mockEventOutbox struct {
	mu     sync.Mutex
	events []*event.IdentityEvent
}

func (m *mockEventOutbox) Insert(_ context.Context, ev *event.IdentityEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

// ── redemptionCodeStore mock ──────────────────────────────────────────────────

type mockRedemptionCodeStore struct {
	mu    sync.Mutex
	codes []entity.RedemptionCode
	err   error // if non-nil, BulkCreate returns this error
}

func newMockRedemptionCodeStore() *mockRedemptionCodeStore {
	return &mockRedemptionCodeStore{}
}

func (m *mockRedemptionCodeStore) BulkCreate(_ context.Context, codes []entity.RedemptionCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.codes = append(m.codes, codes...)
	return nil
}

// ── planStore mock ────────────────────────────────────────────────────────────

type mockPlanStore struct {
	mu       sync.Mutex
	products map[string]*entity.Product
	plans    map[int64]*entity.ProductPlan
	nextPID  int64
}

func newMockPlanStore() *mockPlanStore {
	return &mockPlanStore{
		products: make(map[string]*entity.Product),
		plans:    make(map[int64]*entity.ProductPlan),
		nextPID:  1,
	}
}

func (m *mockPlanStore) GetByID(_ context.Context, id string) (*entity.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.products[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *mockPlanStore) ListActive(_ context.Context) ([]entity.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Product
	for _, p := range m.products {
		if p.Status == 1 {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *mockPlanStore) Create(_ context.Context, p *entity.Product) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.products[cp.ID] = &cp
	return nil
}

func (m *mockPlanStore) Update(_ context.Context, p *entity.Product) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.products[cp.ID] = &cp
	return nil
}

func (m *mockPlanStore) GetPlanByID(_ context.Context, id int64) (*entity.ProductPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *mockPlanStore) ListPlans(_ context.Context, productID string) ([]entity.ProductPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.ProductPlan
	for _, p := range m.plans {
		if p.ProductID == productID && p.Status == 1 {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *mockPlanStore) CreatePlan(_ context.Context, p *entity.ProductPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.ID = m.nextPID
	m.nextPID++
	cp := *p
	m.plans[cp.ID] = &cp
	return nil
}

func (m *mockPlanStore) UpdatePlan(_ context.Context, p *entity.ProductPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.plans[cp.ID] = &cp
	return nil
}

// ── entitlementCache mock ─────────────────────────────────────────────────────

type mockCache struct {
	mu   sync.Mutex
	data map[string]map[string]string
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]map[string]string)}
}

func (m *mockCache) Get(_ context.Context, accountID int64, productID string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := fmt.Sprintf("%d:%s", accountID, productID)
	v, ok := m.data[k]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *mockCache) Set(_ context.Context, accountID int64, productID string, em map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[fmt.Sprintf("%d:%s", accountID, productID)] = em
	return nil
}

func (m *mockCache) Invalidate(_ context.Context, accountID int64, productID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, fmt.Sprintf("%d:%s", accountID, productID))
	return nil
}

// ── checkinStore mock ─────────────────────────────────────────────────────────

type mockCheckinStore struct {
	mu      sync.Mutex
	checkins []entity.Checkin
	nextID  int64
	createErr error // if non-nil, Create returns this error
}

func newMockCheckinStore() *mockCheckinStore {
	return &mockCheckinStore{nextID: 1}
}

func (m *mockCheckinStore) Create(_ context.Context, c *entity.Checkin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	c.ID = m.nextID
	m.nextID++
	cp := *c
	m.checkins = append(m.checkins, cp)
	return nil
}

func (m *mockCheckinStore) GetByAccountAndDate(_ context.Context, accountID int64, date string) (*entity.Checkin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.checkins {
		if c.AccountID == accountID && c.CheckinDate == date {
			cp := c
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *mockCheckinStore) ListByAccountAndMonth(_ context.Context, accountID int64, yearMonth string) ([]entity.Checkin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Checkin
	for _, c := range m.checkins {
		if c.AccountID == accountID && len(c.CheckinDate) >= 7 && c.CheckinDate[:7] == yearMonth {
			out = append(out, c)
		}
	}
	return out, nil
}

// CountConsecutive counts consecutive days ending on 'date' (inclusive).
func (m *mockCheckinStore) CountConsecutive(_ context.Context, accountID int64, date string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if date == "" {
		return 0, nil
	}
	// Build a set of dates checked in.
	dateSet := make(map[string]bool)
	for _, c := range m.checkins {
		if c.AccountID == accountID {
			dateSet[c.CheckinDate] = true
		}
	}
	// Count backward from 'date'.
	count := 0
	cur := date
	for dateSet[cur] {
		count++
		// Go back one day.
		t, err := time.Parse("2006-01-02", cur)
		if err != nil {
			break
		}
		cur = t.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return count, nil
}
