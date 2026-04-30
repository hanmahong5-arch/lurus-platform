package newapi_sync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/newapi"
)

// fakeDeduper is a minimal in-memory Deduper for unit tests. It mimics
// the behaviour of idempotency.WebhookDeduper without needing miniredis.
//
// Behaviour switches let tests target each branch:
//
//	emptyEventErr    — return ErrEmptyEventID for ""
//	redisErr         — simulate fail-closed: ErrRedisUnavailable
//	emitDuplicate    — second TryProcess for same id returns ErrAlreadyProcessed
type fakeDeduper struct {
	mu               sync.Mutex
	seen             map[string]bool
	emitDuplicate    bool
	redisErr         bool
	tryCalls         int
	emptyEventReject bool
}

func (f *fakeDeduper) TryProcess(_ context.Context, eventID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tryCalls++
	if eventID == "" && f.emptyEventReject {
		return idempotency.ErrEmptyEventID
	}
	if f.redisErr {
		return idempotency.ErrRedisUnavailable
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	if f.emitDuplicate && f.seen[eventID] {
		return idempotency.ErrAlreadyProcessed
	}
	f.seen[eventID] = true
	return nil
}

// fakeClient 是 NewAPIClient 的内存实现，记录调用次数 + 模拟 user 库。
type fakeClient struct {
	mu              sync.Mutex
	users           map[string]int // username -> id
	quotas          map[int]int64  // id -> quota (for IncrementUserQuota)
	apiKeys         []apiKeyEntry
	nextID          int
	nextKey         int
	createCalls     int
	findCalls       int
	incCalls        int
	createUserError error
	findError       error
	incError        error
	ensureKeyError  error
}

func newFakeClient() *fakeClient {
	return &fakeClient{users: map[string]int{}, quotas: map[int]int64{}, nextID: 1000}
}

func (f *fakeClient) IncrementUserQuota(_ context.Context, id int, delta int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.incCalls++
	if f.incError != nil {
		return 0, f.incError
	}
	f.quotas[id] += delta
	return f.quotas[id], nil
}

// fakeAPIKeys: per (userID, name) → APIKey. EnsureUserAPIKey is idempotent
// so we record the issued key on first call and replay on subsequent ones.
type apiKeyEntry struct {
	userID int
	name   string
	key    *newapi.APIKey
}

func (f *fakeClient) EnsureUserAPIKey(_ context.Context, userID int, name string) (*newapi.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ensureKeyError != nil {
		return nil, f.ensureKeyError
	}
	for _, e := range f.apiKeys {
		if e.userID == userID && e.name == name {
			cp := *e.key
			return &cp, nil
		}
	}
	f.nextKey++
	key := &newapi.APIKey{
		ID:             f.nextKey,
		UserID:         userID,
		Name:           name,
		Key:            "sk-fake-" + strings.ToLower(name),
		UnlimitedQuota: true,
	}
	f.apiKeys = append(f.apiKeys, apiKeyEntry{userID, name, key})
	cp := *key
	return &cp, nil
}

func (f *fakeClient) CreateUser(_ context.Context, username, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if f.createUserError != nil {
		return f.createUserError
	}
	if _, exists := f.users[username]; exists {
		return errors.New("username already exists")
	}
	f.nextID++
	f.users[username] = f.nextID
	return nil
}

func (f *fakeClient) FindUserByUsername(_ context.Context, username string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findCalls++
	if f.findError != nil {
		return 0, f.findError
	}
	id, ok := f.users[username]
	if !ok {
		return 0, newapi.ErrUserNotFound
	}
	return id, nil
}

// fakeStore captures SetNewAPIUserID calls.
type fakeStore struct {
	mu        sync.Mutex
	calls     []struct {
		AccountID    int64
		NewAPIUserID int
	}
	setError error
	// accounts indexed by id, returned by GetByID. nil means "not found".
	accounts map[int64]*entity.Account
}

func (s *fakeStore) SetNewAPIUserID(_ context.Context, accountID int64, newapiUserID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setError != nil {
		return s.setError
	}
	s.calls = append(s.calls, struct {
		AccountID    int64
		NewAPIUserID int
	}{accountID, newapiUserID})
	if s.accounts != nil {
		if a, ok := s.accounts[accountID]; ok {
			a.NewAPIUserID = &newapiUserID
		}
	}
	return nil
}

func (s *fakeStore) GetByID(_ context.Context, id int64) (*entity.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounts == nil {
		return nil, nil
	}
	a, ok := s.accounts[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

// ListWithoutNewAPIUser — used by ReconcileTick. Default fake returns
// orphans from the in-memory map; tests that need error injection
// embed this fake (see reconcileFakeStore).
func (s *fakeStore) ListWithoutNewAPIUser(_ context.Context, limit int) ([]*entity.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entity.Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		if a.NewAPIUserID == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func TestNew_NilDeps(t *testing.T) {
	if New(nil, &fakeStore{}) != nil {
		t.Error("expected nil when client is nil")
	}
	if New(newFakeClient(), nil) != nil {
		t.Error("expected nil when store is nil")
	}
	if New(newFakeClient(), &fakeStore{}) == nil {
		t.Error("expected non-nil when both deps present")
	}
}

func TestOnAccountCreated_HappyPath_CreateAndPersist(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{}
	m := New(c, s)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 42, DisplayName: "Alice"})
	if err != nil {
		t.Fatalf("OnAccountCreated: %v", err)
	}
	if c.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1", c.createCalls)
	}
	// We expect 2 finds: the initial NotFound + the post-create lookup.
	if c.findCalls != 2 {
		t.Errorf("findCalls = %d, want 2", c.findCalls)
	}
	if len(s.calls) != 1 {
		t.Fatalf("expected 1 SetNewAPIUserID call, got %d", len(s.calls))
	}
	if s.calls[0].AccountID != 42 {
		t.Errorf("accountID = %d, want 42", s.calls[0].AccountID)
	}
	// The faked nextID was 1000 → first create increments to 1001.
	if s.calls[0].NewAPIUserID != 1001 {
		t.Errorf("newapiUserID = %d, want 1001", s.calls[0].NewAPIUserID)
	}
}

func TestOnAccountCreated_UsernameConvention(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{}
	m := New(c, s)
	_ = m.OnAccountCreated(context.Background(), &entity.Account{ID: 7})

	if _, ok := c.users["lurus_7"]; !ok {
		t.Errorf("expected username 'lurus_7' to be created; got %+v", c.users)
	}
}

func TestOnAccountCreated_AlreadySynced_NoOp(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{}
	m := New(c, s)

	existingID := 42
	err := m.OnAccountCreated(context.Background(), &entity.Account{
		ID:           99,
		NewAPIUserID: &existingID,
	})
	if err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
	if c.createCalls != 0 || c.findCalls != 0 {
		t.Errorf("expected zero NewAPI calls, got create=%d find=%d", c.createCalls, c.findCalls)
	}
	if len(s.calls) != 0 {
		t.Errorf("expected zero store writes, got %d", len(s.calls))
	}
}

func TestOnAccountCreated_RecoversFromOrphanedNewAPIUser(t *testing.T) {
	// Simulate: previous run created NewAPI user successfully but
	// SetNewAPIUserID failed (DB blip). Now NewAPIUserID is still nil
	// in DB but NewAPI already has the user. Module should *find*
	// rather than create-again.
	c := newFakeClient()
	c.users["lurus_42"] = 1234 // pre-existing orphan
	s := &fakeStore{}
	m := New(c, s)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 42, DisplayName: "Alice"})
	if err != nil {
		t.Fatalf("OnAccountCreated: %v", err)
	}
	if c.createCalls != 0 {
		t.Errorf("expected NO Create (user already existed), got createCalls=%d", c.createCalls)
	}
	if len(s.calls) != 1 || s.calls[0].NewAPIUserID != 1234 {
		t.Errorf("expected mapping to 1234, got %+v", s.calls)
	}
}

func TestOnAccountCreated_CreateError_PropagatesAndDoesNotPersist(t *testing.T) {
	c := newFakeClient()
	c.createUserError = errors.New("boom")
	s := &fakeStore{}
	m := New(c, s)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 1})
	if err == nil {
		t.Fatal("expected error from CreateUser to propagate")
	}
	if len(s.calls) != 0 {
		t.Errorf("store should not be written on create failure; got %d calls", len(s.calls))
	}
}

func TestOnAccountCreated_StoreFailureDoesNotLoseNewAPISide(t *testing.T) {
	// SetNewAPIUserID failing leaves an "orphaned" NewAPI user. Next
	// hook trigger should recover via the find-first idempotent path.
	c := newFakeClient()
	s := &fakeStore{setError: errors.New("db blip")}
	m := New(c, s)

	err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 5})
	if err == nil {
		t.Fatal("expected error from store failure")
	}
	// NewAPI user got created but mapping unsaved.
	if _, ok := c.users["lurus_5"]; !ok {
		t.Error("expected NewAPI user to be created despite store failure")
	}

	// Now retry with healthy store — should find existing user, no create.
	s.setError = nil
	c.createCalls = 0
	c.findCalls = 0
	if err := m.OnAccountCreated(context.Background(), &entity.Account{ID: 5}); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if c.createCalls != 0 {
		t.Errorf("retry should not re-create user, got createCalls=%d", c.createCalls)
	}
}

func TestOnAccountCreated_NilAccount(t *testing.T) {
	m := New(newFakeClient(), &fakeStore{})
	if err := m.OnAccountCreated(context.Background(), nil); err == nil {
		t.Error("expected error on nil account")
	}
}

// ── OnTopupCompleted (4d) ─────────────────────────────────────────────────

func TestOnTopupCompleted_HappyPath(t *testing.T) {
	c := newFakeClient()
	c.quotas[7] = 0 // pre-existing user with 0 quota
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	m := New(c, s)

	if err := m.OnTopupCompleted(context.Background(), "evt-test", 42, 100.0); err != nil {
		t.Fatalf("OnTopupCompleted: %v", err)
	}
	if c.incCalls != 1 {
		t.Errorf("incCalls = %d, want 1", c.incCalls)
	}
	expectedDelta := int64(100.0 * QuotaPerUnit) // 100 * 500_000 = 50_000_000
	if c.quotas[7] != expectedDelta {
		t.Errorf("quota = %d, want %d", c.quotas[7], expectedDelta)
	}
}

func TestOnTopupCompleted_NoMapping_Skips(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: nil}, // not yet synced
		},
	}
	m := New(c, s)

	if err := m.OnTopupCompleted(context.Background(), "evt-1", 42, 50.0); err != nil {
		t.Fatalf("expected nil error on missing mapping, got: %v", err)
	}
	if c.incCalls != 0 {
		t.Error("expected zero NewAPI calls when mapping is nil")
	}
}

func TestOnTopupCompleted_AccountNotFound_Skips(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{accounts: map[int64]*entity.Account{}}
	m := New(c, s)

	if err := m.OnTopupCompleted(context.Background(), "evt-2", 999, 50.0); err != nil {
		t.Fatalf("expected nil error on missing account, got: %v", err)
	}
	if c.incCalls != 0 {
		t.Error("expected zero NewAPI calls on missing account")
	}
}

func TestOnTopupCompleted_ZeroOrNegative_NoOp(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{}
	m := New(c, s)

	for _, amount := range []float64{0, -5.0} {
		if err := m.OnTopupCompleted(context.Background(), "evt-z", 42, amount); err != nil {
			t.Errorf("amount=%v: expected nil error, got %v", amount, err)
		}
	}
	if c.incCalls != 0 {
		t.Error("expected zero NewAPI calls for non-positive amounts")
	}
}

// ── EnsureUserLLMToken (4e) ───────────────────────────────────────────────

func TestEnsureUserLLMToken_HappyPath_DefaultName(t *testing.T) {
	c := newFakeClient()
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	m := New(c, s)

	tok, err := m.EnsureUserLLMToken(context.Background(), 42, "")
	if err != nil {
		t.Fatalf("EnsureUserLLMToken: %v", err)
	}
	if tok.Key == "" || !strings.HasPrefix(tok.Key, "sk-fake-") {
		t.Errorf("unexpected key %q", tok.Key)
	}
	if tok.Name != DefaultTokenName {
		t.Errorf("name = %q, want %q", tok.Name, DefaultTokenName)
	}
}

func TestEnsureUserLLMToken_Idempotent(t *testing.T) {
	c := newFakeClient()
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	m := New(c, s)

	t1, _ := m.EnsureUserLLMToken(context.Background(), 42, "lucrum")
	t2, _ := m.EnsureUserLLMToken(context.Background(), 42, "lucrum")
	if t1.Key != t2.Key {
		t.Errorf("expected same key on repeat, got %q vs %q", t1.Key, t2.Key)
	}
	// Different name → different key.
	t3, _ := m.EnsureUserLLMToken(context.Background(), 42, "tally")
	if t3.Key == t1.Key {
		t.Errorf("expected distinct key for different name, got same %q", t3.Key)
	}
}

func TestEnsureUserLLMToken_UnprovisionedAccount_TypedError(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: nil}, // not yet synced
		},
	}
	m := New(c, s)

	_, err := m.EnsureUserLLMToken(context.Background(), 42, "")
	if !errors.Is(err, ErrAccountNotProvisioned) {
		t.Errorf("expected ErrAccountNotProvisioned, got %v", err)
	}
}

func TestEnsureUserLLMToken_AccountMissing_TypedError(t *testing.T) {
	c := newFakeClient()
	s := &fakeStore{accounts: map[int64]*entity.Account{}}
	m := New(c, s)

	_, err := m.EnsureUserLLMToken(context.Background(), 999, "")
	if !errors.Is(err, ErrAccountNotProvisioned) {
		t.Errorf("expected ErrAccountNotProvisioned for missing account, got %v", err)
	}
}

func TestEnsureUserLLMToken_NewAPIError_Propagates(t *testing.T) {
	c := newFakeClient()
	c.ensureKeyError = errors.New("newapi 503")
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	m := New(c, s)

	_, err := m.EnsureUserLLMToken(context.Background(), 42, "")
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if errors.Is(err, ErrAccountNotProvisioned) {
		t.Errorf("NewAPI error should NOT be ErrAccountNotProvisioned (different surface)")
	}
}

// ── Deduper integration (P0-1 平台硬化) ─────────────────────────────────

func TestOnTopupCompleted_DuplicateEvent_SkipsAndReturnsNil(t *testing.T) {
	c := newFakeClient()
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	d := &fakeDeduper{emitDuplicate: true}
	m := New(c, s).WithDeduper(d)

	if err := m.OnTopupCompleted(context.Background(), "evt-1", 42, 100.0); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Replay — should be a no-op AND no NewAPI call (no double-credit).
	if err := m.OnTopupCompleted(context.Background(), "evt-1", 42, 100.0); err != nil {
		t.Fatalf("duplicate replay should not error: %v", err)
	}
	if c.incCalls != 1 {
		t.Errorf("expected exactly 1 NewAPI call, got %d (DOUBLE-CREDIT BUG)", c.incCalls)
	}
}

func TestOnTopupCompleted_RedisDown_FailClosed_ReturnsError(t *testing.T) {
	c := newFakeClient()
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	d := &fakeDeduper{redisErr: true}
	m := New(c, s).WithDeduper(d)

	err := m.OnTopupCompleted(context.Background(), "evt-x", 42, 50.0)
	if err == nil {
		t.Fatal("Redis unavailable should bubble up so consumer NAKs (fail-closed)")
	}
	if !errors.Is(err, idempotency.ErrRedisUnavailable) {
		t.Errorf("expected wrapped ErrRedisUnavailable, got %v", err)
	}
	if c.incCalls != 0 {
		t.Errorf("must not credit NewAPI when dedup is unavailable; got %d calls", c.incCalls)
	}
}

func TestOnTopupCompleted_EmptyEventID_ProcessesWithWarning(t *testing.T) {
	c := newFakeClient()
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	d := &fakeDeduper{emptyEventReject: true}
	m := New(c, s).WithDeduper(d)

	// Empty event ID = upstream envelope bug; we don't drop the topup.
	if err := m.OnTopupCompleted(context.Background(), "", 42, 50.0); err != nil {
		t.Fatalf("empty event ID should not block traffic, got: %v", err)
	}
	if c.incCalls != 1 {
		t.Errorf("expected NewAPI to be credited despite missing event_id, got %d", c.incCalls)
	}
}

func TestOnTopupCompleted_NoDeduper_StillWorks(t *testing.T) {
	// dev / unit-test path: module without WithDeduper still functions
	// (no dedup, but no panic). Production main.go ALWAYS wires one.
	c := newFakeClient()
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	m := New(c, s) // no WithDeduper

	if err := m.OnTopupCompleted(context.Background(), "evt-no-dedup", 42, 50.0); err != nil {
		t.Fatalf("module without deduper should still work: %v", err)
	}
	if c.incCalls != 1 {
		t.Errorf("expected 1 credit, got %d", c.incCalls)
	}
}

func TestOnTopupCompleted_IncrementError_Propagates(t *testing.T) {
	c := newFakeClient()
	c.incError = errors.New("newapi down")
	newapiID := 7
	s := &fakeStore{
		accounts: map[int64]*entity.Account{
			42: {ID: 42, NewAPIUserID: &newapiID},
		},
	}
	m := New(c, s)

	err := m.OnTopupCompleted(context.Background(), "evt-test", 42, 100.0)
	if err == nil {
		t.Fatal("expected error to propagate so consumer can NAK")
	}
}
