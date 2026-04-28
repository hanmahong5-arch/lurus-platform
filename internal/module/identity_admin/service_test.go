package identity_admin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── 内存版 Store + ZitadelClient — 让测试不依赖 GORM/HTTP ─────────────────

type memStore struct {
	mu     sync.Mutex
	rows   map[int64]*entity.APIKey
	byName map[string]int64
	nextID int64
}

func newMemStore() *memStore {
	return &memStore{rows: map[int64]*entity.APIKey{}, byName: map[string]int64{}, nextID: 1}
}

func (s *memStore) FindByName(_ context.Context, name string) (*entity.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byName[name]
	if !ok {
		return nil, repo.ErrAPIKeyNotFound
	}
	r := *s.rows[id]
	return &r, nil
}
func (s *memStore) FindByID(_ context.Context, id int64) (*entity.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return nil, repo.ErrAPIKeyNotFound
	}
	c := *r
	return &c, nil
}
func (s *memStore) Create(_ context.Context, k *entity.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[k.Name]; ok {
		return errors.New("memstore: duplicate name")
	}
	k.ID = s.nextID
	s.nextID++
	k.CreatedAt = time.Now()
	k.UpdatedAt = time.Now()
	cpy := *k
	s.rows[k.ID] = &cpy
	s.byName[k.Name] = k.ID
	return nil
}
func (s *memStore) MarkActive(_ context.Context, id int64, uid, tid, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return repo.ErrAPIKeyNotFound
	}
	r.Status = entity.APIKeyStatusActive
	r.ZitadelUserID = uid
	r.ZitadelTokenID = tid
	r.TokenHash = h
	r.Error = ""
	r.UpdatedAt = time.Now()
	return nil
}
func (s *memStore) MarkFailed(_ context.Context, id int64, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return repo.ErrAPIKeyNotFound
	}
	r.Status = entity.APIKeyStatusFailed
	r.Error = msg
	r.UpdatedAt = time.Now()
	return nil
}
func (s *memStore) MarkRevoked(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return repo.ErrAPIKeyNotFound
	}
	r.Status = entity.APIKeyStatusRevoked
	now := time.Now()
	r.RevokedAt = &now
	r.UpdatedAt = now
	return nil
}
func (s *memStore) UpdateToken(_ context.Context, id int64, tid, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return repo.ErrAPIKeyNotFound
	}
	r.ZitadelTokenID = tid
	r.TokenHash = h
	r.UpdatedAt = time.Now()
	return nil
}
func (s *memStore) Reincarnate(_ context.Context, id int64, dn, p string, exp *time.Time, by *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return repo.ErrAPIKeyNotFound
	}
	r.DisplayName = dn
	r.Purpose = p
	r.ExpiresAt = exp
	r.CreatedBy = by
	r.ZitadelUserID = ""
	r.ZitadelTokenID = ""
	r.TokenHash = ""
	r.Status = entity.APIKeyStatusCreating
	r.Error = ""
	r.RevokedAt = nil
	r.UpdatedAt = time.Now()
	return nil
}
func (s *memStore) List(_ context.Context, purpose, status string, limit, offset int) ([]entity.APIKey, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []entity.APIKey{}
	for _, r := range s.rows {
		if purpose != "" && r.Purpose != purpose {
			continue
		}
		if status != "" && r.Status != status {
			continue
		}
		if status == "" && r.Status == entity.APIKeyStatusRevoked {
			continue
		}
		out = append(out, *r)
	}
	return out, int64(len(out)), nil
}

// ── 可控 ZitadelClient — 测试可注入失败 ─────────────────────────────────────

type fakeZitadel struct {
	createUserErr error
	createPATErr  error
	deleteUserErr error
	deletePATErr  error

	createUserCalls int
	createPATCalls  int
	deleteUserCalls int
	deletePATCalls  int

	nextUserID  string
	nextTokenID string
	nextToken   string
}

func (z *fakeZitadel) CreateMachineUser(_ context.Context, _, _, _ string) (string, error) {
	z.createUserCalls++
	if z.createUserErr != nil {
		return "", z.createUserErr
	}
	if z.nextUserID == "" {
		return "user-default", nil
	}
	return z.nextUserID, nil
}
func (z *fakeZitadel) CreatePAT(_ context.Context, _ string, _ time.Time) (string, string, error) {
	z.createPATCalls++
	if z.createPATErr != nil {
		return "", "", z.createPATErr
	}
	tid := z.nextTokenID
	if tid == "" {
		tid = "tok-default"
	}
	tk := z.nextToken
	if tk == "" {
		tk = "pat_secret"
	}
	return tid, tk, nil
}
func (z *fakeZitadel) DeletePAT(_ context.Context, _, _ string) error {
	z.deletePATCalls++
	return z.deletePATErr
}
func (z *fakeZitadel) DeleteUser(_ context.Context, _ string) error {
	z.deleteUserCalls++
	return z.deleteUserErr
}

// ── tests ─────────────────────────────────────────────────────────────────

func TestService_Create_HappyPath(t *testing.T) {
	store := newMemStore()
	z := &fakeZitadel{nextUserID: "u-100", nextTokenID: "t-100", nextToken: "pat_secret_xyz"}
	svc := NewService(store, z, nil)

	out, err := svc.Create(context.Background(), CreateRequest{
		Name:           "login-ui",
		DisplayName:    "Lurus Login UI",
		Purpose:        entity.APIKeyPurposeLoginUI,
		ExpirationDays: 365,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Token != "pat_secret_xyz" {
		t.Fatalf("token mismatch: got %q", out.Token)
	}
	if out.APIKey.Status != entity.APIKeyStatusActive {
		t.Fatalf("status: got %s want active", out.APIKey.Status)
	}
	if out.APIKey.ZitadelUserID != "u-100" {
		t.Fatalf("zitadel user id not stored")
	}
	if out.APIKey.TokenHash == "" {
		t.Fatalf("token hash should be filled")
	}
	if z.createUserCalls != 1 || z.createPATCalls != 1 {
		t.Fatalf("expected 1+1 zitadel calls, got %d+%d", z.createUserCalls, z.createPATCalls)
	}
}

func TestService_Create_Idempotent_ActiveKeyReturnsExistsError(t *testing.T) {
	store := newMemStore()
	z := &fakeZitadel{}
	svc := NewService(store, z, nil)

	first, err := svc.Create(context.Background(), CreateRequest{
		Name: "mcp-key", DisplayName: "MCP Key", Purpose: entity.APIKeyPurposeMCP,
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Second call with same name on an active key should:
	// - NOT call zitadel again
	// - return ErrAPIKeyExists with the existing row metadata (no token)
	second, err := svc.Create(context.Background(), CreateRequest{
		Name: "mcp-key", DisplayName: "MCP Key 2", Purpose: entity.APIKeyPurposeMCP,
	})
	if !errors.Is(err, ErrAPIKeyExists) {
		t.Fatalf("want ErrAPIKeyExists, got %v", err)
	}
	if second.Token != "" {
		t.Fatalf("second call must NOT return token; got %q", second.Token)
	}
	if second.APIKey.ID != first.APIKey.ID {
		t.Fatalf("row id should match; got %d vs %d", second.APIKey.ID, first.APIKey.ID)
	}
	if z.createUserCalls != 1 {
		t.Fatalf("zitadel should not be called second time; got %d calls", z.createUserCalls)
	}
}

func TestService_Create_FailedKey_Reincarnates(t *testing.T) {
	store := newMemStore()
	z := &fakeZitadel{createUserErr: errors.New("zitadel boom")}
	svc := NewService(store, z, nil)

	// First attempt fails after the row is in 'creating' state.
	_, err := svc.Create(context.Background(), CreateRequest{
		Name: "ext-monitor", DisplayName: "External Monitor", Purpose: entity.APIKeyPurposeExternal,
	})
	if err == nil {
		t.Fatalf("expected err, got nil")
	}

	// Row should be 'failed' now.
	row, _ := store.FindByName(context.Background(), "ext-monitor")
	if row.Status != entity.APIKeyStatusFailed {
		t.Fatalf("after failure want status=failed, got %s", row.Status)
	}

	// Recover zitadel and retry — service should reincarnate the row,
	// not create a new one (id stable).
	z.createUserErr = nil
	z.nextUserID = "u-fresh"
	z.nextToken = "pat_fresh_token"

	out, err := svc.Create(context.Background(), CreateRequest{
		Name: "ext-monitor", DisplayName: "External Monitor", Purpose: entity.APIKeyPurposeExternal,
	})
	if err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if out.APIKey.ID != row.ID {
		t.Fatalf("retry must reincarnate same row; got id %d vs %d", out.APIKey.ID, row.ID)
	}
	if out.Token != "pat_fresh_token" {
		t.Fatalf("retry should return new token; got %q", out.Token)
	}
}

func TestService_Create_PATFailure_CleansUpUser(t *testing.T) {
	store := newMemStore()
	z := &fakeZitadel{
		nextUserID:   "u-300",
		createPATErr: errors.New("pat boom"),
	}
	svc := NewService(store, z, nil)

	_, err := svc.Create(context.Background(), CreateRequest{
		Name: "stuck", DisplayName: "Stuck Key", Purpose: entity.APIKeyPurposeAdmin,
	})
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if z.deleteUserCalls != 1 {
		t.Fatalf("PAT failure must trigger DeleteUser cleanup; got %d calls", z.deleteUserCalls)
	}
	row, _ := store.FindByName(context.Background(), "stuck")
	if row.Status != entity.APIKeyStatusFailed {
		t.Fatalf("want status=failed, got %s", row.Status)
	}
}

func TestService_Create_ValidatesInput(t *testing.T) {
	svc := NewService(newMemStore(), &fakeZitadel{}, nil)

	bad := []CreateRequest{
		{Name: "AB", DisplayName: "x", Purpose: entity.APIKeyPurposeMCP},          // too short
		{Name: "INVALID", DisplayName: "x", Purpose: entity.APIKeyPurposeMCP},     // uppercase
		{Name: "ok-name", DisplayName: "", Purpose: entity.APIKeyPurposeMCP},      // empty display
		{Name: "ok-name", DisplayName: "x", Purpose: "not_a_real_purpose"},        // bad purpose
		{Name: "ok-name", DisplayName: "x", Purpose: entity.APIKeyPurposeMCP, ExpirationDays: -1}, // bad days
	}
	for i, b := range bad {
		if _, err := svc.Create(context.Background(), b); err == nil {
			t.Errorf("case %d: expected validation error, got nil", i)
		}
	}
}

func TestService_Rotate_ReplacesToken(t *testing.T) {
	store := newMemStore()
	z := &fakeZitadel{nextUserID: "u-1", nextTokenID: "t-1", nextToken: "first"}
	svc := NewService(store, z, nil)

	first, err := svc.Create(context.Background(), CreateRequest{
		Name: "rotate-me", DisplayName: "Rotate Me", Purpose: entity.APIKeyPurposeMCP,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	z.nextTokenID = "t-2"
	z.nextToken = "second"
	out, err := svc.Rotate(context.Background(), "rotate-me")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if out.Token != "second" {
		t.Fatalf("rotate should return new token; got %q", out.Token)
	}
	if out.APIKey.ZitadelTokenID != "t-2" {
		t.Fatalf("rotate should update token id; got %s", out.APIKey.ZitadelTokenID)
	}
	if z.deletePATCalls != 1 {
		t.Fatalf("rotate should delete old PAT; got %d", z.deletePATCalls)
	}
	if first.APIKey.ID != out.APIKey.ID {
		t.Fatalf("rotate must keep same row; %d vs %d", first.APIKey.ID, out.APIKey.ID)
	}
}

func TestService_Revoke_Idempotent(t *testing.T) {
	store := newMemStore()
	z := &fakeZitadel{nextUserID: "u-1"}
	svc := NewService(store, z, nil)

	_, _ = svc.Create(context.Background(), CreateRequest{
		Name: "doomed", DisplayName: "Doomed", Purpose: entity.APIKeyPurposeAdmin,
	})

	if err := svc.Revoke(context.Background(), "doomed"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if z.deleteUserCalls != 1 {
		t.Fatalf("first revoke should delete zitadel user; got %d", z.deleteUserCalls)
	}
	// Second revoke is a no-op (idempotent).
	if err := svc.Revoke(context.Background(), "doomed"); err != nil {
		t.Fatalf("second revoke must be idempotent: %v", err)
	}
	if z.deleteUserCalls != 1 {
		t.Fatalf("second revoke must not call zitadel again; got %d", z.deleteUserCalls)
	}
}

func TestService_Create_ReportsZitadelErrorContext(t *testing.T) {
	z := &fakeZitadel{createUserErr: errors.New("403 forbidden")}
	svc := NewService(newMemStore(), z, nil)

	_, err := svc.Create(context.Background(), CreateRequest{
		Name: "no-perm", DisplayName: "No Perm", Purpose: entity.APIKeyPurposeAdmin,
	})
	if err == nil {
		t.Fatalf("expected err")
	}
	// Error should mention the underlying cause so operators can act.
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("err should surface zitadel cause; got %v", err)
	}
}
