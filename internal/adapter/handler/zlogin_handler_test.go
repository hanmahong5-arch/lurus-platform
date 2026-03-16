package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	lurusauth "github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// makeZLoginHandler creates a ZLoginHandler wired to the given Zitadel mock URL.
func makeZLoginHandler(t *testing.T, zitadelURL string) *ZLoginHandler {
	t.Helper()
	h := NewZLoginHandler(makeAccountService(), newMockAccountStore(), zitadelURL, "test-pat", testRegSessionSecret)
	if h == nil {
		t.Fatal("expected non-nil ZLoginHandler")
	}
	return h
}

// ---------- constructor ----------

// TestZLoginHandler_New_NilWhenNoPAT verifies nil return when serviceAccountPAT is empty.
func TestZLoginHandler_New_NilWhenNoPAT(t *testing.T) {
	h := NewZLoginHandler(makeAccountService(), newMockAccountStore(), "https://auth.example.com", "", "secret")
	if h != nil {
		t.Error("expected nil when serviceAccountPAT is empty")
	}
}

// TestZLoginHandler_New_Success verifies handler is created when PAT is provided.
func TestZLoginHandler_New_Success(t *testing.T) {
	h := NewZLoginHandler(makeAccountService(), newMockAccountStore(), "https://auth.example.com", "test-pat", "secret")
	if h == nil {
		t.Error("expected non-nil ZLoginHandler when PAT is set")
	}
}

// ---------- GetAuthInfo ----------

// TestZLoginHandler_GetAuthInfo_MissingParam verifies 400 when authRequestId is absent.
func TestZLoginHandler_GetAuthInfo_MissingParam(t *testing.T) {
	h := makeZLoginHandler(t, "http://localhost")
	r := testRouter()
	r.GET("/api/v1/auth/info", h.GetAuthInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/info", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestZLoginHandler_GetAuthInfo_UpstreamFailure verifies 200 fallback when upstream is unreachable.
func TestZLoginHandler_GetAuthInfo_UpstreamFailure(t *testing.T) {
	// Create server and immediately close it to cause connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.GET("/api/v1/auth/info", h.GetAuthInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/info?authRequestId=req-001", nil)
	r.ServeHTTP(w, req)

	// Fallback → 200 with {"app_name":"Lurus"}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fallback on upstream failure)", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["app_name"] != "Lurus" {
		t.Errorf("app_name = %v, want Lurus", resp["app_name"])
	}
}

// TestZLoginHandler_GetAuthInfo_InvalidJSON verifies 200 fallback when response is not valid JSON.
func TestZLoginHandler_GetAuthInfo_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-valid-json"))
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.GET("/api/v1/auth/info", h.GetAuthInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/info?authRequestId=req-002", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fallback on invalid JSON)", w.Code)
	}
}

// TestZLoginHandler_GetAuthInfo_Success verifies 200 and upstream response forwarding.
func TestZLoginHandler_GetAuthInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"app_name": "TestApp", "request_id": "req-003"})
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.GET("/api/v1/auth/info", h.GetAuthInfo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/info?authRequestId=req-003", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["app_name"] != "TestApp" {
		t.Errorf("app_name = %v, want TestApp", resp["app_name"])
	}
}

// ---------- SubmitPassword ----------

// TestZLoginHandler_SubmitPassword_MissingFields verifies 400 when required fields are absent.
func TestZLoginHandler_SubmitPassword_MissingFields(t *testing.T) {
	h := makeZLoginHandler(t, "http://localhost")
	r := testRouter()
	r.POST("/api/v1/auth/zlogin/password", h.SubmitPassword)

	// Missing password field.
	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-004",
		"username":        "user@example.com",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/zlogin/password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestZLoginHandler_SubmitPassword_InvalidCredentials verifies 401 when Zitadel rejects credentials.
func TestZLoginHandler_SubmitPassword_InvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid credentials"}`))
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/zlogin/password", h.SubmitPassword)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-005",
		"username":        "user@example.com",
		"password":        "wrongpassword",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/zlogin/password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestZLoginHandler_SubmitPassword_EmptySessionID verifies 401 when Zitadel returns empty sessionId.
func TestZLoginHandler_SubmitPassword_EmptySessionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		// No session ID in response.
		json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{}})
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/zlogin/password", h.SubmitPassword)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-006",
		"username":        "user@example.com",
		"password":        "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/zlogin/password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (empty sessionId)", w.Code)
	}
}

// ---------- LinkWechatAndComplete ----------

// TestZLoginHandler_LinkWechat_MissingFields verifies 400 when required fields are absent.
func TestZLoginHandler_LinkWechat_MissingFields(t *testing.T) {
	h := makeZLoginHandler(t, "http://localhost")
	r := testRouter()
	r.POST("/api/v1/auth/wechat/link-oidc", h.LinkWechatAndComplete)

	// Missing lurus_token field.
	body := jsonBody(t, map[string]string{"auth_request_id": "req-007"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/link-oidc", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestZLoginHandler_LinkWechat_NoSessionSecret verifies 503 when sessionSecret is not configured.
func TestZLoginHandler_LinkWechat_NoSessionSecret(t *testing.T) {
	// Build handler with empty session secret.
	h := NewZLoginHandler(makeAccountService(), newMockAccountStore(), "http://localhost", "test-pat", "")
	r := testRouter()
	r.POST("/api/v1/auth/wechat/link-oidc", h.LinkWechatAndComplete)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-008",
		"lurus_token":     "some-token",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/link-oidc", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// TestZLoginHandler_LinkWechat_InvalidToken verifies 401 for invalid lurus session token.
func TestZLoginHandler_LinkWechat_InvalidToken(t *testing.T) {
	h := makeZLoginHandler(t, "http://localhost")
	r := testRouter()
	r.POST("/api/v1/auth/wechat/link-oidc", h.LinkWechatAndComplete)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-009",
		"lurus_token":     "invalid-token-value",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/link-oidc", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ---------- DirectLogin ----------

// TestZLoginHandler_DirectLogin_MissingFields verifies 400 when required fields are absent.
func TestZLoginHandler_DirectLogin_MissingFields(t *testing.T) {
	h := makeZLoginHandler(t, "http://localhost")
	r := testRouter()
	r.POST("/api/v1/auth/login", h.DirectLogin)

	// Missing password field.
	body := jsonBody(t, map[string]string{"username": "user@example.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestZLoginHandler_DirectLogin_NoSessionSecret verifies 503 when sessionSecret is empty.
func TestZLoginHandler_DirectLogin_NoSessionSecret(t *testing.T) {
	h := NewZLoginHandler(makeAccountService(), newMockAccountStore(), "http://localhost", "test-pat", "")
	r := testRouter()
	r.POST("/api/v1/auth/login", h.DirectLogin)

	body := jsonBody(t, map[string]string{
		"username": "user@example.com",
		"password": "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// TestZLoginHandler_DirectLogin_InvalidCredentials verifies 401 when Zitadel rejects credentials.
func TestZLoginHandler_DirectLogin_InvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid credentials"}`))
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/login", h.DirectLogin)

	body := jsonBody(t, map[string]string{
		"username": "user@example.com",
		"password": "wrongpassword",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestZLoginHandler_DirectLogin_SessionFetchFails verifies 500 when session fetch fails after credential check.
func TestZLoginHandler_DirectLogin_SessionFetchFails(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodPost {
			// postSession succeeds and returns a valid sessionId.
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"sessionId": "sess-abc"})
		} else {
			// getSessionUser fails.
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/login", h.DirectLogin)

	body := jsonBody(t, map[string]string{
		"username": "user@example.com",
		"password": "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (session fetch failed)", w.Code)
	}
}

// TestZLoginHandler_SubmitPassword_Success verifies 200 and callback_url on full success path.
// This exercises createSessionByCredentials + createCallback together.
func TestZLoginHandler_SubmitPassword_Success(t *testing.T) {
	// Mock Zitadel server handles both POST /v2/sessions and POST /v2/oidc/auth_requests/.../create_callback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/sessions" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"sessionId":    "sess-success-001",
				"sessionToken": "tok-success-001",
			})
			return
		}
		// OIDC callback endpoint.
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"callbackUrl": "https://auth.example.com/callback?code=oidc-code",
		})
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/zlogin/password", h.SubmitPassword)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-success",
		"username":        "user@example.com",
		"password":        "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/zlogin/password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["callback_url"] == "" || resp["callback_url"] == nil {
		t.Error("response missing non-empty 'callback_url'")
	}
}

// TestZLoginHandler_SubmitPassword_CallbackFails verifies 500 when OIDC callback creation fails.
func TestZLoginHandler_SubmitPassword_CallbackFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/sessions" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"sessionId":    "sess-fail-cb",
				"sessionToken": "tok-fail-cb",
			})
			return
		}
		// Callback creation fails.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"callback error"}`))
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/zlogin/password", h.SubmitPassword)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-cb-fail",
		"username":        "user@example.com",
		"password":        "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/zlogin/password", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (callback creation failed)", w.Code)
	}
}

// TestZLoginHandler_LinkWechat_Success verifies full success path covering createSessionByUserID.
func TestZLoginHandler_LinkWechat_Success(t *testing.T) {
	// Build mock Zitadel server: POST /v2/sessions → 201, POST .../create_callback → 200.
	zitadelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/sessions" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"sessionId":    "sess-wechat-001",
				"sessionToken": "tok-wechat-001",
			})
			return
		}
		// OIDC callback.
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"callbackUrl": "https://auth.example.com/callback?code=wechat-oidc",
		})
	}))
	t.Cleanup(zitadelSrv.Close)

	// Seed an account with a Zitadel sub.
	accounts := newMockAccountStore()
	acc := accounts.seed(entity.Account{
		ZitadelSub:  "zid-wechat-sub-001",
		DisplayName: "WechatUser",
	})

	h := NewZLoginHandler(
		makeAccountServiceWith(accounts),
		accounts,
		zitadelSrv.URL,
		"test-pat",
		testRegSessionSecret,
	)

	// Issue a valid lurus session token for this account.
	token, err := lurusauth.IssueSessionToken(acc.ID, 24*time.Hour, testRegSessionSecret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	r := testRouter()
	r.POST("/api/v1/auth/wechat/link-oidc", h.LinkWechatAndComplete)

	body := jsonBody(t, map[string]string{
		"auth_request_id": "req-wechat-success",
		"lurus_token":     token,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/link-oidc", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["callback_url"] == "" || resp["callback_url"] == nil {
		t.Error("response missing non-empty 'callback_url'")
	}
}

// TestZLoginHandler_DirectLogin_Success verifies 200 with token on full success path.
// Covers getSessionUser, createSessionByCredentials, UpsertByZitadelSub, IssueSessionToken.
func TestZLoginHandler_DirectLogin_Success(t *testing.T) {
	// Mock Zitadel: POST /v2/sessions → 201, GET /v2/sessions/{id} → 200 with user data.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v2/sessions" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"sessionId":    "sess-direct-001",
				"sessionToken": "tok-direct-001",
			})
			return
		}
		// GET /v2/sessions/{sessionId}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"session": map[string]any{
				"factors": map[string]any{
					"user": map[string]any{
						"id":          "zid-user-001",
						"loginName":   "user@example.com",
						"displayName": "Test User",
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	h := makeZLoginHandler(t, srv.URL)
	r := testRouter()
	r.POST("/api/v1/auth/login", h.DirectLogin)

	body := jsonBody(t, map[string]string{
		"username": "user@example.com",
		"password": "Password123!",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] == "" || resp["token"] == nil {
		t.Error("response missing non-empty 'token'")
	}
	if resp["account_id"] == nil {
		t.Error("response missing 'account_id'")
	}
}
