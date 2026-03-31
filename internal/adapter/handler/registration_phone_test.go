package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── SendPhoneCode tests ───────────────────────────────────────────────────

func TestRegistration_SendPhoneCode_MissingPhone(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/send-phone-code", withAccountID(1), h.SendPhoneCode)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/send-phone-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegistration_SendPhoneCode_NoAuth(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	// No withAccountID middleware — simulates unauthenticated user.
	r.POST("/api/v1/account/me/send-phone-code", h.SendPhoneCode)

	body, _ := json.Marshal(map[string]string{"phone": "13800138000"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/send-phone-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRegistration_SendPhoneCode_InvalidFormat(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/send-phone-code", withAccountID(1), h.SendPhoneCode)

	// "123" is too short to be a valid phone number.
	body, _ := json.Marshal(map[string]string{"phone": "123"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/send-phone-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// The RegistrationService.SendPhoneVerificationCode should return "invalid phone" error
	// which the handler maps to a validation error (400).
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (invalid phone format)", w.Code)
	}
}

func TestRegistration_SendPhoneCode_ValidPhone(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/send-phone-code", withAccountID(1), h.SendPhoneCode)

	// Valid phone number, but no SMS provider or Redis → service error.
	// The test validates the handler correctly routes to the service.
	body, _ := json.Marshal(map[string]string{"phone": "13800138000"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/send-phone-code", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Without Redis, the service will return an internal error (500).
	// This confirms the handler reaches the service layer.
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		t.Errorf("status = %d, should not be auth-related", w.Code)
	}
}

// ── VerifyPhone tests ─────────────────────────────────────────────────────

func TestRegistration_VerifyPhone_MissingPhone(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/verify-phone", withAccountID(1), h.VerifyPhone)

	body, _ := json.Marshal(map[string]string{"code": "123456"}) // missing phone
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/verify-phone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegistration_VerifyPhone_MissingCode(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/verify-phone", withAccountID(1), h.VerifyPhone)

	body, _ := json.Marshal(map[string]string{"phone": "13800138000"}) // missing code
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/verify-phone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegistration_VerifyPhone_NoAuth(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/verify-phone", h.VerifyPhone)

	body, _ := json.Marshal(map[string]string{"phone": "13800138000", "code": "123456"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/verify-phone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRegistration_VerifyPhone_InvalidPhone(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/account/me/verify-phone", withAccountID(1), h.VerifyPhone)

	body, _ := json.Marshal(map[string]string{"phone": "123", "code": "123456"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/me/verify-phone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (invalid phone)", w.Code)
	}
}

// ── SendSMSCode tests (unauthenticated) ───────────────────────────────────

func TestRegistration_SendSMSCode_Success(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/send-sms", h.SendSMSCode)

	body, _ := json.Marshal(map[string]string{"identifier": "nonexistent@test.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/send-sms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Even for nonexistent accounts, returns 200 (security: no enumeration).
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRegistration_SendSMSCode_MissingIdentifier(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-123")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/send-sms", h.SendSMSCode)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/send-sms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
