package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

const testWechatSessionSecret = "test-wechat-session-secret-32b!!"

// newWechatProxyServer starts an httptest server simulating a WeChat proxy.
// It responds to GET /api/wechat/user with the given openID and status code.
func newWechatProxyServer(t *testing.T, openID string, proxyStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/wechat/user") {
			if proxyStatus != http.StatusOK {
				w.WriteHeader(proxyStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"openid": openID})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/wechat/qrcode") {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWechatAuth_New_NilWhenAddrEmpty(t *testing.T) {
	h := NewWechatAuthHandler(makeAccountService(), "", "token", testWechatSessionSecret)
	if h != nil {
		t.Error("expected nil when wechatServerAddr is empty")
	}
}

func TestWechatAuth_New_NilWhenSecretEmpty(t *testing.T) {
	h := NewWechatAuthHandler(makeAccountService(), "http://proxy.svc:8080", "token", "")
	if h != nil {
		t.Error("expected nil when sessionSecret is empty")
	}
}

func TestWechatAuth_Initiate_SetsCookieAndRedirects(t *testing.T) {
	proxy := newWechatProxyServer(t, "wx-unused", http.StatusOK)

	h := NewWechatAuthHandler(makeAccountService(), proxy.URL, "proxy-token", testWechatSessionSecret)
	r := testRouter()
	r.GET("/api/v1/auth/wechat", h.Initiate)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/wechat", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}

	// Verify the wx_state cookie is set with a non-empty value
	var stateVal string
	for _, c := range w.Result().Cookies() {
		if c.Name == "wx_state" {
			stateVal = c.Value
			break
		}
	}
	if stateVal == "" {
		t.Error("wx_state cookie not set in Initiate response")
	}

	// Redirect location should point to the proxy server and contain the state
	location := w.Header().Get("Location")
	if !strings.Contains(location, proxy.URL) {
		t.Errorf("Location %q should contain proxy URL %q", location, proxy.URL)
	}
	if !strings.Contains(location, stateVal) {
		t.Errorf("Location %q should contain state %q", location, stateVal)
	}
}

func TestWechatAuth_Callback_HappyPath(t *testing.T) {
	proxy := newWechatProxyServer(t, "wx999", http.StatusOK)

	h := NewWechatAuthHandler(makeAccountService(), proxy.URL, "", testWechatSessionSecret)
	r := testRouter()
	r.GET("/api/v1/auth/wechat/callback", h.Callback)

	const testState = "abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/wechat/callback?code=testcode&state="+testState, nil)
	req.AddCookie(&http.Cookie{Name: "wx_state", Value: testState})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", w.Code, w.Body.String())
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "lurus_token=") {
		t.Fatalf("Location %q should contain lurus_token", location)
	}

	// Extract token and validate it
	idx := strings.Index(location, "lurus_token=")
	token := location[idx+len("lurus_token="):]
	id, err := auth.ValidateSessionToken(token, testWechatSessionSecret)
	if err != nil {
		t.Fatalf("ValidateSessionToken error: %v", err)
	}
	if id <= 0 {
		t.Errorf("account ID from token = %d, want > 0", id)
	}
}

func TestWechatAuth_Callback_MissingCookie(t *testing.T) {
	proxy := newWechatProxyServer(t, "wx-any", http.StatusOK)

	h := NewWechatAuthHandler(makeAccountService(), proxy.URL, "", testWechatSessionSecret)
	r := testRouter()
	r.GET("/api/v1/auth/wechat/callback", h.Callback)

	// No wx_state cookie → CSRF check fails
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/wechat/callback?code=abc&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestWechatAuth_Callback_StateMismatch(t *testing.T) {
	proxy := newWechatProxyServer(t, "wx-any", http.StatusOK)

	h := NewWechatAuthHandler(makeAccountService(), proxy.URL, "", testWechatSessionSecret)
	r := testRouter()
	r.GET("/api/v1/auth/wechat/callback", h.Callback)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/wechat/callback?code=abc&state=wrongstate", nil)
	req.AddCookie(&http.Cookie{Name: "wx_state", Value: "correctstate"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestWechatAuth_Callback_ProxyError(t *testing.T) {
	proxy := newWechatProxyServer(t, "", http.StatusInternalServerError)

	h := NewWechatAuthHandler(makeAccountService(), proxy.URL, "", testWechatSessionSecret)
	r := testRouter()
	r.GET("/api/v1/auth/wechat/callback", h.Callback)

	const testState = "abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/wechat/callback?code=abc&state="+testState, nil)
	req.AddCookie(&http.Cookie{Name: "wx_state", Value: testState})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
}

func TestWechatAuth_Callback_WechatIDField(t *testing.T) {
	// Proxy returns wechat_id field instead of openid
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"wechat_id": "wx000"})
	}))
	defer proxy.Close()

	h := NewWechatAuthHandler(makeAccountService(), proxy.URL, "", testWechatSessionSecret)
	r := testRouter()
	r.GET("/api/v1/auth/wechat/callback", h.Callback)

	const testState = "abcdef1234567890abcdef1234567890"
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/wechat/callback?code=abc&state="+testState, nil)
	req.AddCookie(&http.Cookie{Name: "wx_state", Value: testState})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 for wechat_id field; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Location"), "lurus_token=") {
		t.Error("expected lurus_token in redirect location")
	}
}

func TestFetchWechatUser_ParsesOpenID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openid":"wx123"}`))
	}))
	defer srv.Close()

	h := &WechatAuthHandler{wechatServerAddr: srv.URL}
	id, err := h.fetchWechatUser(context.Background(), "testcode")
	if err != nil {
		t.Fatalf("fetchWechatUser error: %v", err)
	}
	if id != "wx123" {
		t.Errorf("fetchWechatUser = %q, want %q", id, "wx123")
	}
}

func TestFetchWechatUser_ParsesWechatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"wechat_id":"wx456"}`))
	}))
	defer srv.Close()

	h := &WechatAuthHandler{wechatServerAddr: srv.URL}
	id, err := h.fetchWechatUser(context.Background(), "testcode")
	if err != nil {
		t.Fatalf("fetchWechatUser error: %v", err)
	}
	if id != "wx456" {
		t.Errorf("fetchWechatUser = %q, want %q", id, "wx456")
	}
}
