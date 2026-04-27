package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appsms "github.com/hanmahong5-arch/lurus-platform/internal/app/sms"
)

// mockSMSRelayUsecase is a test double for SMSRelayUsecaseIface.
type mockSMSRelayUsecase struct {
	err error
}

func (m *mockSMSRelayUsecase) SendOTP(_ context.Context, phone, code string) error {
	return m.err
}

// newSMSRelayTestRouter builds a Gin test router with internalKeyAuth bypassed.
func newSMSRelayTestRouter(h *SMSRelayHandler) *gin.Engine {
	r := testRouter()
	// Simulate the internalKeyAuth middleware having already run successfully
	// by injecting service_id and service_scopes into context.
	r.POST("/internal/v1/sms/relay", withAllScopes(), h.Relay)
	return r
}

// TestSMSRelayHandler_Relay_Success verifies valid payload returns 200.
func TestSMSRelayHandler_Relay_Success(t *testing.T) {
	h := NewSMSRelayHandler(&mockSMSRelayUsecase{err: nil})
	r := newSMSRelayTestRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"contextInfo": map[string]string{
			"recipient": "+8613800138000",
		},
		"templateData": map[string]string{
			"code": "382910",
		},
		"messageContent": "您的验证码是382910，5分钟内有效。",
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/sms/relay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("response status = %v, want ok", resp["status"])
	}
}

// TestSMSRelayHandler_Relay_NoAuth verifies missing Authorization header returns 401.
// This test exercises the router-level middleware by building a router WITHOUT the mock auth helper.
func TestSMSRelayHandler_Relay_NoAuth(t *testing.T) {
	const internalKey = "test-internal-key-value"
	h := NewSMSRelayHandler(&mockSMSRelayUsecase{err: nil})

	r := testRouter()
	// Build a minimal internalKeyAuth that checks the bearer token.
	r.POST("/internal/v1/sms/relay", func(c *gin.Context) {
		bearer := c.GetHeader("Authorization")
		if len(bearer) <= 7 || bearer[:7] != "Bearer " || bearer[7:] != internalKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized", "message": "Invalid service API key",
			})
			return
		}
		c.Next()
	}, h.Relay)

	body, _ := json.Marshal(map[string]interface{}{
		"contextInfo":  map[string]string{"recipient": "+8613800138000"},
		"templateData": map[string]string{"code": "123456"},
	})

	// No auth header.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/sms/relay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestSMSRelayHandler_Relay_InvalidPhone verifies malformed phone returns 400.
func TestSMSRelayHandler_Relay_InvalidPhone(t *testing.T) {
	h := NewSMSRelayHandler(&mockSMSRelayUsecase{err: appsms.ErrInvalidPhone})
	r := newSMSRelayTestRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"contextInfo":  map[string]string{"recipient": "13800138000"}, // missing +86
		"templateData": map[string]string{"code": "123456"},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/sms/relay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

// TestSMSRelayHandler_Relay_RateLimit verifies rate-limit errors from the usecase return 429.
func TestSMSRelayHandler_Relay_RateLimit(t *testing.T) {
	h := NewSMSRelayHandler(&mockSMSRelayUsecase{err: appsms.ErrRateLimit})
	r := newSMSRelayTestRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"contextInfo":  map[string]string{"recipient": "+8613800138000"},
		"templateData": map[string]string{"code": "123456"},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/sms/relay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429. body: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}

// TestSMSRelayHandler_Relay_MissingRecipient verifies missing recipient returns 400.
func TestSMSRelayHandler_Relay_MissingRecipient(t *testing.T) {
	h := NewSMSRelayHandler(&mockSMSRelayUsecase{err: nil})
	r := newSMSRelayTestRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"contextInfo":  map[string]string{}, // no recipient
		"templateData": map[string]string{"code": "123456"},
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/sms/relay", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}
