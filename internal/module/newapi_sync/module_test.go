package newapi_sync

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/newapi"
)

// fakeClient 是 NewAPIClient 的内存实现，记录调用次数 + 模拟 user 库。
type fakeClient struct {
	mu              sync.Mutex
	users           map[string]int // username -> id
	nextID          int
	createCalls     int
	findCalls       int
	createUserError error
	findError       error
}

func newFakeClient() *fakeClient {
	return &fakeClient{users: map[string]int{}, nextID: 1000}
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
	return nil
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
