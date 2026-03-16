package repo

import (
	"context"
	"fmt"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// newTestOrg returns a minimal Organization for use in tests.
func newTestOrg(slug string) *entity.Organization {
	return &entity.Organization{
		Name:           "Test Org " + slug,
		Slug:           slug,
		OwnerAccountID: 1,
		Status:         "active",
		Plan:           "free",
	}
}

// TestOrgRepo_Create_Success verifies an organization can be created and its ID is populated.
func TestOrgRepo_Create_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-create")
	if err := r.Create(ctx, org); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if org.ID == 0 {
		t.Error("expected non-zero ID after create")
	}
}

// TestOrgRepo_GetByID_Found verifies retrieval of an existing organization by ID.
func TestOrgRepo_GetByID_Found(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-byid")
	_ = r.Create(ctx, org)

	got, err := r.GetByID(ctx, org.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil org")
	}
	if got.Slug != "slug-byid" {
		t.Errorf("Slug = %q, want %q", got.Slug, "slug-byid")
	}
}

// TestOrgRepo_GetByID_NotFound verifies nil is returned for a non-existent ID.
func TestOrgRepo_GetByID_NotFound(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)

	got, err := r.GetByID(context.Background(), 99999)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent org")
	}
}

// TestOrgRepo_GetBySlug_Found verifies retrieval by slug.
func TestOrgRepo_GetBySlug_Found(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	_ = r.Create(ctx, newTestOrg("my-company"))

	got, err := r.GetBySlug(ctx, "my-company")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got == nil || got.Slug != "my-company" {
		t.Errorf("GetBySlug returned wrong org: %v", got)
	}
}

// TestOrgRepo_GetBySlug_NotFound verifies nil is returned for an unknown slug.
func TestOrgRepo_GetBySlug_NotFound(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)

	got, err := r.GetBySlug(context.Background(), "nonexistent-slug")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent slug")
	}
}

// TestOrgRepo_ListAll_Pagination verifies listing with limit and offset.
func TestOrgRepo_ListAll_Pagination(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = r.Create(ctx, newTestOrg(fmt.Sprintf("paginate-%d", i)))
	}

	page1, err := r.ListAll(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListAll page1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("page1 got %d, want 3", len(page1))
	}

	page2, err := r.ListAll(ctx, 3, 3)
	if err != nil {
		t.Fatalf("ListAll page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page2 got %d, want 2", len(page2))
	}
}

// TestOrgRepo_UpdateStatus_Success verifies the status field is updated.
func TestOrgRepo_UpdateStatus_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-status")
	_ = r.Create(ctx, org)

	if err := r.UpdateStatus(ctx, org.ID, "suspended"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := r.GetByID(ctx, org.ID)
	if got.Status != "suspended" {
		t.Errorf("Status = %q, want suspended", got.Status)
	}
}

// TestOrgRepo_AddMember_Success verifies a member can be added to an organization.
func TestOrgRepo_AddMember_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-member")
	_ = r.Create(ctx, org)

	m := &entity.OrgMember{OrgID: org.ID, AccountID: 42, Role: "member"}
	if err := r.AddMember(ctx, m); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	got, err := r.GetMember(ctx, org.ID, 42)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil member")
	}
	if got.Role != "member" {
		t.Errorf("Role = %q, want member", got.Role)
	}
}

// TestOrgRepo_AddMember_Idempotent verifies calling AddMember twice for the same (org, account) is safe.
func TestOrgRepo_AddMember_Idempotent(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-idem")
	_ = r.Create(ctx, org)

	m1 := &entity.OrgMember{OrgID: org.ID, AccountID: 10, Role: "member"}
	m2 := &entity.OrgMember{OrgID: org.ID, AccountID: 10, Role: "admin"}
	if err := r.AddMember(ctx, m1); err != nil {
		t.Fatalf("AddMember first: %v", err)
	}
	if err := r.AddMember(ctx, m2); err != nil {
		t.Fatalf("AddMember second (idempotent): %v", err)
	}

	members, _ := r.ListMembers(ctx, org.ID)
	if len(members) != 1 {
		t.Errorf("expected 1 member after idempotent add, got %d", len(members))
	}
}

// TestOrgRepo_RemoveMember_Success verifies a member is removed from the organization.
func TestOrgRepo_RemoveMember_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-remove")
	_ = r.Create(ctx, org)
	_ = r.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: 55, Role: "member"})

	if err := r.RemoveMember(ctx, org.ID, 55); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	got, _ := r.GetMember(ctx, org.ID, 55)
	if got != nil {
		t.Error("expected nil after remove")
	}
}

// TestOrgRepo_GetMember_NotFound verifies nil is returned for a non-existent member.
func TestOrgRepo_GetMember_NotFound(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)

	got, err := r.GetMember(context.Background(), 999, 888)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent member")
	}
}

// TestOrgRepo_ListMembers_Success verifies all members of an org are returned.
func TestOrgRepo_ListMembers_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-list-members")
	_ = r.Create(ctx, org)
	_ = r.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: 1, Role: "owner"})
	_ = r.AddMember(ctx, &entity.OrgMember{OrgID: org.ID, AccountID: 2, Role: "member"})

	members, err := r.ListMembers(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}
}

// TestOrgRepo_CreateAPIKey_Success verifies an API key can be created.
func TestOrgRepo_CreateAPIKey_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-apikey")
	_ = r.Create(ctx, org)

	k := &entity.OrgAPIKey{
		OrgID:     org.ID,
		KeyHash:   "sha256hash001",
		KeyPrefix: "lk_",
		Name:      "CI Key",
		CreatedBy: 1,
		Status:    "active",
	}
	if err := r.CreateAPIKey(ctx, k); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if k.ID == 0 {
		t.Error("expected non-zero ID after CreateAPIKey")
	}
}

// TestOrgRepo_GetAPIKeyByHash_Found verifies retrieval by key hash.
func TestOrgRepo_GetAPIKeyByHash_Found(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-keyhash")
	_ = r.Create(ctx, org)
	_ = r.CreateAPIKey(ctx, &entity.OrgAPIKey{
		OrgID: org.ID, KeyHash: "uniquehash999", KeyPrefix: "lk_",
		Name: "Test Key", CreatedBy: 1, Status: "active",
	})

	got, err := r.GetAPIKeyByHash(ctx, "uniquehash999")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil API key")
	}
	if got.KeyHash != "uniquehash999" {
		t.Errorf("KeyHash = %q, want uniquehash999", got.KeyHash)
	}
}

// TestOrgRepo_GetAPIKeyByHash_NotFound verifies nil is returned for an unknown hash.
func TestOrgRepo_GetAPIKeyByHash_NotFound(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)

	got, err := r.GetAPIKeyByHash(context.Background(), "nonexistent-hash")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if got != nil {
		t.Error("expected nil for unknown hash")
	}
}

// TestOrgRepo_ListAPIKeys_Success verifies all API keys for an org are listed.
func TestOrgRepo_ListAPIKeys_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-listkeys")
	_ = r.Create(ctx, org)
	_ = r.CreateAPIKey(ctx, &entity.OrgAPIKey{OrgID: org.ID, KeyHash: "h1", KeyPrefix: "lk_", Name: "K1", CreatedBy: 1, Status: "active"})
	_ = r.CreateAPIKey(ctx, &entity.OrgAPIKey{OrgID: org.ID, KeyHash: "h2", KeyPrefix: "lk_", Name: "K2", CreatedBy: 1, Status: "active"})

	keys, err := r.ListAPIKeys(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("got %d keys, want 2", len(keys))
	}
}

// TestOrgRepo_RevokeAPIKey_Success verifies revoking changes status to "revoked".
func TestOrgRepo_RevokeAPIKey_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-revoke")
	_ = r.Create(ctx, org)
	k := &entity.OrgAPIKey{OrgID: org.ID, KeyHash: "rev-hash", KeyPrefix: "lk_", Name: "Rev Key", CreatedBy: 1, Status: "active"}
	_ = r.CreateAPIKey(ctx, k)

	if err := r.RevokeAPIKey(ctx, k.ID); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}

	got, _ := r.GetAPIKeyByHash(ctx, "rev-hash")
	if got.Status != "revoked" {
		t.Errorf("Status = %q, want revoked", got.Status)
	}
}

// TestOrgRepo_TouchAPIKey_Success verifies last_used_at is set after Touch.
func TestOrgRepo_TouchAPIKey_Success(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-touch")
	_ = r.Create(ctx, org)
	k := &entity.OrgAPIKey{OrgID: org.ID, KeyHash: "touch-hash", KeyPrefix: "lk_", Name: "Touch Key", CreatedBy: 1, Status: "active"}
	_ = r.CreateAPIKey(ctx, k)

	if err := r.TouchAPIKey(ctx, k.ID); err != nil {
		t.Fatalf("TouchAPIKey: %v", err)
	}

	got, _ := r.GetAPIKeyByHash(ctx, "touch-hash")
	if got.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be set after TouchAPIKey")
	}
}

// TestOrgRepo_GetOrCreateWallet_CreatesNew verifies a wallet is created when none exists.
func TestOrgRepo_GetOrCreateWallet_CreatesNew(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-wallet")
	_ = r.Create(ctx, org)

	w, err := r.GetOrCreateWallet(ctx, org.ID)
	if err != nil {
		t.Fatalf("GetOrCreateWallet: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil wallet")
	}
	if w.OrgID != org.ID {
		t.Errorf("OrgID = %d, want %d", w.OrgID, org.ID)
	}
}

// TestOrgRepo_GetOrCreateWallet_ReturnsExisting verifies calling twice returns the same wallet.
func TestOrgRepo_GetOrCreateWallet_ReturnsExisting(t *testing.T) {
	db := setupOrgDB(t)
	r := NewOrganizationRepo(db)
	ctx := context.Background()

	org := newTestOrg("slug-wallet2")
	_ = r.Create(ctx, org)

	w1, _ := r.GetOrCreateWallet(ctx, org.ID)
	w2, err := r.GetOrCreateWallet(ctx, org.ID)
	if err != nil {
		t.Fatalf("GetOrCreateWallet second call: %v", err)
	}
	if w1.OrgID != w2.OrgID {
		t.Error("expected same wallet on second call")
	}
}
