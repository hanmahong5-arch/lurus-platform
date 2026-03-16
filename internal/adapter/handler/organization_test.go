package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ---------- mock org store (satisfies app.orgStore structurally) ----------

type mockOrgStoreH struct {
	mu      sync.Mutex
	orgs    map[int64]*entity.Organization
	bySlug  map[string]*entity.Organization
	members map[int64]map[int64]*entity.OrgMember
	keys    map[int64]*entity.OrgAPIKey
	keyHash map[string]*entity.OrgAPIKey
	wallets map[int64]*entity.OrgWallet
	nextOrg int64
	nextKey int64
}

func newMockOrgStoreH() *mockOrgStoreH {
	return &mockOrgStoreH{
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

func (m *mockOrgStoreH) Create(_ context.Context, org *entity.Organization) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.bySlug[org.Slug]; exists {
		return fmt.Errorf("slug already exists")
	}
	org.ID = m.nextOrg
	m.nextOrg++
	cp := *org
	m.orgs[org.ID] = &cp
	m.bySlug[org.Slug] = &cp
	return nil
}

func (m *mockOrgStoreH) GetByID(_ context.Context, id int64) (*entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orgs[id]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (m *mockOrgStoreH) GetBySlug(_ context.Context, slug string) (*entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.bySlug[slug]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}

func (m *mockOrgStoreH) ListByAccountID(_ context.Context, accountID int64) ([]entity.Organization, error) {
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

func (m *mockOrgStoreH) UpdateStatus(_ context.Context, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orgs[id]
	if !ok {
		return fmt.Errorf("org not found")
	}
	o.Status = status
	return nil
}

func (m *mockOrgStoreH) ListAll(_ context.Context, limit, _ int) ([]entity.Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.Organization
	for _, o := range m.orgs {
		out = append(out, *o)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *mockOrgStoreH) AddMember(_ context.Context, mem *entity.OrgMember) error {
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

func (m *mockOrgStoreH) RemoveMember(_ context.Context, orgID, accountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[orgID] != nil {
		delete(m.members[orgID], accountID)
	}
	return nil
}

func (m *mockOrgStoreH) GetMember(_ context.Context, orgID, accountID int64) (*entity.OrgMember, error) {
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

func (m *mockOrgStoreH) ListMembers(_ context.Context, orgID int64) ([]entity.OrgMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []entity.OrgMember
	for _, mem := range m.members[orgID] {
		out = append(out, *mem)
	}
	return out, nil
}

func (m *mockOrgStoreH) CreateAPIKey(_ context.Context, k *entity.OrgAPIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k.ID = m.nextKey
	m.nextKey++
	cp := *k
	m.keys[k.ID] = &cp
	m.keyHash[k.KeyHash] = &cp
	return nil
}

func (m *mockOrgStoreH) GetAPIKeyByHash(_ context.Context, hash string) (*entity.OrgAPIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keyHash[hash]
	if !ok {
		return nil, nil
	}
	cp := *k
	return &cp, nil
}

func (m *mockOrgStoreH) ListAPIKeys(_ context.Context, orgID int64) ([]entity.OrgAPIKey, error) {
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

func (m *mockOrgStoreH) RevokeAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[id]
	if !ok {
		return fmt.Errorf("key not found")
	}
	k.Status = "revoked"
	m.keyHash[k.KeyHash] = k
	return nil
}

func (m *mockOrgStoreH) TouchAPIKey(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.keys[id]; ok {
		now := time.Now()
		k.LastUsedAt = &now
	}
	return nil
}

func (m *mockOrgStoreH) GetOrCreateWallet(_ context.Context, orgID int64) (*entity.OrgWallet, error) {
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

// ---------- helper ----------

func makeOrgHandler() (*OrganizationHandler, *mockOrgStoreH) {
	store := newMockOrgStoreH()
	svc := app.NewOrganizationService(store)
	return NewOrganizationHandler(svc), store
}

func postJSON(r http.Handler, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- tests ----------

func TestOrgHandler_Create_OK(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.Use(withAccountID(10))
	r.POST("/organizations", h.Create)

	w := postJSON(r, "/organizations", map[string]string{
		"name": "My Company",
		"slug": "my-company",
	})

	if w.Code != http.StatusCreated {
		t.Errorf("status: want 201, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["slug"] != "my-company" {
		t.Errorf("slug in response: want my-company, got %v", resp["slug"])
	}
}

func TestOrgHandler_Create_InvalidBody(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.Use(withAccountID(10))
	r.POST("/organizations", h.Create)

	// Missing required "slug" field — binding validation should fail.
	w := postJSON(r, "/organizations", map[string]string{
		"name": "No Slug Corp",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", w.Code)
	}
}

func TestOrgHandler_CreateAPIKey_ReturnsRawKey(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	// Seed an org with account 20 as owner.
	_ = store.Create(ctx, &entity.Organization{Name: "Key Org", Slug: "key-org", Status: "active", Plan: "free", OwnerAccountID: 20})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 20, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(20))
	r.POST("/organizations/:id/api-keys", h.CreateAPIKey)

	w := postJSON(r, "/organizations/1/api-keys", map[string]string{
		"name": "CI Key",
	})

	if w.Code != http.StatusCreated {
		t.Errorf("status: want 201, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	rawKey, ok := resp["raw_key"].(string)
	if !ok || rawKey == "" {
		t.Error("raw_key must be present and non-empty in response")
	}
	// Verify key_hash is not exposed (key object should not have it exposed as non-empty string).
	keyObj, _ := resp["key"].(map[string]any)
	if keyObj != nil {
		if h, exists := keyObj["key_hash"]; exists && h != "" {
			t.Error("key_hash must not be exposed in API response")
		}
	}
}

func TestOrgHandler_ResolveAPIKey_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	// Seed org and create a key via service (to get a valid hash).
	svc := app.NewOrganizationService(store)
	org, _ := svc.Create(ctx, "Resolve Org", "resolve-org", 30)
	rawKey, _, _ := svc.CreateAPIKey(ctx, org.ID, 30, "test-key")

	r := testRouter()
	r.POST("/orgs/resolve-api-key", h.ResolveAPIKey)

	w := postJSON(r, "/orgs/resolve-api-key", map[string]string{
		"raw_key": rawKey,
	})

	if w.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d — body: %s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_ResolveAPIKey_InvalidKey(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.POST("/orgs/resolve-api-key", h.ResolveAPIKey)

	w := postJSON(r, "/orgs/resolve-api-key", map[string]string{
		"raw_key": "this-key-does-not-exist-in-store",
	})

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", w.Code)
	}
}

// ---------- ListMine ----------

func TestOrgHandler_ListMine_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(1))
	r.GET("/organizations", h.ListMine)

	req := httptest.NewRequest(http.MethodGet, "/organizations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data, ok := resp["data"].([]any)
	if !ok || len(data) != 1 {
		t.Errorf("expected 1 org, got %v", resp["data"])
	}
}

func TestOrgHandler_ListMine_Empty(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.Use(withAccountID(999))
	r.GET("/organizations", h.ListMine)

	req := httptest.NewRequest(http.MethodGet, "/organizations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

// ---------- Get ----------

func TestOrgHandler_Get_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(1))
	r.GET("/organizations/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/organizations/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_Get_NotFound(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.Use(withAccountID(1))
	r.GET("/organizations/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/organizations/999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Org not found → service returns nil → 403 or 404
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 403 or 404", w.Code)
	}
}

func TestOrgHandler_Get_NotMember(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(99)) // not a member
	r.GET("/organizations/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/organizations/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", w.Code)
	}
}

func TestOrgHandler_Get_InvalidID(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.Use(withAccountID(1))
	r.GET("/organizations/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/organizations/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// ---------- AddMember ----------

func TestOrgHandler_AddMember_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(1))
	r.POST("/organizations/:id/members", h.AddMember)

	w := postJSON(r, "/organizations/1/members", map[string]any{
		"account_id": 2,
		"role":       "member",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_AddMember_MissingBody(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(1))
	r.POST("/organizations/:id/members", h.AddMember)

	w := postJSON(r, "/organizations/1/members", map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

// ---------- RemoveMember ----------

func TestOrgHandler_RemoveMember_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 2, Role: "member"})

	r := testRouter()
	r.Use(withAccountID(1))
	r.DELETE("/organizations/:id/members/:uid", h.RemoveMember)

	req := httptest.NewRequest(http.MethodDelete, "/organizations/1/members/2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// ---------- ListAPIKeys ----------

func TestOrgHandler_ListAPIKeys_OK(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.GET("/organizations/:id/api-keys", h.ListAPIKeys)

	req := httptest.NewRequest(http.MethodGet, "/organizations/1/api-keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

// ---------- RevokeAPIKey ----------

func TestOrgHandler_RevokeAPIKey_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()

	svc := app.NewOrganizationService(store)
	_, _ = svc.Create(ctx, "RevokeOrg", "revoke-org", 1)
	_, key, _ := svc.CreateAPIKey(ctx, 1, 1, "to-revoke")

	r := testRouter()
	r.Use(withAccountID(1))
	r.DELETE("/organizations/:id/api-keys/:kid", h.RevokeAPIKey)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/organizations/1/api-keys/%d", key.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body=%s", w.Code, w.Body.String())
	}
}

// ---------- GetWallet ----------

func TestOrgHandler_GetWallet_OK(t *testing.T) {
	h, _ := makeOrgHandler()

	r := testRouter()
	r.GET("/organizations/:id/wallet", h.GetWallet)

	req := httptest.NewRequest(http.MethodGet, "/organizations/1/wallet", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

// ---------- AdminList ----------

func TestOrgHandler_AdminList_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})

	r := testRouter()
	r.GET("/admin/organizations", h.AdminList)

	req := httptest.NewRequest(http.MethodGet, "/admin/organizations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

// ---------- AdminUpdateStatus ----------

func TestOrgHandler_AdminUpdateStatus_OK(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})

	r := testRouter()
	r.PATCH("/admin/organizations/:id", h.AdminUpdateStatus)

	w := patchJSON(r, "/admin/organizations/1", map[string]string{"status": "suspended"})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_AdminUpdateStatus_InvalidStatus(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	_ = store.Create(ctx, &entity.Organization{Name: "Org1", Slug: "org1", Status: "active", Plan: "free", OwnerAccountID: 1})

	r := testRouter()
	r.PATCH("/admin/organizations/:id", h.AdminUpdateStatus)

	w := patchJSON(r, "/admin/organizations/1", map[string]string{"status": "invalid"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func patchJSON(r http.Handler, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- Additional edge-case tests for invalid-ID paths ----------

func TestOrgHandler_Create_ServiceError(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.Use(withAccountID(1))
	r.POST("/organizations", h.Create)

	// Slug too short → service validation error → 400.
	w := postJSON(r, "/organizations", map[string]string{"name": "X", "slug": "ab"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid slug)", w.Code)
	}
}

func TestOrgHandler_AddMember_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.Use(withAccountID(1))
	r.POST("/organizations/:id/members", h.AddMember)

	w := postJSON(r, "/organizations/bad/members", map[string]any{"account_id": 2})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_AddMember_Forbidden(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	_ = store.Create(ctx, &entity.Organization{Name: "Org", Slug: "org-af", Status: "active", Plan: "free", OwnerAccountID: 1})
	_ = store.AddMember(ctx, &entity.OrgMember{OrgID: 1, AccountID: 1, Role: "owner"})

	r := testRouter()
	r.Use(withAccountID(99)) // not a member
	r.POST("/organizations/:id/members", h.AddMember)

	w := postJSON(r, "/organizations/1/members", map[string]any{"account_id": 55})
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403 (non-member cannot add)", w.Code)
	}
}

func TestOrgHandler_RemoveMember_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.Use(withAccountID(1))
	r.DELETE("/organizations/:id/members/:uid", h.RemoveMember)

	req := httptest.NewRequest(http.MethodDelete, "/organizations/bad/members/2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_RemoveMember_InvalidUID(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	_ = store.Create(ctx, &entity.Organization{Name: "Org", Slug: "org-rmuid", Status: "active", Plan: "free", OwnerAccountID: 1})

	r := testRouter()
	r.Use(withAccountID(1))
	r.DELETE("/organizations/:id/members/:uid", h.RemoveMember)

	req := httptest.NewRequest(http.MethodDelete, "/organizations/1/members/bad", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid uid)", w.Code)
	}
}

func TestOrgHandler_ListAPIKeys_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.GET("/organizations/:id/api-keys", h.ListAPIKeys)

	req := httptest.NewRequest(http.MethodGet, "/organizations/bad/api-keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_CreateAPIKey_MissingName(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	svc := app.NewOrganizationService(store)
	org, _ := svc.Create(ctx, "Org", "org-ck-noname", 1)

	r := testRouter()
	r.Use(withAccountID(1))
	r.POST("/organizations/:id/api-keys", h.CreateAPIKey)

	w := postJSON(r, fmt.Sprintf("/organizations/%d/api-keys", org.ID), map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (missing name)", w.Code)
	}
}

func TestOrgHandler_CreateAPIKey_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.Use(withAccountID(1))
	r.POST("/organizations/:id/api-keys", h.CreateAPIKey)

	w := postJSON(r, "/organizations/bad/api-keys", map[string]string{"name": "k"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_RevokeAPIKey_InvalidKeyID(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	svc := app.NewOrganizationService(store)
	org, _ := svc.Create(ctx, "Org", "org-revkey-kid", 1)

	r := testRouter()
	r.Use(withAccountID(1))
	r.DELETE("/organizations/:id/api-keys/:kid", h.RevokeAPIKey)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/organizations/%d/api-keys/bad", org.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid key id)", w.Code)
	}
}

func TestOrgHandler_RevokeAPIKey_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.Use(withAccountID(1))
	r.DELETE("/organizations/:id/api-keys/:kid", h.RevokeAPIKey)

	req := httptest.NewRequest(http.MethodDelete, "/organizations/bad/api-keys/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_GetWallet_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.GET("/organizations/:id/wallet", h.GetWallet)

	req := httptest.NewRequest(http.MethodGet, "/organizations/bad/wallet", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_ResolveAPIKey_MissingRawKey(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.POST("/orgs/resolve-api-key", h.ResolveAPIKey)

	w := postJSON(r, "/orgs/resolve-api-key", map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (missing raw_key)", w.Code)
	}
}

func TestOrgHandler_AdminUpdateStatus_InvalidOrgID(t *testing.T) {
	h, _ := makeOrgHandler()
	r := testRouter()
	r.PATCH("/admin/organizations/:id", h.AdminUpdateStatus)

	w := patchJSON(r, "/admin/organizations/bad", map[string]string{"status": "active"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid org id)", w.Code)
	}
}

func TestOrgHandler_AdminUpdateStatus_MissingStatus(t *testing.T) {
	h, store := makeOrgHandler()
	ctx := context.Background()
	_ = store.Create(ctx, &entity.Organization{Name: "Org", Slug: "org-nostatus", Status: "active", Plan: "free", OwnerAccountID: 1})

	r := testRouter()
	r.PATCH("/admin/organizations/:id", h.AdminUpdateStatus)

	w := patchJSON(r, "/admin/organizations/1", map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (missing status)", w.Code)
	}
}

// ---------- error-path tests for OrgHandler ----------

// errListOrgStoreH overrides store list operations to return errors.
type errListOrgStoreH struct {
	mockOrgStoreH
}

func (s *errListOrgStoreH) ListByAccountID(_ context.Context, _ int64) ([]entity.Organization, error) {
	return nil, fmt.Errorf("db error")
}

func (s *errListOrgStoreH) ListAll(_ context.Context, _, _ int) ([]entity.Organization, error) {
	return nil, fmt.Errorf("db error")
}

func TestOrgHandler_ListMine_Error(t *testing.T) {
	store := &errListOrgStoreH{*newMockOrgStoreH()}
	svc := app.NewOrganizationService(store)
	h := NewOrganizationHandler(svc)
	r := testRouter()
	r.GET("/api/v1/organizations", withAccountID(1), h.ListMine)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/organizations", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_AdminList_Error(t *testing.T) {
	store := &errListOrgStoreH{*newMockOrgStoreH()}
	svc := app.NewOrganizationService(store)
	h := NewOrganizationHandler(svc)
	r := testRouter()
	r.GET("/admin/v1/organizations", h.AdminList)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/v1/organizations", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}
