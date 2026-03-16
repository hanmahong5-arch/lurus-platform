package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// errProductListStore overrides list methods to return errors.
type errProductListStore struct {
	mockPlanStore
}

func (s *errProductListStore) ListActive(_ context.Context) ([]entity.Product, error) {
	return nil, fmt.Errorf("db error")
}

func (s *errProductListStore) ListPlans(_ context.Context, _ string) ([]entity.ProductPlan, error) {
	return nil, fmt.Errorf("db error")
}

func TestProductHandler_ListProducts(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.GET("/api/v1/products", h.ListProducts)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestProductHandler_ListPlans(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.GET("/api/v1/products/:id/plans", h.ListPlans)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/lurus_api/plans", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestProductHandler_AdminCreateProduct(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.POST("/admin/v1/products", h.AdminCreateProduct)

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"valid", `{"id":"test-prod","name":"Test","description":"desc"}`, http.StatusCreated},
		{"invalid_json", `{bad`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/v1/products", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestProductHandler_AdminUpdateProduct_NotFound(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.PUT("/admin/v1/products/:id", h.AdminUpdateProduct)

	body := `{"name":"Updated"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/products/nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestProductHandler_AdminUpdatePlan_InvalidID(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.PUT("/admin/v1/plans/:id", h.AdminUpdatePlan)

	body := `{"name":"Updated"}`
	tests := []struct {
		name   string
		id     string
		status int
	}{
		{"invalid_id", "abc", http.StatusBadRequest},
		{"not_found", "999", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/admin/v1/plans/"+tt.id, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestProductHandler_AdminCreatePlan(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.POST("/admin/v1/products/:id/plans", h.AdminCreatePlan)

	body, _ := json.Marshal(map[string]interface{}{
		"code":          "pro",
		"name":          "Pro Plan",
		"price_cny":     99.0,
		"billing_cycle": "monthly",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/products/lurus_api/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestProductHandler_ListProducts_Error(t *testing.T) {
	store := &errProductListStore{*newMockPlanStore()}
	h := NewProductHandler(app.NewProductService(store))
	r := testRouter()
	r.GET("/api/v1/products", h.ListProducts)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestProductHandler_ListPlans_Error(t *testing.T) {
	store := &errProductListStore{*newMockPlanStore()}
	h := NewProductHandler(app.NewProductService(store))
	r := testRouter()
	r.GET("/api/v1/products/:id/plans", h.ListPlans)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/lurus_api/plans", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestProductHandler_AdminUpdateProduct_Success(t *testing.T) {
	store := newMockPlanStore()
	store.products["prod-x"] = &entity.Product{ID: "prod-x", Name: "Old Name"}
	h := NewProductHandler(app.NewProductService(store))
	r := testRouter()
	r.PUT("/admin/v1/products/:id", h.AdminUpdateProduct)

	body := `{"name":"New Name","description":"updated"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/products/prod-x", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestProductHandler_AdminUpdateProduct_InvalidJSON(t *testing.T) {
	store := newMockPlanStore()
	store.products["prod-x"] = &entity.Product{ID: "prod-x", Name: "Test"}
	h := NewProductHandler(app.NewProductService(store))
	r := testRouter()
	r.PUT("/admin/v1/products/:id", h.AdminUpdateProduct)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/products/prod-x", bytes.NewBufferString("{bad json}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestProductHandler_AdminUpdatePlan_Success(t *testing.T) {
	store := newMockPlanStore()
	store.plans[42] = &entity.ProductPlan{ID: 42, ProductID: "lurus_api", Code: "pro", PriceCNY: 99.0}
	h := NewProductHandler(app.NewProductService(store))
	r := testRouter()
	r.PUT("/admin/v1/plans/:id", h.AdminUpdatePlan)

	body := `{"name":"Updated Plan","price_cny":199.0}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/plans/42", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestProductHandler_AdminUpdatePlan_InvalidJSON(t *testing.T) {
	store := newMockPlanStore()
	store.plans[7] = &entity.ProductPlan{ID: 7, ProductID: "lurus_api", Code: "basic"}
	h := NewProductHandler(app.NewProductService(store))
	r := testRouter()
	r.PUT("/admin/v1/plans/:id", h.AdminUpdatePlan)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/plans/7", bytes.NewBufferString("{bad json}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestProductHandler_AdminCreatePlan_InvalidJSON(t *testing.T) {
	h := NewProductHandler(makeProductService())
	r := testRouter()
	r.POST("/admin/v1/products/:id/plans", h.AdminCreatePlan)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/products/lurus_api/plans", bytes.NewBufferString("{bad json}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}
