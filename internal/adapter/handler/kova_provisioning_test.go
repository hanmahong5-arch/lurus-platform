package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/kovaprov"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ---------- in-memory doubles (mirror app package fakes; lighter shape) ----------

type kovaHOrgs struct {
	mu      sync.Mutex
	orgs    map[int64]*entity.Organization
	members map[int64]map[int64]*entity.OrgMember
}

func newKovaHOrgs() *kovaHOrgs {
	return &kovaHOrgs{
		orgs:    map[int64]*entity.Organization{},
		members: map[int64]map[int64]*entity.OrgMember{},
	}
}

func (f *kovaHOrgs) GetByID(_ context.Context, id int64) (*entity.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.orgs[id]
	if !ok {
		return nil, nil
	}
	cp := *o
	return &cp, nil
}
func (f *kovaHOrgs) GetMember(_ context.Context, orgID, accountID int64) (*entity.OrgMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.members[orgID] == nil {
		return nil, nil
	}
	m, ok := f.members[orgID][accountID]
	if !ok {
		return nil, nil
	}
	cp := *m
	return &cp, nil
}

type kovaHStore struct {
	mu     sync.Mutex
	rows   map[string]*entity.OrgService
	usages []*entity.UsageEvent
}

func newKovaHStore() *kovaHStore {
	return &kovaHStore{rows: map[string]*entity.OrgService{}}
}

func (f *kovaHStore) Get(_ context.Context, orgID int64, service string) (*entity.OrgService, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[service+"/"+strInt(orgID)]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}
func (f *kovaHStore) Upsert(_ context.Context, s *entity.OrgService) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.rows[s.Service+"/"+strInt(s.OrgID)] = &cp
	return nil
}
func (f *kovaHStore) CreateUsageEvent(_ context.Context, ev *entity.UsageEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *ev
	cp.ID = int64(len(f.usages) + 1)
	f.usages = append(f.usages, &cp)
	ev.ID = cp.ID
	return nil
}

func strInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(b[i:])
	if neg {
		return "-" + s
	}
	return s
}

type kovaHProvisioner struct {
	resp *kovaprov.ProvisionResponse
	err  error
}

func (p *kovaHProvisioner) Provision(_ context.Context, _ kovaprov.ProvisionRequest) (*kovaprov.ProvisionResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.resp, nil
}
func (p *kovaHProvisioner) IsMock() bool { return true }

func makeKovaH() (*KovaProvisioningHandler, *kovaHOrgs, *kovaHStore, *kovaHProvisioner) {
	orgs := newKovaHOrgs()
	store := newKovaHStore()
	prov := &kovaHProvisioner{
		resp: &kovaprov.ProvisionResponse{
			TesterName: "acme",
			BaseURL:    "http://kova-mock.local",
			AdminKey:   "sk-kova-aaaa1111bbbb2222cccc3333dddd4444",
			Port:       -1,
		},
	}
	svc := app.NewKovaProvisioningService(orgs, store, store, prov)
	return NewKovaProvisioningHandler(svc), orgs, store, prov
}

// ---------- tests ----------

func TestKovaH_Provision_Success(t *testing.T) {
	h, orgs, _, _ := makeKovaH()
	orgs.orgs[1] = &entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100}

	r := testRouter()
	r.Use(withServiceScopes(entity.ScopeOrgProvision))
	r.POST("/internal/v1/orgs/:id/services/kova-tester", h.ProvisionKovaTester)

	w := postKovaJSON(r, "/internal/v1/orgs/1/services/kova-tester", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["admin_key"] == nil || resp["admin_key"].(string) == "" {
		t.Error("admin_key must be returned exactly once on first provision")
	}
	if resp["status"] != "active" {
		t.Errorf("status=%v want active", resp["status"])
	}
	if resp["mock_mode"] != true {
		t.Errorf("expected mock_mode=true, got %v", resp["mock_mode"])
	}
}

func TestKovaH_Provision_RequiresScope(t *testing.T) {
	h, orgs, _, _ := makeKovaH()
	orgs.orgs[1] = &entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100}

	r := testRouter()
	// No scope injected.
	r.POST("/internal/v1/orgs/:id/services/kova-tester", h.ProvisionKovaTester)
	w := postKovaJSON(r, "/internal/v1/orgs/1/services/kova-tester", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestKovaH_Provision_OrgNotFound(t *testing.T) {
	h, _, _, _ := makeKovaH()

	r := testRouter()
	r.Use(withServiceScopes(entity.ScopeOrgProvision))
	r.POST("/internal/v1/orgs/:id/services/kova-tester", h.ProvisionKovaTester)

	w := postKovaJSON(r, "/internal/v1/orgs/999/services/kova-tester", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestKovaH_GetKova_OK(t *testing.T) {
	h, orgs, store, _ := makeKovaH()
	orgs.orgs[1] = &entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100}
	orgs.members[1] = map[int64]*entity.OrgMember{
		100: {OrgID: 1, AccountID: 100, Role: "owner"},
	}
	_ = store.Upsert(context.Background(), &entity.OrgService{
		OrgID: 1, Service: entity.OrgServiceKova, Status: entity.OrgServiceStatusActive,
		BaseURL: "http://r6:3015", KeyPrefix: "sk-kova-",
	})

	r := testRouter()
	r.Use(withAccountID(100))
	r.GET("/api/v1/orgs/:id/services/kova", h.GetKova)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/1/services/kova", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["base_url"] != "http://r6:3015" {
		t.Errorf("base_url mismatch: %v", resp["base_url"])
	}
	if resp["admin_key"] != nil {
		t.Error("GET must NEVER return raw admin_key")
	}
}

func TestKovaH_GetKova_PermissionDenied(t *testing.T) {
	h, orgs, store, _ := makeKovaH()
	orgs.orgs[1] = &entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100}
	_ = store.Upsert(context.Background(), &entity.OrgService{
		OrgID: 1, Service: entity.OrgServiceKova, Status: entity.OrgServiceStatusActive,
	})

	r := testRouter()
	r.Use(withAccountID(999)) // not a member
	r.GET("/api/v1/orgs/:id/services/kova", h.GetKova)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/1/services/kova", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestKovaH_GetKova_NotProvisioned(t *testing.T) {
	h, orgs, _, _ := makeKovaH()
	orgs.orgs[1] = &entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100}
	orgs.members[1] = map[int64]*entity.OrgMember{
		100: {OrgID: 1, AccountID: 100, Role: "owner"},
	}

	r := testRouter()
	r.Use(withAccountID(100))
	r.GET("/api/v1/orgs/:id/services/kova", h.GetKova)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/1/services/kova", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestKovaH_ReportUsage_OK(t *testing.T) {
	h, _, store, _ := makeKovaH()

	r := testRouter()
	r.Use(withServiceScopes(entity.ScopeUsageReport))
	r.POST("/internal/v1/usage/report/kova", h.ReportKovaUsage)

	body := map[string]any{
		"org_id":      42,
		"tester_name": "acme",
		"agent_id":    "hello",
		"tokens_in":   100,
		"tokens_out":  200,
		"cost_micros": 5000,
	}
	w := postKovaJSON(r, "/internal/v1/usage/report/kova", body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(store.usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(store.usages))
	}
	if store.usages[0].TokensIn != 100 || store.usages[0].TokensOut != 200 || store.usages[0].CostMicros != 5000 {
		t.Errorf("usage event fields mismatch: %+v", store.usages[0])
	}
	if store.usages[0].Service != entity.OrgServiceKova {
		t.Errorf("default service should be kova, got %q", store.usages[0].Service)
	}
}

func TestKovaH_ReportUsage_RequiresScope(t *testing.T) {
	h, _, _, _ := makeKovaH()
	r := testRouter()
	r.POST("/internal/v1/usage/report/kova", h.ReportKovaUsage)
	w := postKovaJSON(r, "/internal/v1/usage/report/kova", map[string]any{"org_id": 1})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestKovaH_ReportUsage_RejectsBadBody(t *testing.T) {
	h, _, _, _ := makeKovaH()
	r := testRouter()
	r.Use(withServiceScopes(entity.ScopeUsageReport))
	r.POST("/internal/v1/usage/report/kova", h.ReportKovaUsage)

	w := postKovaJSON(r, "/internal/v1/usage/report/kova", map[string]any{
		"org_id":     0, // missing/zero
		"tokens_in":  -10,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad body, got %d body=%s", w.Code, w.Body.String())
	}
}

// ---------- end-to-end provision → get round-trip ----------
//
// Exercises all four "must do" endpoints in sequence with one handler set:
//  1. POST /internal/v1/orgs/:id/services/kova-tester (provision)
//  2. GET  /api/v1/orgs/:id/services/kova            (tenant view)
//  3. POST /internal/v1/usage/report/kova            (worker callback)
//
// /api/v1/organizations is exercised in organization_test.go and is unchanged
// by this slice — we don't double-cover it here.
func TestKovaH_FullRoundTrip(t *testing.T) {
	h, orgs, store, _ := makeKovaH()
	orgs.orgs[1] = &entity.Organization{ID: 1, Slug: "acme", OwnerAccountID: 100}
	orgs.members[1] = map[int64]*entity.OrgMember{
		100: {OrgID: 1, AccountID: 100, Role: "owner"},
	}

	// 1) provision
	rProv := testRouter()
	rProv.Use(withServiceScopes(entity.ScopeOrgProvision))
	rProv.POST("/internal/v1/orgs/:id/services/kova-tester", h.ProvisionKovaTester)
	wProv := postKovaJSON(rProv, "/internal/v1/orgs/1/services/kova-tester", nil)
	if wProv.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", wProv.Code, wProv.Body.String())
	}
	var provBody map[string]any
	_ = json.Unmarshal(wProv.Body.Bytes(), &provBody)
	rawKey, _ := provBody["admin_key"].(string)
	if rawKey == "" {
		t.Fatal("admin_key missing from provision response")
	}

	// 2) tenant GET — must surface base_url, must NOT surface admin_key
	rGet := testRouter()
	rGet.Use(withAccountID(100))
	rGet.GET("/api/v1/orgs/:id/services/kova", h.GetKova)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/1/services/kova", nil)
	wGet := httptest.NewRecorder()
	rGet.ServeHTTP(wGet, req)
	if wGet.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", wGet.Code, wGet.Body.String())
	}
	var getBody map[string]any
	_ = json.Unmarshal(wGet.Body.Bytes(), &getBody)
	if getBody["base_url"] == nil || getBody["base_url"] == "" {
		t.Error("get response should carry base_url")
	}
	if getBody["admin_key"] != nil {
		t.Error("get response leaked admin_key")
	}

	// 3) usage report from a worker
	rUsage := testRouter()
	rUsage.Use(withServiceScopes(entity.ScopeUsageReport))
	rUsage.POST("/internal/v1/usage/report/kova", h.ReportKovaUsage)
	wUsage := postKovaJSON(rUsage, "/internal/v1/usage/report/kova", map[string]any{
		"org_id":      1,
		"tester_name": "acme",
		"agent_id":    "hello",
		"tokens_in":   100,
		"tokens_out":  200,
		"cost_micros": 1234,
	})
	if wUsage.Code != http.StatusAccepted {
		t.Fatalf("usage status=%d body=%s", wUsage.Code, wUsage.Body.String())
	}
	if len(store.usages) != 1 {
		t.Errorf("expected 1 usage record, got %d", len(store.usages))
	}
}

// ---------- helpers ----------

func postKovaJSON(r http.Handler, path string, body any) *httptest.ResponseRecorder {
	var buf *bytes.Reader
	if body == nil {
		buf = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req := httptest.NewRequest(http.MethodPost, path, buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
