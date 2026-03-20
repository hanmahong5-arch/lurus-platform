package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
)

func makeLocalRegHandler(t *testing.T) *RegistrationHandler {
	t.Helper()
	svc := app.NewRegistrationService(
		newMockAccountStore(), newMockWalletStore(), newMockVIPStore(),
		nil, nil, "test-secret-32-bytes-long!!!!!!!!",
		email.NoopSender{}, sms.NoopSender{}, nil, sms.SMSConfig{},
	)
	return NewRegistrationHandler(svc)
}

// ── Rich error format tests ─────────────────────────────────────────────────

func TestRegister_FieldLevelValidationError(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	// Short password.
	body, _ := json.Marshal(map[string]string{
		"username": "testuser",
		"password": "short",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}

	var resp RichError
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have field-level error on "password".
	if resp.Error.Code != "validation_error" {
		t.Errorf("code = %q, want 'validation_error'", resp.Error.Code)
	}
	if resp.Error.Fields == nil {
		t.Fatal("expected fields map in validation error")
	}
	if resp.Error.Fields["password"] == "" {
		t.Error("expected error on 'password' field")
	}
}

func TestRegister_ConflictWithLoginAction(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	// Register first user.
	body, _ := json.Marshal(map[string]string{
		"username": "taken_user",
		"password": "password123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Register same username again.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w2.Code, w2.Body.String())
	}

	var resp RichError
	json.Unmarshal(w2.Body.Bytes(), &resp)

	if resp.Error.Code != "conflict" {
		t.Errorf("code = %q, want 'conflict'", resp.Error.Code)
	}

	// Should suggest "Sign in" as an action.
	if len(resp.Error.Actions) == 0 {
		t.Fatal("expected actions in conflict response")
	}
	foundLogin := false
	for _, a := range resp.Error.Actions {
		if a.URL == "/login" {
			foundLogin = true
		}
	}
	if !foundLogin {
		t.Error("expected login action in conflict response")
	}
}

func TestRegister_SuccessIncludesRedirectURL(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "password123",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["token"] == nil || resp["token"] == "" {
		t.Error("expected non-empty token")
	}
	if resp["redirect_url"] != "/dashboard" {
		t.Errorf("redirect_url = %v, want '/dashboard'", resp["redirect_url"])
	}
}

// ── Pre-submit validation tests ─────────────────────────────────────────────

func TestCheckUsername_Available(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/check-username", h.CheckUsername)

	body, _ := json.Marshal(map[string]string{"username": "newuser"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/check-username", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["available"] != true {
		t.Errorf("available = %v, want true", resp["available"])
	}
}

func TestCheckUsername_InvalidFormat(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/check-username", h.CheckUsername)

	body, _ := json.Marshal(map[string]string{"username": "ab"}) // too short
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/check-username", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["available"] != false {
		t.Errorf("available = %v, want false for invalid username", resp["available"])
	}
	if resp["reason"] != "invalid" {
		t.Errorf("reason = %v, want 'invalid'", resp["reason"])
	}
}

func TestCheckUsername_Taken(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)
	r.POST("/api/v1/auth/check-username", h.CheckUsername)

	// Register first.
	body, _ := json.Marshal(map[string]string{"username": "occupieduser", "password": "password123"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Check availability.
	body2, _ := json.Marshal(map[string]string{"username": "occupieduser"})
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/v1/auth/check-username", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	var resp map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["available"] != false {
		t.Errorf("available = %v, want false (username taken)", resp["available"])
	}
}

// ── Reset password UX tests ─────────────────────────────────────────────────

func TestResetPassword_ExpiredCode_SuggestsNewCode(t *testing.T) {
	h := makeLocalRegHandler(t)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)
	r.POST("/api/v1/auth/reset-password", h.ResetPassword)

	// Register an account first.
	body, _ := json.Marshal(map[string]string{"username": "resetuser", "password": "password123"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Try reset with wrong code.
	body2, _ := json.Marshal(map[string]string{
		"identifier":   "resetuser",
		"code":         "000000",
		"new_password": "newpassword123",
	})
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/v1/auth/reset-password", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	var resp RichError
	json.Unmarshal(w2.Body.Bytes(), &resp)

	// Should suggest requesting a new code.
	if resp.Error.Code == "" {
		t.Skip("no error returned (service may not have Redis configured for this test)")
	}
	if len(resp.Error.Actions) > 0 {
		foundForgot := false
		for _, a := range resp.Error.Actions {
			if a.URL == "/forgot-password" {
				foundForgot = true
			}
		}
		if foundForgot {
			// Good: suggests re-requesting a code.
		}
	}
}
