package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newOAuthTestRedis creates a miniredis instance and returns a redis.Client.
func newOAuthTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// makeWechatOAuthHandler creates a WechatOAuthHandler with miniredis and a mock WeChat server.
func makeWechatOAuthHandler(t *testing.T, wechatURL, clientSecret string) *WechatOAuthHandler {
	t.Helper()
	rdb := newOAuthTestRedis(t)
	h := NewWechatOAuthHandler(wechatURL, "test-server-token", clientSecret, rdb)
	if h == nil {
		t.Fatal("expected non-nil WechatOAuthHandler")
	}
	return h
}

// ---------- constructor ----------

// TestWechatOAuth_New_NilWhenAddrEmpty verifies nil return when wechatServerAddr is empty.
func TestWechatOAuth_New_NilWhenAddrEmpty(t *testing.T) {
	rdb := newOAuthTestRedis(t)
	h := NewWechatOAuthHandler("", "token", "client-secret", rdb)
	if h != nil {
		t.Error("expected nil when wechatServerAddr is empty")
	}
}

// TestWechatOAuth_New_NilWhenSecretEmpty verifies nil return when oauthClientSecret is empty.
func TestWechatOAuth_New_NilWhenSecretEmpty(t *testing.T) {
	rdb := newOAuthTestRedis(t)
	h := NewWechatOAuthHandler("http://wechat.example.com", "token", "", rdb)
	if h != nil {
		t.Error("expected nil when oauthClientSecret is empty")
	}
}

// TestWechatOAuth_New_Success verifies successful construction.
func TestWechatOAuth_New_Success(t *testing.T) {
	rdb := newOAuthTestRedis(t)
	h := NewWechatOAuthHandler("http://wechat.example.com", "token", "client-secret", rdb)
	if h == nil {
		t.Error("expected non-nil handler when both addr and secret are set")
	}
}

// ---------- Authorize ----------

// TestWechatOAuth_Authorize_MissingRedirectURI verifies 400 when redirect_uri is absent.
func TestWechatOAuth_Authorize_MissingRedirectURI(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/authorize", h.Authorize)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/authorize?state=abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing redirect_uri)", w.Code)
	}
}

// TestWechatOAuth_Authorize_MissingState verifies 400 when state is absent.
func TestWechatOAuth_Authorize_MissingState(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/authorize", h.Authorize)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/authorize?redirect_uri=https://auth.example.com/callback", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing state)", w.Code)
	}
}

// TestWechatOAuth_Authorize_Success verifies 302 redirect to WeChat QR page.
func TestWechatOAuth_Authorize_Success(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/authorize", h.Authorize)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/oauth/wechat/authorize?redirect_uri=https://auth.example.com/cb&state=test-state&client_id=lurus-wechat", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect to WeChat)", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "wechat.example.com") {
		t.Errorf("Location = %q, want redirect to wechat.example.com", location)
	}
}

// ---------- Callback ----------

// TestWechatOAuth_Callback_MissingParams verifies 400 when code or state is absent.
func TestWechatOAuth_Callback_MissingParams(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/callback", h.Callback)

	// Missing state.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/callback?code=wx-code", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing state)", w.Code)
	}
}

// TestWechatOAuth_Callback_UnknownState verifies 400 when state is not in Redis.
func TestWechatOAuth_Callback_UnknownState(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/callback", h.Callback)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/callback?code=wx-code&state=unknown-state", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unknown state)", w.Code)
	}
}

// ---------- Token ----------

// TestWechatOAuth_Token_WrongClientSecret verifies 401 when client_secret is incorrect.
func TestWechatOAuth_Token_WrongClientSecret(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "correct-secret")
	r := testRouter()
	r.POST("/oauth/wechat/token", h.Token)

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("code", "some-code")
	formData.Set("client_secret", "wrong-secret")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/wechat/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestWechatOAuth_Token_WrongGrantType verifies 400 for unsupported grant_type.
func TestWechatOAuth_Token_WrongGrantType(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "correct-secret")
	r := testRouter()
	r.POST("/oauth/wechat/token", h.Token)

	formData := url.Values{}
	formData.Set("grant_type", "client_credentials") // unsupported
	formData.Set("client_secret", "correct-secret")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/wechat/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unsupported grant_type)", w.Code)
	}
}

// TestWechatOAuth_Token_MissingCode verifies 400 when code is absent.
func TestWechatOAuth_Token_MissingCode(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "correct-secret")
	r := testRouter()
	r.POST("/oauth/wechat/token", h.Token)

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("client_secret", "correct-secret")
	// No code field.

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/wechat/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing code)", w.Code)
	}
}

// TestWechatOAuth_Token_UnknownCode verifies 400 when the auth code is not in Redis.
func TestWechatOAuth_Token_UnknownCode(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "correct-secret")
	r := testRouter()
	r.POST("/oauth/wechat/token", h.Token)

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("code", "nonexistent-code")
	formData.Set("client_secret", "correct-secret")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/wechat/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unknown code)", w.Code)
	}
}

// TestWechatOAuth_Token_BasicAuth verifies that client_secret from Basic Auth is accepted.
func TestWechatOAuth_Token_BasicAuth(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "correct-secret")
	r := testRouter()
	r.POST("/oauth/wechat/token", h.Token)

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("code", "some-code")
	// No client_secret in body — use Basic Auth.

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/wechat/token", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("lurus-wechat", "correct-secret")
	r.ServeHTTP(w, req)

	// Code is missing from Redis → 400 invalid_grant (not 401 — client auth passed).
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (code not in Redis, but basic auth accepted)", w.Code)
	}
}

// ---------- UserInfo ----------

// TestWechatOAuth_UserInfo_MissingBearer verifies 401 when Authorization header is absent.
func TestWechatOAuth_UserInfo_MissingBearer(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/userinfo", h.UserInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/userinfo", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (missing bearer)", w.Code)
	}
}

// TestWechatOAuth_UserInfo_EmptyBearer verifies 401 when the bearer token is empty.
func TestWechatOAuth_UserInfo_EmptyBearer(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/userinfo", h.UserInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/userinfo", nil)
	req.Header.Set("Authorization", "Bearer ")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (empty bearer token)", w.Code)
	}
}

// TestWechatOAuth_UserInfo_Success verifies 200 with sub/name when openid is valid.
func TestWechatOAuth_UserInfo_Success(t *testing.T) {
	h := makeWechatOAuthHandler(t, "http://wechat.example.com", "client-secret")
	r := testRouter()
	r.GET("/oauth/wechat/userinfo", h.UserInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/userinfo", nil)
	req.Header.Set("Authorization", "Bearer openid_abc123xyz")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestWechatOAuth_Callback_Success exercises fetchWechatOpenID and generateOAuthCode via Callback.
func TestWechatOAuth_Callback_Success(t *testing.T) {
	// Mock WeChat proxy server: exchanges code for openid.
	wechatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"openid":"test-openid-xyz"}`))
	}))
	t.Cleanup(wechatSrv.Close)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h := NewWechatOAuthHandler(wechatSrv.URL, "server-token", "client-secret", rdb)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}

	// Pre-seed state key in Redis to simulate a prior Authorize call.
	stateKey := "wechat_oauth_state:test-state-001"
	mr.Set(stateKey, "https://auth.example.com/callback")

	r := testRouter()
	r.GET("/oauth/wechat/callback", h.Callback)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/callback?code=wx-code&state=test-state-001", nil)
	r.ServeHTTP(w, req)

	// Should redirect back to Zitadel with an auth code.
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect to Zitadel); body: %s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if location == "" {
		t.Error("expected Location header in response")
	}
}

// TestWechatOAuth_Callback_FetchOpenIDFails verifies 502 when WeChat proxy returns an error.
func TestWechatOAuth_Callback_FetchOpenIDFails(t *testing.T) {
	// WeChat proxy returns error.
	wechatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"upstream failed"}`))
	}))
	t.Cleanup(wechatSrv.Close)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h := NewWechatOAuthHandler(wechatSrv.URL, "server-token", "client-secret", rdb)

	// Pre-seed state key.
	mr.Set("wechat_oauth_state:err-state", "https://auth.example.com/callback")

	r := testRouter()
	r.GET("/oauth/wechat/callback", h.Callback)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/wechat/callback?code=bad-code&state=err-state", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (WeChat proxy error)", w.Code)
	}
}
