package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/newapi_sync"
)

// LLMTokenHandler tests don't drive a real Module — that's covered in
// newapi_sync. Here we cover the auth+status-code branches the handler
// owns: unconfigured (503), unauth (401), happy/fail paths run via the
// module-level tests.

func TestLLMTokenHandler_NoSessionSecret_503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewLLMTokenHandler("", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account/me/llm-token", nil)
	h.Get(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (no session secret), got %d", w.Code)
	}
}

func TestLLMTokenHandler_NoModule_503WithExplicitReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewLLMTokenHandler("secret", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account/me/llm-token", nil)
	h.Get(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	if !contains(w.Body.String(), "newapi_sync_disabled") {
		t.Errorf("expected machine-readable error code in body, got: %s", w.Body.String())
	}
}

func TestLLMTokenHandler_NoToken_401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// pass non-nil module so we get past the 503 gate; we still expect 401
	// because no auth header / cookie present.
	mod := &newapi_sync.Module{} // zero-value won't be called — auth check happens first
	h := NewLLMTokenHandler("secret", mod)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/account/me/llm-token", nil)
	h.Get(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on missing token, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestLLMTokenHandler_BadToken_401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mod := &newapi_sync.Module{}
	h := NewLLMTokenHandler("secret", mod)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/me/llm-token", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	c.Request = req
	h.Get(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on invalid token, got %d", w.Code)
	}
}

// `contains` is defined in respond_helpers_test.go — same package so reuse.
