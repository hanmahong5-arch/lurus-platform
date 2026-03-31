package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── mock service key admin ────────────────────────────────────────────────

type mockServiceKeyAdmin struct {
	keys    []entity.ServiceAPIKey
	nextID  int64
	createErr error
	listErr   error
	revokeErr error
}

func newMockServiceKeyAdmin() *mockServiceKeyAdmin {
	return &mockServiceKeyAdmin{nextID: 1}
}

func (m *mockServiceKeyAdmin) Create(_ context.Context, key *entity.ServiceAPIKey) error {
	if m.createErr != nil {
		return m.createErr
	}
	key.ID = m.nextID
	m.nextID++
	m.keys = append(m.keys, *key)
	return nil
}

func (m *mockServiceKeyAdmin) ListAll(_ context.Context) ([]entity.ServiceAPIKey, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.keys, nil
}

func (m *mockServiceKeyAdmin) Revoke(_ context.Context, id int64) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	for i := range m.keys {
		if m.keys[i].ID == id {
			if m.keys[i].Status == entity.ServiceKeyRevoked {
				return fmt.Errorf("service key %d already revoked", id)
			}
			m.keys[i].Status = entity.ServiceKeyRevoked
			return nil
		}
	}
	return fmt.Errorf("service key %d not found or already revoked", id)
}

// ── CreateServiceKey tests ────────────────────────────────────────────────

func TestAdminServiceKey_Create_Valid(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"service_name": "forge",
		"description":  "Test key",
		"scopes":       []string{"account:read", "entitlement"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Verify key format: sk-<service>-<hex>
	key, _ := resp["key"].(string)
	if !strings.HasPrefix(key, "sk-forge-") {
		t.Errorf("key = %s, want prefix sk-forge-", key)
	}
	if len(key) < 20 {
		t.Errorf("key too short: %d chars", len(key))
	}

	// Verify response fields.
	if resp["service_name"] != "forge" {
		t.Errorf("service_name = %v, want forge", resp["service_name"])
	}
	if resp["rate_limit_rpm"] != float64(1000) {
		t.Errorf("rate_limit_rpm = %v, want 1000 (default)", resp["rate_limit_rpm"])
	}
}

func TestAdminServiceKey_Create_MissingName(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"scopes": []string{"account:read"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminServiceKey_Create_MissingScopes(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"service_name": "api",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminServiceKey_Create_EmptyScopes(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"service_name": "api",
		"scopes":       []string{},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminServiceKey_Create_InvalidScope(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"service_name": "api",
		"scopes":       []string{"invalid:scope"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminServiceKey_Create_CustomRateLimit(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"service_name":   "api",
		"scopes":         []string{"account:read"},
		"rate_limit_rpm": 500,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["rate_limit_rpm"] != float64(500) {
		t.Errorf("rate_limit_rpm = %v, want 500", resp["rate_limit_rpm"])
	}
}

func TestAdminServiceKey_Create_StoreError(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	svc.createErr = fmt.Errorf("db error")
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.POST("/admin/v1/service-keys", h.CreateServiceKey)

	body, _ := json.Marshal(map[string]any{
		"service_name": "api",
		"scopes":       []string{"account:read"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/service-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// ── ListServiceKeys tests ─────────────────────────────────────────────────

func TestAdminServiceKey_List_Empty(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.GET("/admin/v1/service-keys", h.ListServiceKeys)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/service-keys", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	keys, _ := resp["keys"].([]any)
	if len(keys) != 0 {
		t.Errorf("keys length = %d, want 0", len(keys))
	}
}

func TestAdminServiceKey_List_MultipleKeys(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	svc.keys = []entity.ServiceAPIKey{
		{ID: 1, ServiceName: "forge", KeyPrefix: "sk-forge"},
		{ID: 2, ServiceName: "api", KeyPrefix: "sk-api"},
	}
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.GET("/admin/v1/service-keys", h.ListServiceKeys)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/service-keys", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	keys, _ := resp["keys"].([]any)
	if len(keys) != 2 {
		t.Errorf("keys length = %d, want 2", len(keys))
	}
}

func TestAdminServiceKey_List_StoreError(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	svc.listErr = fmt.Errorf("db error")
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.GET("/admin/v1/service-keys", h.ListServiceKeys)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/service-keys", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// ── RevokeServiceKey tests ────────────────────────────────────────────────

func TestAdminServiceKey_Revoke_Valid(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	svc.keys = []entity.ServiceAPIKey{
		{ID: 1, ServiceName: "forge", Status: entity.ServiceKeyActive},
	}
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.DELETE("/admin/v1/service-keys/:id", h.RevokeServiceKey)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/service-keys/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["revoked"] != true {
		t.Errorf("revoked = %v, want true", resp["revoked"])
	}
}

func TestAdminServiceKey_Revoke_InvalidID(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.DELETE("/admin/v1/service-keys/:id", h.RevokeServiceKey)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/service-keys/abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminServiceKey_Revoke_NotFound(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.DELETE("/admin/v1/service-keys/:id", h.RevokeServiceKey)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/service-keys/999", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (key not found)", w.Code)
	}
}

func TestAdminServiceKey_Revoke_AlreadyRevoked(t *testing.T) {
	svc := newMockServiceKeyAdmin()
	svc.keys = []entity.ServiceAPIKey{
		{ID: 1, ServiceName: "forge", Status: entity.ServiceKeyRevoked},
	}
	h := NewAdminServiceKeyHandler(svc)
	r := testRouter()
	r.DELETE("/admin/v1/service-keys/:id", h.RevokeServiceKey)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/service-keys/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (already revoked)", w.Code)
	}
}
