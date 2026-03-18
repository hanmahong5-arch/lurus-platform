package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	router := BuildRouter(Deps{
		Notifications: &NotificationHandler{},
		Templates:     &TemplateHandler{},
		InternalKey:   "test-key",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /health returned %d, want %d", w.Code, http.StatusOK)
	}
}

func TestInternalKeyAuth_Missing(t *testing.T) {
	router := BuildRouter(Deps{
		Notifications: &NotificationHandler{},
		Templates:     &TemplateHandler{},
		InternalKey:   "test-key",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/v1/templates", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /admin/v1/templates without key returned %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestInternalKeyAuth_Valid(t *testing.T) {
	router := BuildRouter(Deps{
		Notifications: &NotificationHandler{},
		Templates:     &TemplateHandler{svc: nil},
		InternalKey:   "test-key",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/v1/templates", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	router.ServeHTTP(w, req)

	// Will fail with nil svc, but should pass auth (not 401)
	if w.Code == http.StatusUnauthorized {
		t.Errorf("GET /admin/v1/templates with valid key returned 401")
	}
}

func TestDevAccountIDMiddleware_Missing(t *testing.T) {
	// When JWT is nil, the dev middleware requires X-Account-ID header.
	router := BuildRouter(Deps{
		Notifications: &NotificationHandler{},
		Templates:     &TemplateHandler{},
		InternalKey:   "test-key",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/notifications without X-Account-ID returned %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestPreferenceRoutes_Registered(t *testing.T) {
	router := BuildRouter(Deps{
		Notifications: &NotificationHandler{},
		Templates:     &TemplateHandler{},
		Preferences:   &PreferenceHandler{},
		InternalKey:   "test-key",
	})

	// Preference GET should return 401 (no auth header) not 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/notifications/preferences", nil)
	router.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Errorf("GET /api/v1/notifications/preferences returned 404, route not registered")
	}
}
