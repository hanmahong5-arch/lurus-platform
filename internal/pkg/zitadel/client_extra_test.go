package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- CreateHumanUserWithUsername ---

func TestZitadelClient_CreateHumanUserWithUsername_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v2/users/human") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"userId": "user-with-username-456"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	user, err := c.CreateHumanUserWithUsername(context.Background(), "alice", "pass!123", "alice@example.com")
	if err != nil {
		t.Fatalf("CreateHumanUserWithUsername: %v", err)
	}
	if user.UserID != "user-with-username-456" {
		t.Errorf("UserID = %q, want %q", user.UserID, "user-with-username-456")
	}
}

// TestZitadelClient_CreateHumanUserWithUsername_EmptyEmail verifies the placeholder
// email path when email is not supplied.
func TestZitadelClient_CreateHumanUserWithUsername_EmptyEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode body to verify placeholder email was used.
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		emailObj, _ := body["email"].(map[string]any)
		emailAddr, _ := emailObj["email"].(string)
		if !strings.HasSuffix(emailAddr, "@noreply.lurus.cn") {
			t.Errorf("expected noreply placeholder email, got %q", emailAddr)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"userId": "user-noreply-789"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	user, err := c.CreateHumanUserWithUsername(context.Background(), "bob", "pass!456", "")
	if err != nil {
		t.Fatalf("CreateHumanUserWithUsername with empty email: %v", err)
	}
	if user.UserID != "user-noreply-789" {
		t.Errorf("UserID = %q, want %q", user.UserID, "user-noreply-789")
	}
}

// TestZitadelClient_CreateHumanUserWithUsername_DuplicateUsername verifies 409 handling.
func TestZitadelClient_CreateHumanUserWithUsername_DuplicateUsername(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"message": "username already taken"})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUserWithUsername(context.Background(), "existinguser", "pass", "x@x.com")
	if err == nil {
		t.Fatal("expected error for duplicate username, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

// TestZitadelClient_CreateHumanUserWithUsername_ServerError verifies 5xx handling.
func TestZitadelClient_CreateHumanUserWithUsername_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUserWithUsername(context.Background(), "charlie", "pass", "charlie@x.com")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500, got: %v", err)
	}
}

// TestZitadelClient_CreateHumanUserWithUsername_EmptyUserID verifies empty userId rejection.
func TestZitadelClient_CreateHumanUserWithUsername_EmptyUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"userId": ""})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUserWithUsername(context.Background(), "dave", "pass", "dave@x.com")
	if err == nil {
		t.Fatal("expected error for empty userId, got nil")
	}
	if !strings.Contains(err.Error(), "empty userId") {
		t.Errorf("error should mention 'empty userId', got: %v", err)
	}
}

// TestZitadelClient_CreateHumanUserWithUsername_InvalidJSON verifies malformed response.
func TestZitadelClient_CreateHumanUserWithUsername_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateHumanUserWithUsername(context.Background(), "eve", "pass", "eve@x.com")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// --- RequestPasswordReset: additional branches ---

// TestZitadelClient_RequestPasswordReset_SendLink verifies the sendLink path
// (returnCode=false) where an empty verification code is returned.
func TestZitadelClient_RequestPasswordReset_SendLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the body contains sendLink, not returnCode.
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, hasSendLink := body["sendLink"]; !hasSendLink {
			t.Errorf("expected sendLink in body, got: %v", body)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	code, err := c.RequestPasswordReset(context.Background(), "user-send-link", false)
	if err != nil {
		t.Fatalf("RequestPasswordReset (sendLink): %v", err)
	}
	if code != "" {
		t.Errorf("expected empty code for sendLink path, got %q", code)
	}
}

// TestZitadelClient_RequestPasswordReset_ReturnCode_InvalidJSON verifies that a
// malformed response body on the returnCode path is handled as an error.
func TestZitadelClient_RequestPasswordReset_ReturnCode_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.RequestPasswordReset(context.Background(), "uid", true)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// --- SetNewPassword: additional branches ---

// TestZitadelClient_SetNewPassword_ServerError verifies 4xx/5xx handling.
func TestZitadelClient_SetNewPassword_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"invalid verification code"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.SetNewPassword(context.Background(), "uid", "bad-code", "newpass")
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention 400, got: %v", err)
	}
}

// TestZitadelClient_SetNewPassword_CorrectPath verifies the URL includes the user ID.
func TestZitadelClient_SetNewPassword_CorrectPath(t *testing.T) {
	const userID = "target-user-id"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, userID) {
			t.Errorf("expected path to contain user ID %q, got %q", userID, r.URL.Path)
		}
		if !strings.HasSuffix(r.URL.Path, "/password") {
			t.Errorf("expected path to end with /password, got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.SetNewPassword(context.Background(), userID, "valid-code", "newPass!789")
	if err != nil {
		t.Errorf("SetNewPassword: %v", err)
	}
}

// --- FindUserByEmail: additional branches ---

// TestZitadelClient_FindUserByEmail_ServerError verifies non-200 error handling.
func TestZitadelClient_FindUserByEmail_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FindUserByEmail(context.Background(), "someone@example.com")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

// TestZitadelClient_FindUserByEmail_InvalidJSON verifies malformed response handling.
func TestZitadelClient_FindUserByEmail_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{not-json"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FindUserByEmail(context.Background(), "bad@example.com")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestZitadelClient_FindUserByEmail_MultipleResults verifies that first result is returned
// when multiple users match (edge case).
func TestZitadelClient_FindUserByEmail_MultipleResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"userId": "first-user"},
				{"userId": "second-user"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	userID, err := c.FindUserByEmail(context.Background(), "shared@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if userID != "first-user" {
		t.Errorf("expected first-user, got %q", userID)
	}
}

// TestZitadelClient_FindUserByEmail_CorrectMethod verifies the request uses POST.
func TestZitadelClient_FindUserByEmail_CorrectMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{}})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.FindUserByEmail(context.Background(), "check@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
}

// TestZitadelClient_NewClient_IssuerStored verifies that the issuer is stored correctly.
func TestZitadelClient_NewClient_IssuerStored(t *testing.T) {
	const issuer = "https://auth.test.cn"
	c := NewClient(issuer, "my-pat")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.issuer != issuer {
		t.Errorf("issuer = %q, want %q", c.issuer, issuer)
	}
	if c.pat != "my-pat" {
		t.Errorf("pat = %q, want %q", c.pat, "my-pat")
	}
}
