package platformclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Each test here targets a function or branch that client_test.go doesn't
// cover, intentionally avoiding any duplication with the existing suite.

// TestValidateSession_Success covers the ValidateSession method — 0% before.
func TestValidateSession_Success(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Account{ID: 42, LurusID: "LU0000042"})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	acct, err := c.ValidateSession(context.Background(), "some-jwt")
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if acct == nil || acct.ID != 42 {
		t.Fatalf("unexpected account: %+v", acct)
	}
	if gotPath != "/internal/v1/accounts/validate-session" {
		t.Errorf("path = %s", gotPath)
	}
	if !strings.Contains(gotBody, "some-jwt") {
		t.Errorf("body didn't include token: %s", gotBody)
	}
}

// TestValidateSession_InvalidToken — surfaces server 401 as an error.
func TestValidateSession_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	_, err := c.ValidateSession(context.Background(), "bad-jwt")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

// TestGetCheckoutStatus_Success covers GetCheckoutStatus — 0% before.
func TestGetCheckoutStatus_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CheckoutResult{OrderNo: "LO20260423", PayURL: "https://pay.example.com"})
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	got, err := c.GetCheckoutStatus(context.Background(), "LO20260423")
	if err != nil {
		t.Fatalf("GetCheckoutStatus: %v", err)
	}
	if got.OrderNo != "LO20260423" {
		t.Errorf("OrderNo = %q", got.OrderNo)
	}
	if gotPath != "/internal/v1/checkout/LO20260423/status" {
		t.Errorf("path = %s", gotPath)
	}
}

// TestGetCheckoutStatus_NotFound — maps backend 404 to error.
func TestGetCheckoutStatus_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	_, err := c.GetCheckoutStatus(context.Background(), "LO-MISSING")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

// TestGetEntitlements_EmptyResult — the empty JSON {} path (no entitlements).
// Existing test covers a populated response.
func TestGetEntitlements_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	got, err := c.GetEntitlements(context.Background(), 1, "platform")
	if err != nil {
		t.Fatalf("GetEntitlements: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty entitlements, got %v", got)
	}
}

// TestDo_MalformedJSON — a successful HTTP response with a garbage body
// should produce a decode error, not a panic or silent success.
func TestDo_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{ not valid json`))
	}))
	defer srv.Close()

	c := New(srv.URL, "key")
	_, err := c.GetAccountByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected decode error for malformed JSON, got nil")
	}
}

// (Removed TestNew_TrimsTrailingSlash — New currently does NOT trim the
// trailing slash; asserting it would demand a production change outside
// the scope of pure coverage work.)
