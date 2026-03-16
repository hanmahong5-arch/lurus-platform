package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	lurusemail "github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

const testRegSessionSecret = "test-session-secret-at-least-32-bytes!!"

// newZitadelTestServer creates an httptest server simulating Zitadel user creation.
func newZitadelTestServer(t *testing.T, statusCode int, userID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if userID != "" {
			json.NewEncoder(w).Encode(map[string]any{"userId": userID})
		} else {
			w.Write([]byte(`{"message":"error"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// makeRegistrationHandler constructs a RegistrationHandler with in-memory mocks.
func makeRegistrationHandler(t *testing.T, zitadelURL string) *RegistrationHandler {
	t.Helper()
	accounts := newMockAccountStore()
	wallets := newMockWalletStore()
	vip := newMockVIPStore()
	referral := app.NewReferralServiceWithCodes(accounts, wallets, &mockRedemptionCodeStore{})
	zc := zitadel.NewClient(zitadelURL, "test-pat")
	svc := app.NewRegistrationService(accounts, wallets, vip, referral, zc, testRegSessionSecret, lurusemail.NoopSender{}, sms.NoopSender{}, nil, sms.SMSConfig{})
	return NewRegistrationHandler(svc)
}

// jsonBody is a convenience helper to build a JSON request body.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

// TestRegistrationHandler_Register_Success verifies successful registration returns 201 with token.
func TestRegistrationHandler_Register_Success(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h01")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body := jsonBody(t, map[string]string{
		"username": "alice123",
		"password": "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201. body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] == "" || resp["token"] == nil {
		t.Error("response missing non-empty 'token'")
	}
}

// TestRegistrationHandler_Register_MissingUsername verifies 400 when username is absent.
func TestRegistrationHandler_Register_MissingUsername(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h02")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body := jsonBody(t, map[string]string{"password": "Password123!"}) // no username
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestRegistrationHandler_Register_WeakPassword verifies 400 for password shorter than 8 chars.
func TestRegistrationHandler_Register_WeakPassword(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h04")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body := jsonBody(t, map[string]string{
		"username": "bob123",
		"password": "short",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for weak password", w.Code)
	}
}

// TestRegistrationHandler_Register_ZitadelConflict verifies 409 when Zitadel reports conflict.
func TestRegistrationHandler_Register_ZitadelConflict(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusConflict, "")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body := jsonBody(t, map[string]string{
		"username": "carol123",
		"password": "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Zitadel conflict: status = %d, want 409", w.Code)
	}
}

// TestRegistrationHandler_Register_ZitadelDown verifies 500 when Zitadel returns error.
func TestRegistrationHandler_Register_ZitadelDown(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusInternalServerError, "")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body := jsonBody(t, map[string]string{"username": "dave123", "password": "Password123!"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Zitadel down: status = %d, want 500", w.Code)
	}
}

// TestRegistrationHandler_Register_InvalidJSON verifies 400 for malformed JSON body.
func TestRegistrationHandler_Register_InvalidJSON(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h06")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON: status = %d, want 400", w.Code)
	}
}

// TestRegistrationHandler_ForgotPassword_ReturnsOK verifies ForgotPassword always returns 200.
func TestRegistrationHandler_ForgotPassword_ReturnsOK(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h07")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/forgot-password", h.ForgotPassword)

	tests := []struct {
		identifier string
	}{
		{"unknown_user"},
		{"valid_user"},
	}
	for _, tt := range tests {
		body := jsonBody(t, map[string]string{"identifier": tt.identifier})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("ForgotPassword(%q): status = %d, want 200", tt.identifier, w.Code)
		}
	}
}

// TestRegistrationHandler_ForgotPassword_MissingIdentifier verifies 400 when identifier is absent.
func TestRegistrationHandler_ForgotPassword_MissingIdentifier(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h08")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/forgot-password", h.ForgotPassword)

	body := jsonBody(t, map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("missing identifier: status = %d, want 400", w.Code)
	}
}

// TestRegistrationHandler_ResetPassword_MissingFields verifies 400 when required fields are absent.
func TestRegistrationHandler_ResetPassword_MissingFields(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusCreated, "zid-h11")
	h := makeRegistrationHandler(t, srv.URL)

	r := testRouter()
	r.POST("/api/v1/auth/reset-password", h.ResetPassword)

	body := jsonBody(t, map[string]string{"identifier": "user123"}) // missing code + new_password
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("missing fields: status = %d, want 400", w.Code)
	}
}

// TestRegistrationHandler_NilService verifies NewRegistrationHandler returns nil for nil service.
func TestRegistrationHandler_NilService(t *testing.T) {
	h := NewRegistrationHandler(nil)
	if h != nil {
		t.Error("expected nil handler for nil service")
	}
}
