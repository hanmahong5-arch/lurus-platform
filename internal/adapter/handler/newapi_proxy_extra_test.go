package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── POST method forwarding ────────────────────────────────────────────────

func TestNewAPIProxy_Handle_PostMethod(t *testing.T) {
	var capturedMethod string
	var capturedBody string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	h, _ := NewNewAPIProxyHandler(backend.URL, "token", "admin")
	r := testRouter()
	r.POST("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Post(srv.URL+"/proxy/newapi/api/tokens", "application/json", strings.NewReader(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedMethod != "POST" {
		t.Errorf("method = %s, want POST", capturedMethod)
	}
	if capturedBody != `{"name":"test"}` {
		t.Errorf("body = %q, want {\"name\":\"test\"}", capturedBody)
	}
}

// ── DELETE method forwarding ──────────────────────────────────────────────

func TestNewAPIProxy_Handle_DeleteMethod(t *testing.T) {
	var capturedMethod string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(backend.Close)

	h, _ := NewNewAPIProxyHandler(backend.URL, "token", "admin")
	r := testRouter()
	r.DELETE("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	req, _ := http.NewRequest("DELETE", srv.URL+"/proxy/newapi/api/tokens/123", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedMethod != "DELETE" {
		t.Errorf("method = %s, want DELETE", capturedMethod)
	}
}

// ── Query string preservation ─────────────────────────────────────────────

func TestNewAPIProxy_Handle_QueryStringPreserved(t *testing.T) {
	var capturedQuery string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	h, _ := NewNewAPIProxyHandler(backend.URL, "token", "admin")
	r := testRouter()
	r.GET("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/api/channels?page=2&limit=10")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedQuery != "page=2&limit=10" {
		t.Errorf("query = %q, want page=2&limit=10", capturedQuery)
	}
}

// ── Empty userID skips New-Api-User header ─────────────────────────────────

func TestNewAPIProxy_Handle_EmptyUserIDNoHeader(t *testing.T) {
	var hasUserHeader bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasUserHeader = r.Header.Get("New-Api-User") != ""
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	h, _ := NewNewAPIProxyHandler(backend.URL, "token", "") // empty userID
	r := testRouter()
	r.GET("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/api/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if hasUserHeader {
		t.Error("New-Api-User should NOT be set when userID is empty")
	}
}

// ── Backend returns non-200 status ────────────────────────────────────────

func TestNewAPIProxy_Handle_BackendNon200(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	t.Cleanup(backend.Close)

	h, _ := NewNewAPIProxyHandler(backend.URL, "token", "admin")
	r := testRouter()
	r.GET("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/api/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Backend 403 should be forwarded as-is.
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (forwarded from backend)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "forbidden") {
		t.Errorf("body should contain 'forbidden', got: %s", body)
	}
}

// ── Deep nested path ──────────────────────────────────────────────────────

func TestNewAPIProxy_Handle_DeepNestedPath(t *testing.T) {
	var capturedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	h, _ := NewNewAPIProxyHandler(backend.URL, "token", "admin")
	r := testRouter()
	r.GET("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/api/v1/users/123/tokens")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedPath != "/api/v1/users/123/tokens" {
		t.Errorf("path = %q, want /api/v1/users/123/tokens", capturedPath)
	}
}
