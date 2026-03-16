package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveViaRealServer spins up a real HTTP server for the Gin engine and returns the server URL.
// This is required for handlers that use httputil.ReverseProxy, which relies on http.CloseNotifier
// which httptest.ResponseRecorder does not implement.
func serveViaRealServer(t *testing.T, r http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// ---------- constructor ----------

// TestNewAPIProxy_New_InvalidURL verifies error when the URL cannot be parsed.
func TestNewAPIProxy_New_InvalidURL(t *testing.T) {
	_, err := NewNewAPIProxyHandler("://invalid-url", "token", "user")
	if err == nil {
		t.Error("expected error for unparseable URL")
	}
}

// TestNewAPIProxy_New_Success verifies successful construction with a valid URL.
func TestNewAPIProxy_New_Success(t *testing.T) {
	h, err := NewNewAPIProxyHandler("http://internal.example.com:8080", "access-token", "admin-user")
	if err != nil {
		t.Fatalf("NewNewAPIProxyHandler: %v", err)
	}
	if h == nil {
		t.Error("expected non-nil handler")
	}
}

// ---------- Handle ----------

// TestNewAPIProxy_Handle_ProxiesRequest verifies the request is proxied with correct Authorization.
func TestNewAPIProxy_Handle_ProxiesRequest(t *testing.T) {
	var capturedAuth string
	var capturedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(backend.Close)

	h, err := NewNewAPIProxyHandler(backend.URL, "my-access-token", "admin-user")
	if err != nil {
		t.Fatalf("NewNewAPIProxyHandler: %v", err)
	}

	r := testRouter()
	r.GET("/proxy/newapi/api/channel", h.Handle)
	srv := serveViaRealServer(t, r)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/proxy/newapi/api/channel", nil)
	req.Header.Set("Authorization", "Bearer original-upstream-jwt")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if capturedAuth != "Bearer my-access-token" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer my-access-token")
	}
	// Verify the /proxy/newapi prefix was stripped from the path.
	if capturedPath != "/api/channel" {
		t.Errorf("backend path = %q, want /api/channel", capturedPath)
	}
}

// TestNewAPIProxy_Handle_InjectsNewApiUser verifies the New-Api-User header is forwarded.
func TestNewAPIProxy_Handle_InjectsNewApiUser(t *testing.T) {
	var capturedUser string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = r.Header.Get("New-Api-User")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	h, err := NewNewAPIProxyHandler(backend.URL, "token", "admin-123")
	if err != nil {
		t.Fatalf("NewNewAPIProxyHandler: %v", err)
	}

	r := testRouter()
	r.GET("/proxy/newapi/test", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedUser != "admin-123" {
		t.Errorf("New-Api-User = %q, want admin-123", capturedUser)
	}
}

// TestNewAPIProxy_Handle_RemovesSetCookie verifies Set-Cookie is stripped from the proxied response.
func TestNewAPIProxy_Handle_RemovesSetCookie(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=leaked; Path=/; HttpOnly")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	t.Cleanup(backend.Close)

	h, err := NewNewAPIProxyHandler(backend.URL, "token", "")
	if err != nil {
		t.Fatalf("NewNewAPIProxyHandler: %v", err)
	}

	r := testRouter()
	r.GET("/proxy/newapi/test", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if cookie := resp.Header.Get("Set-Cookie"); cookie != "" {
		t.Errorf("Set-Cookie should be stripped, got: %q", cookie)
	}
}

// TestNewAPIProxy_Handle_BackendDown verifies 502 when the backend is unreachable.
func TestNewAPIProxy_Handle_BackendDown(t *testing.T) {
	// Use a port that is not listening.
	h, err := NewNewAPIProxyHandler("http://127.0.0.1:19999", "token", "")
	if err != nil {
		t.Fatalf("NewNewAPIProxyHandler: %v", err)
	}

	r := testRouter()
	r.GET("/proxy/newapi/test", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (backend unreachable)", resp.StatusCode)
	}
}

// TestNewAPIProxy_Handle_EmptyPathBecomesRoot verifies /proxy/newapi is mapped to / on backend.
func TestNewAPIProxy_Handle_EmptyPathBecomesRoot(t *testing.T) {
	var capturedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	h, err := NewNewAPIProxyHandler(backend.URL, "token", "")
	if err != nil {
		t.Fatalf("NewNewAPIProxyHandler: %v", err)
	}

	// The proxy strips /proxy/newapi — an empty suffix becomes "/".
	r := testRouter()
	r.Any("/proxy/newapi/*path", h.Handle)
	srv := serveViaRealServer(t, r)

	resp, err := http.Get(srv.URL + "/proxy/newapi/")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedPath != "/" {
		t.Errorf("backend path = %q, want /", capturedPath)
	}
}
