package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient creates a Client pointing to a test server.
func newTestClient(serverURL string) *Client {
	return &Client{
		issuer: serverURL,
		pat:    "test-pat",
		http:   &http.Client{Timeout: 5 * time.Second},
	}
}

// TestZitadelClient_CreateHumanUser_Success verifies a successful user creation.
func TestZitadelClient_CreateHumanUser_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v2/users/human") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Verify Authorization header.
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-pat" {
			t.Errorf("want 'Bearer test-pat', got %q", auth)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"userId": "zitadel-user-123"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	user, err := c.CreateHumanUser(context.Background(), "test@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateHumanUser: %v", err)
	}
	if user.UserID != "zitadel-user-123" {
		t.Errorf("UserID = %q, want %q", user.UserID, "zitadel-user-123")
	}
}

// TestZitadelClient_CreateHumanUser_DuplicateEmail verifies that 409 returns an error.
func TestZitadelClient_CreateHumanUser_DuplicateEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"message": "user already exists"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUser(context.Background(), "existing@example.com", "pass1234")
	if err == nil {
		t.Fatal("expected error for duplicate email, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

// TestZitadelClient_CreateHumanUser_ServerError verifies that 5xx returns an error.
func TestZitadelClient_CreateHumanUser_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUser(context.Background(), "user@example.com", "pass1234")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}

// TestZitadelClient_CreateHumanUser_Timeout verifies timeout handling.
func TestZitadelClient_CreateHumanUser_Timeout(t *testing.T) {
	// shutdownCtx is cancelled after the client call returns, so the server
	// handler can unblock and srv.Close() can proceed without hanging.
	shutdownCtx, shutdown := context.WithCancel(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until either the client disconnects or we're told to shut down.
		select {
		case <-r.Context().Done():
		case <-shutdownCtx.Done():
		}
	}))

	// Use a very short timeout.
	c := &Client{
		issuer: srv.URL,
		pat:    "test-pat",
		http:   &http.Client{Timeout: 10 * time.Millisecond},
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := c.CreateHumanUser(reqCtx, "user@example.com", "pass1234")

	// Unblock the handler before closing the server so srv.Close() returns promptly.
	shutdown()
	srv.Close()

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestZitadelClient_CreateHumanUser_EmptyUserID verifies that empty userId in response is rejected.
func TestZitadelClient_CreateHumanUser_EmptyUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"userId": ""}) // empty userId
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUser(context.Background(), "user@example.com", "pass1234")
	if err == nil {
		t.Fatal("expected error for empty userId, got nil")
	}
	if !strings.Contains(err.Error(), "empty userId") {
		t.Errorf("error should mention 'empty userId', got: %v", err)
	}
}

// TestZitadelClient_CreateHumanUser_InvalidJSON verifies malformed response handling.
func TestZitadelClient_CreateHumanUser_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json{{{"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUser(context.Background(), "user@example.com", "pass1234")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestZitadelClient_NewClient_NilForEmptyPAT verifies that empty PAT returns nil.
func TestZitadelClient_NewClient_NilForEmptyPAT(t *testing.T) {
	c := NewClient("https://auth.example.com", "")
	if c != nil {
		t.Error("NewClient with empty PAT should return nil")
	}
}

// TestZitadelClient_NewClient_ValidPAT verifies that non-empty PAT returns a client.
func TestZitadelClient_NewClient_ValidPAT(t *testing.T) {
	c := NewClient("https://auth.example.com", "some-pat")
	if c == nil {
		t.Error("NewClient with valid PAT should not return nil")
	}
}

// TestZitadelClient_RequestPasswordReset_Success verifies successful password reset initiation.
func TestZitadelClient_RequestPasswordReset_Success(t *testing.T) {
	const verificationCode = "ABC-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/password_reset") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"verificationCode": verificationCode})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	code, err := c.RequestPasswordReset(context.Background(), "user-id-abc", true)
	if err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if code != verificationCode {
		t.Errorf("code = %q, want %q", code, verificationCode)
	}
}

// TestZitadelClient_RequestPasswordReset_ServerError verifies 5xx handling.
func TestZitadelClient_RequestPasswordReset_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.RequestPasswordReset(context.Background(), "user-id", true)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

// TestZitadelClient_SetNewPassword_Success verifies successful password update.
func TestZitadelClient_SetNewPassword_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/password") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.SetNewPassword(context.Background(), "user-id", "code-123", "newPass!123")
	if err != nil {
		t.Errorf("SetNewPassword: %v", err)
	}
}

// TestZitadelClient_FindUserByEmail_Found verifies user lookup by email.
func TestZitadelClient_FindUserByEmail_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"userId": "found-user-id"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	userID, err := c.FindUserByEmail(context.Background(), "found@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if userID != "found-user-id" {
		t.Errorf("userID = %q, want %q", userID, "found-user-id")
	}
}

// TestZitadelClient_FindUserByEmail_NotFound verifies empty result returns empty string.
func TestZitadelClient_FindUserByEmail_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{}})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	userID, err := c.FindUserByEmail(context.Background(), "notfound@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if userID != "" {
		t.Errorf("userID should be empty for not found, got %q", userID)
	}
}
