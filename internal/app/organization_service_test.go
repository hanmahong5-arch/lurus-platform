package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ---------- in-memory mockOrgStore ----------

type mockOrgStore struct {
	mu      sync.Mutex
	orgs    map[int64]*entity.Organization
	bySlug  map[string]*entity.Organization
	members map[int64]map[int64]*entity.OrgMember // orgID → accountID → member
	keys    map[int64]*entity.OrgAPIKey
	keyHash map[string]*entity.OrgAPIKey
	wallets map[int64]*entity.OrgWallet
	nextOrg int64
	nextKey int64
}

func newMockOrgStore() *mockOrgStore {
	return &mockOrgStore{
		orgs:    make(map[int64]*entity.Organization),
		bySlug:  make(map[string]*entity.Organization),
		members: make(map[int64]map[int64]*entity.OrgMember),
		keys:    make(map[int64]*entity.OrgAPIKey),
		keyHash: make(map[string]*entity.OrgAPIKey),
		wallets: make(map[int64]*entity.OrgWallet),
		nextOrg: 1,
		nextKey: 1,
	}
}

func (m *mockOrgStore) Create(_ context.Context, org *entity.Organization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.bySlug[org.Slug]; exists {
		return fmt.Errorf("slug already exists: %s", org.Slug)
	}
	org.ID = m.nextOrg
	m.nextOrg++
	cp := *org
	m.orgs[org.ID] = &cp
	m.bySlug[org.Slug] = &cp
	return nil
}

func (m *mockOrgStore) GetByID(_ context.Context, id int64) (*entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orgs[id]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (m *mockOrgStore) GetBySlug(_ context.Context, slug string) (*entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.bySlug[slug]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (m *mockOrgStore) ListByAccountID(_ context.Context, accountID int64) ([]entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Organization
	for orgID, members := range m.members {
		if _, ok := members[accountID]; ok {
			if org, ok := m.orgs[orgID]; ok {
				out = append(out, *org)
			}
		}
	}
	return out, nil
}

func (m *mockOrgStore) UpdateStatus(_ context.Context, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orgs[id]
	if !ok {
		return fmt.Errorf("org not found: %d", id)
	}
	o.Status = status
	cp := *o
	m.orgs[id] = &cp
	return nil
}

func (m *mockOrgStore) ListAll(_ context.Context, limit, offset int) ([]entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []entity.Organization
	for _, o := range m.orgs {
		all = append(all, *o)
	}
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *mockOrgStore) AddMember(_ context.Context, mem *entity.OrgMember) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[mem.OrgID] == nil {
		m.members[mem.OrgID] = make(map[int64]*entity.OrgMember)
	}
	cp := *mem
	if cp.JoinedAt.IsZero() {
		cp.JoinedAt = time.Now()
	}
	m.members[mem.OrgID][mem.AccountID] = &cp
	return nil
}

func (m *mockOrgStore) RemoveMember(_ context.Context, orgID, accountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[orgID] != nil {
		delete(m.members[orgID], accountID)
	}
	return nil
}

func (m *mockOrgStore) GetMember(_ context.Context, orgID, accountID int64) (*entity.OrgMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[orgID] == nil {
		return nil, nil
	}
	mem, ok := m.members[orgID][accountID]
	if !ok {
		return nil, nil
	}
	cp := *mem
	return &cp, nil
}

func (m *mockOrgStore) ListMembers(_ context.Context, orgID int64) ([]entity.OrgMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.OrgMember
	for _, mem := range m.members[orgID] {
		out = append(out, *mem)
	}
	return out, nil
}

func (m *mockOrgStore) CreateAPIKey(_ context.Context, k *entity.OrgAPIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k.ID = m.nextKey
	m.nextKey++
	cp := *k
	m.keys[k.ID] = &cp
	m.keyHash[k.KeyHash] = &cp
	return nil
}

func (m *mockOrgStore) GetAPIKeyByHash(_ context.Context, hash string) (*entity.OrgAPIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keyHash[hash]
	if !ok {
		return nil, nil
	}
	cp := *k
	return &cp, nil
}

func (m *mockOrgStore) ListAPIKeys(_ context.Context, orgID int64) ([]entity.OrgAPIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.OrgAPIKey
	for _, k := range m.keys {
		if k.OrgID == orgID {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (m *mockOrgStore) RevokeAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok {
		return fmt.Errorf("api key not found: %d", id)
	}
	k.Status = "revoked"
	// Update keyHash index too
	m.keyHash[k.KeyHash] = k
	return nil
}

func (m *mockOrgStore) TouchAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.keys[id]; ok {
		now := time.Now()
		k.LastUsedAt = &now
	}
	return nil
}

func (m *mockOrgStore) GetOrCreateWallet(_ context.Context, orgID int64) (*entity.OrgWallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[orgID]
	if !ok {
		w = &entity.OrgWallet{OrgID: orgID}
		m.wallets[orgID] = w
	}
	cp := *w
	return &cp, nil
}

// ---------- helpers ----------

func makeSvc() (*OrganizationService, *mockOrgStore) {
	store := newMockOrgStore()
	return NewOrganizationService(store), store
}

func seedOrg(t *testing.T, svc *OrganizationService, ownerID int64) *entity.Organization {
	t.Helper()
	org, err := svc.Create(context.Background(), "Acme Corp", "acme-corp", ownerID)
	if err != nil {
		t.Fatalf("seed org: %v", err)
	}
	return org
}

// ---------- tests ----------

func TestOrgService_Create_OK(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(42)

	org, err := svc.Create(ctx, "Test Org", "test-org", ownerID)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if org.ID == 0 {
		t.Error("expected org.ID to be assigned")
	}
	if org.Slug != "test-org" {
		t.Errorf("slug: want test-org, got %s", org.Slug)
	}
	if org.Status != "active" {
		t.Errorf("status: want active, got %s", org.Status)
	}

	// Verify owner membership was created.
	mem, _ := store.GetMember(ctx, org.ID, ownerID)
	if mem == nil || mem.Role != "owner" {
		t.Error("expected owner membership to be created")
	}

	// Verify wallet was created.
	w, _ := store.GetOrCreateWallet(ctx, org.ID)
	if w == nil || w.OrgID != org.ID {
		t.Error("expected org wallet to be created")
	}
}

func TestOrgService_Create_InvalidSlug(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()

	cases := []string{"AB", "UPPERCASE", "has space", "x", "ab", "a-very-long-slug-that-exceeds-the-max-limit-of-32"}
	for _, slug := range cases {
		if _, err := svc.Create(ctx, "Name", slug, 1); err == nil {
			t.Errorf("expected error for slug %q, got nil", slug)
		}
	}
}

func TestOrgService_AddMember_NotAuthorized(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)
	regularMemberID := int64(2)

	org := seedOrg(t, svc, ownerID)

	// Add regular member
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: regularMemberID, Role: "member"})

	// Regular member tries to add someone — must fail.
	err := svc.AddMember(ctx, org.ID, regularMemberID, 99, "member")
	if err == nil {
		t.Error("expected permission denied error, got nil")
	}
}

func TestOrgService_RemoveMember_LastOwner(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)

	// Owner tries to remove themselves (only owner) — must fail.
	err := svc.RemoveMember(ctx, org.ID, ownerID, ownerID)
	if err == nil {
		t.Error("expected error when removing last owner, got nil")
	}
}

func TestOrgService_CreateAPIKey_HashStored(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)

	rawKey, key, err := svc.CreateAPIKey(ctx, org.ID, ownerID, "my-key")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if rawKey == "" {
		t.Error("rawKey must not be empty")
	}
	// The stored hash must differ from the raw key.
	sum := sha256.Sum256([]byte(rawKey))
	expectedHash := hex.EncodeToString(sum[:])
	if rawKey == expectedHash {
		t.Error("rawKey must not equal keyHash")
	}
	if key.KeyHash != expectedHash {
		t.Errorf("keyHash mismatch: want %s, got %s", expectedHash, key.KeyHash)
	}
	// Verify key is retrievable by hash from store.
	stored, _ := store.GetAPIKeyByHash(ctx, expectedHash)
	if stored == nil {
		t.Error("api key not found in store by hash")
	}
}

func TestOrgService_ResolveAPIKey_OK(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)
	rawKey, _, err := svc.CreateAPIKey(ctx, org.ID, ownerID, "resolve-test")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	resolved, err := svc.ResolveAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	if resolved == nil || resolved.ID != org.ID {
		t.Errorf("resolved org ID: want %d, got %v", org.ID, resolved)
	}
}

func TestOrgService_ResolveAPIKey_Revoked(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)
	rawKey, key, _ := svc.CreateAPIKey(ctx, org.ID, ownerID, "revoke-test")

	// Revoke the key via store directly to bypass service auth for simplicity.
	_ = store.RevokeAPIKey(ctx, key.ID)

	_, err := svc.ResolveAPIKey(ctx, rawKey)
	if err == nil {
		t.Error("expected error for revoked api key, got nil")
	}
}

func TestOrgService_Get_OK(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)

	got, err := svc.Get(ctx, org.ID, ownerID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.ID != org.ID {
		t.Errorf("expected org %d, got %v", org.ID, got)
	}
}

func TestOrgService_Get_NotMember(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()

	org := seedOrg(t, svc, 1)

	_, err := svc.Get(ctx, org.ID, 999) // 999 is not a member
	if err == nil {
		t.Error("expected permission error, got nil")
	}
}

func TestOrgService_ListMine(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()

	seedOrg(t, svc, 1)

	orgs, err := svc.ListMine(ctx, 1)
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	if len(orgs) != 1 {
		t.Errorf("expected 1 org, got %d", len(orgs))
	}
}

func TestOrgService_RevokeAPIKey_OK(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)
	rawKey, key, _ := svc.CreateAPIKey(ctx, org.ID, ownerID, "to-revoke")

	if err := svc.RevokeAPIKey(ctx, org.ID, ownerID, key.ID); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}

	// After revocation, resolving must fail.
	_, err := svc.ResolveAPIKey(ctx, rawKey)
	if err == nil {
		t.Error("expected error resolving revoked key")
	}
}

func TestOrgService_ListAPIKeys(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)
	_, _, _ = svc.CreateAPIKey(ctx, org.ID, ownerID, "key-a")
	_, _, _ = svc.CreateAPIKey(ctx, org.ID, ownerID, "key-b")

	keys, err := svc.ListAPIKeys(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestOrgService_GetWallet(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)
	w, err := svc.GetWallet(ctx, org.ID)
	if err != nil {
		t.Fatalf("GetWallet: %v", err)
	}
	if w == nil || w.OrgID != org.ID {
		t.Errorf("expected wallet for org %d, got %v", org.ID, w)
	}
}

func TestOrgService_ListAll(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()

	if _, err := svc.Create(ctx, "Org One", "org-one", 1); err != nil {
		t.Fatalf("create org one: %v", err)
	}
	if _, err := svc.Create(ctx, "Org Two", "org-two", 2); err != nil {
		t.Fatalf("create org two: %v", err)
	}

	orgs, err := svc.ListAll(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(orgs) < 2 {
		t.Errorf("expected ≥2 orgs, got %d", len(orgs))
	}
}

func TestOrgService_UpdateStatus(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()

	org := seedOrg(t, svc, 1)
	if err := svc.UpdateStatus(ctx, org.ID, "suspended"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
}

// TestOrgService_Create_DuplicateSlug verifies that creating two orgs with the same slug returns an error.
func TestOrgService_Create_DuplicateSlug(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()

	if _, err := svc.Create(ctx, "First Org", "shared-slug", 1); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := svc.Create(ctx, "Second Org", "shared-slug", 2); err == nil {
		t.Error("expected error on duplicate slug, got nil")
	}
}

// TestOrgService_AddMember_Success verifies that an owner can successfully add a new member.
func TestOrgService_AddMember_Success(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)
	newMemberID := int64(99)

	org := seedOrg(t, svc, ownerID)

	if err := svc.AddMember(ctx, org.ID, ownerID, newMemberID, "member"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	mem, _ := store.GetMember(ctx, org.ID, newMemberID)
	if mem == nil || mem.Role != "member" {
		t.Errorf("expected new member with role=member, got %v", mem)
	}
}

// TestOrgService_RemoveMember_NotAuthorized verifies that a regular member cannot remove others.
func TestOrgService_RemoveMember_NotAuthorized(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)
	regularID := int64(2)
	victimID := int64(3)

	org := seedOrg(t, svc, ownerID)
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: regularID, Role: "member"})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: victimID, Role: "member"})

	err := svc.RemoveMember(ctx, org.ID, regularID, victimID)
	if err == nil {
		t.Error("expected permission denied error, got nil")
	}
}

// TestOrgService_RemoveMember_TargetNotFound verifies that removing a non-member returns an error.
func TestOrgService_RemoveMember_TargetNotFound(t *testing.T) {
	svc, _ := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)

	org := seedOrg(t, svc, ownerID)

	// Target 999 is not a member.
	err := svc.RemoveMember(ctx, org.ID, ownerID, 999)
	if err == nil {
		t.Error("expected 'member not found' error, got nil")
	}
}

// TestOrgService_RemoveMember_Success verifies that an owner can remove a regular member.
func TestOrgService_RemoveMember_Success(t *testing.T) {
	svc, store := makeSvc()
	ctx := context.Background()
	ownerID := int64(1)
	memberID := int64(5)

	org := seedOrg(t, svc, ownerID)
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: memberID, Role: "member"})

	if err := svc.RemoveMember(ctx, org.ID, ownerID, memberID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	mem, _ := store.GetMember(ctx, org.ID, memberID)
	if mem != nil {
		t.Error("expected member to be removed, but still present")
	}
}

// errGetMemberOrgStore returns an error from GetMember to cover the RevokeAPIKey db-error branch.
type errGetMemberOrgStore struct{ mockOrgStore }

func (s *errGetMemberOrgStore) GetMember(_ context.Context, _, _ int64) (*entity.OrgMember, error) {
	return nil, fmt.Errorf("db error")
}

// TestOrgService_RevokeAPIKey_GetMemberError covers the GetMember error branch in RevokeAPIKey.
func TestOrgService_RevokeAPIKey_GetMemberError(t *testing.T) {
	store := &errGetMemberOrgStore{*newMockOrgStore()}
	svc := NewOrganizationService(store)

	err := svc.RevokeAPIKey(context.Background(), 1, 1, 1)
	if err == nil {
		t.Fatal("expected error from GetMember, got nil")
	}
}

// TestOrgService_CreateAPIKey_GetMemberError covers the GetMember error branch in CreateAPIKey.
func TestOrgService_CreateAPIKey_GetMemberError(t *testing.T) {
	store := &errGetMemberOrgStore{*newMockOrgStore()}
	svc := NewOrganizationService(store)

	_, _, err := svc.CreateAPIKey(context.Background(), 1, 1, "test-key")
	if err == nil {
		t.Fatal("expected error from GetMember, got nil")
	}
}
