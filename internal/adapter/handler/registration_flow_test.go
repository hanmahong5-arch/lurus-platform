package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── Register endpoint edge cases ──────────────────────────────────────────

func TestRegistration_Register_MissingUsername(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body, _ := json.Marshal(map[string]string{
		"password": "SecurePassword123!",
		"email":    "test@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing username)", w.Code)
	}
}

func TestRegistration_Register_MissingPassword(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body, _ := json.Marshal(map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing password)", w.Code)
	}
}

func TestRegistration_Register_EmptyBody(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (empty body)", w.Code)
	}
}

func TestRegistration_Register_InvalidJSON(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (invalid JSON)", w.Code)
	}
}

func TestRegistration_Register_ShortPassword(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body, _ := json.Marshal(map[string]string{
		"username": "testuser",
		"password": "short", // < 8 chars
		"email":    "test@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// RegistrationService validates password length and returns a specific error.
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (short password)", w.Code)
	}
}

func TestRegistration_Register_InvalidUsernameChars(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body, _ := json.Marshal(map[string]string{
		"username": "a b", // space is invalid
		"password": "SecurePassword123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (invalid username)", w.Code)
	}
}

func TestRegistration_Register_UsernameTooShort(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/register", h.Register)

	body, _ := json.Marshal(map[string]string{
		"username": "ab", // < 3 chars
		"password": "SecurePassword123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (username too short)", w.Code)
	}
}

// ── ForgotPassword endpoint ───────────────────────────────────────────────

func TestRegistration_ForgotPassword_Success(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/forgot-password", h.ForgotPassword)

	body, _ := json.Marshal(map[string]string{"identifier": "test@example.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Always returns 200 to prevent account enumeration.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRegistration_ForgotPassword_MissingIdentifier(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/forgot-password", h.ForgotPassword)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegistration_ForgotPassword_NonexistentAccount(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/forgot-password", h.ForgotPassword)

	body, _ := json.Marshal(map[string]string{"identifier": "nobody@nowhere.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Should still return 200 to prevent enumeration.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (anti-enumeration)", w.Code)
	}
}

// ── ResetPassword endpoint ────────────────────────────────────────────────

func TestRegistration_ResetPassword_MissingFields(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/reset-password", h.ResetPassword)

	body, _ := json.Marshal(map[string]string{"identifier": "test@example.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing code and password)", w.Code)
	}
}

func TestRegistration_ResetPassword_EmptyBody(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/reset-password", h.ResetPassword)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/reset-password", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── CheckUsername endpoint ────────────────────────────────────────────────

func TestRegistration_CheckUsername_InvalidFormat(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/check-username", h.CheckUsername)

	body, _ := json.Marshal(map[string]string{"username": "ab"}) // too short
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/check-username", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["available"] != false {
		t.Error("expected available=false for invalid username")
	}
	if resp["reason"] != "invalid" {
		t.Errorf("reason = %v, want invalid", resp["reason"])
	}
}

func TestRegistration_CheckUsername_Available(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/check-username", h.CheckUsername)

	body, _ := json.Marshal(map[string]string{"username": "available_user"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/check-username", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["available"] != true {
		t.Errorf("available = %v, want true", resp["available"])
	}
}

func TestRegistration_CheckUsername_MissingField(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/check-username", h.CheckUsername)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/check-username", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── CheckEmail endpoint ───────────────────────────────────────────────────

func TestRegistration_CheckEmail_Available(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/check-email", h.CheckEmail)

	body, _ := json.Marshal(map[string]string{"email": "new@example.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/check-email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["available"] != true {
		t.Errorf("available = %v, want true", resp["available"])
	}
}

func TestRegistration_CheckEmail_MissingField(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/check-email", h.CheckEmail)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/check-email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegistration_CheckEmail_InvalidFormat(t *testing.T) {
	srv := newZitadelTestServer(t, http.StatusOK, "user-1")
	h := makeRegistrationHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/check-email", h.CheckEmail)

	body, _ := json.Marshal(map[string]string{"email": "not-an-email"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/check-email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["available"] != false {
		t.Error("expected available=false for invalid email format")
	}
}
