package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
)

// TestAppsAdmin_RotateSecret_NoReconciler covers the 503 path: when the
// reconciler couldn't be wired (no apps.yaml, not in K8s, no PAT) the
// endpoint must say so explicitly rather than NPE.
func TestAppsAdmin_RotateSecret_NoReconciler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewAppsAdminHandler("/non/existent/apps.yaml", nil, nil)
	r := gin.New()
	r.POST("/admin/v1/apps/:name/:env/rotate-secret", h.RotateSecret)

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/apps/admin/prod/rotate-secret", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "rotation_unavailable" {
		t.Errorf("error code = %v, want rotation_unavailable", body["error"])
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "reconciler is not wired") {
		t.Errorf("message should mention reconciler wiring, got %q", msg)
	}
}
