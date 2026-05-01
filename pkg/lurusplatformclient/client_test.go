package lurusplatformclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stub serves a single canned response. Records the last incoming
// request for assertion. One per test for isolation.
type stub struct {
	t          *testing.T
	gotMethod  string
	gotPath    string
	gotQuery   string
	gotAuth    string
	gotCookie  string
	gotBody    string
	respStatus int
	respBody   string
}

func newStub(t *testing.T, status int, body string) (*stub, *httptest.Server) {
	t.Helper()
	s := &stub{t: t, respStatus: status, respBody: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.gotMethod = r.Method
		s.gotPath = r.URL.Path
		s.gotQuery = r.URL.RawQuery
		s.gotAuth = r.Header.Get("Authorization")
		if ck, err := r.Cookie(sessionCookieName); err == nil {
			s.gotCookie = ck.Value
		}
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			s.gotBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.respStatus)
		_, _ = io.WriteString(w, s.respBody)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

// ── New / WithXxx --------------------------------------------------------

func TestNew_TrimsTrailingSlash(t *testing.T) {
	c := New("https://identity.lurus.cn/")
	if c.baseURL != "https://identity.lurus.cn" {
		t.Fatalf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestNew_DefaultTimeout(t *testing.T) {
	c := New("http://x")
	if c.httpClient.Timeout != defaultHTTPTimeout {
		t.Fatalf("default timeout = %v, want %v", c.httpClient.Timeout, defaultHTTPTimeout)
	}
}

func TestWithHTTPClient_NilNoOp(t *testing.T) {
	c := New("http://x")
	orig := c.httpClient
	c.WithHTTPClient(nil)
	if c.httpClient != orig {
		t.Fatal("nil http.Client should not replace existing client")
	}
}

func TestWithHTTPClient_Replaces(t *testing.T) {
	c := New("http://x")
	custom := &http.Client{Timeout: 99 * time.Second}
	c.WithHTTPClient(custom)
	if c.httpClient != custom {
		t.Fatal("WithHTTPClient did not replace")
	}
}

// ── Auth header / cookie wiring ------------------------------------------

func TestDo_SendsInternalKeyAsBearer(t *testing.T) {
	s, srv := newStub(t, 200, `{"id":1,"lurus_id":"LU0000001","created_at":"2026-05-01T00:00:00Z"}`)
	c := New(srv.URL).WithInternalKey("ik-secret")
	if _, err := c.GetAccountByID(context.Background(), 1); err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotAuth != "Bearer ik-secret" {
		t.Fatalf("Authorization = %q, want %q", s.gotAuth, "Bearer ik-secret")
	}
	if s.gotCookie != "" {
		t.Fatalf("cookie should not be sent in InternalKey mode, got %q", s.gotCookie)
	}
}

func TestDo_SendsBearerToken(t *testing.T) {
	s, srv := newStub(t, 200, `{"account_id":42,"lurus_id":"LU0000042"}`)
	c := New(srv.URL).WithBearerToken("user-jwt")
	if _, err := c.Whoami(context.Background()); err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotAuth != "Bearer user-jwt" {
		t.Fatalf("Authorization = %q, want %q", s.gotAuth, "Bearer user-jwt")
	}
}

func TestDo_SendsCookie(t *testing.T) {
	s, srv := newStub(t, 200, `{"account_id":7,"lurus_id":"LU0000007"}`)
	c := New(srv.URL).WithCookieToken("cookie-val")
	if _, err := c.Whoami(context.Background()); err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotCookie != "cookie-val" {
		t.Fatalf("cookie value = %q, want %q", s.gotCookie, "cookie-val")
	}
	if s.gotAuth != "" {
		t.Fatalf("Authorization should be empty in cookie mode, got %q", s.gotAuth)
	}
}

func TestDo_LastWithXxxWins(t *testing.T) {
	s, srv := newStub(t, 200, `{"id":1,"lurus_id":"x","created_at":"2026-01-01T00:00:00Z"}`)
	c := New(srv.URL).
		WithBearerToken("first").
		WithInternalKey("second")
	if _, err := c.GetAccountByID(context.Background(), 1); err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotAuth != "Bearer second" {
		t.Fatalf("Authorization = %q, want last setter to win", s.gotAuth)
	}
}

func TestDo_EmptyKeyOmitsHeader(t *testing.T) {
	s, srv := newStub(t, 200, `{"account_id":1,"lurus_id":"x"}`)
	c := New(srv.URL).WithBearerToken("")
	if _, err := c.Whoami(context.Background()); err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotAuth != "" {
		t.Fatalf("Authorization should be empty when token is empty, got %q", s.gotAuth)
	}
}

// ── Whoami ---------------------------------------------------------------

func TestWhoami_Success(t *testing.T) {
	s, srv := newStub(t, 200, `{"account_id":42,"lurus_id":"LU0000042","display_name":"Alice","email":"a@b.com","phone":"+86138****2222"}`)
	c := New(srv.URL).WithBearerToken("tok")
	got, err := c.Whoami(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.AccountID != 42 || got.LurusID != "LU0000042" || got.DisplayName != "Alice" || got.Email != "a@b.com" || got.Phone != "+86138****2222" {
		t.Fatalf("unexpected response: %+v", got)
	}
	if s.gotMethod != http.MethodGet || s.gotPath != "/api/v1/whoami" {
		t.Fatalf("got %s %s, want GET /api/v1/whoami", s.gotMethod, s.gotPath)
	}
}

func TestWhoami_Unauthorized(t *testing.T) {
	_, srv := newStub(t, 401, `{"error":"unauthorized","message":"Authentication required"}`)
	c := New(srv.URL).WithBearerToken("expired")
	_, err := c.Whoami(context.Background())
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err = %v, want *PlatformError", err)
	}
	if !pe.IsUnauthorized() {
		t.Fatalf("IsUnauthorized = false; pe = %+v", pe)
	}
	if pe.Status != 401 || pe.Code != "unauthorized" || pe.Message != "Authentication required" {
		t.Fatalf("unexpected pe: %+v", pe)
	}
}

// ── GetLLMToken ----------------------------------------------------------

func TestGetLLMToken_Success(t *testing.T) {
	s, srv := newStub(t, 200, `{"key":"sk-abc","base_url":"https://newapi.lurus.cn/v1","name":"lurus-platform-default","unlimited_quota":true}`)
	c := New(srv.URL).WithBearerToken("u")
	got, err := c.GetLLMToken(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Key != "sk-abc" || got.BaseURL != "https://newapi.lurus.cn/v1" || got.Name != "lurus-platform-default" || !got.UnlimitedQuota {
		t.Fatalf("unexpected: %+v", got)
	}
	if s.gotPath != "/api/v1/account/me/llm-token" {
		t.Fatalf("path = %q", s.gotPath)
	}
}

func TestGetLLMToken_WithName(t *testing.T) {
	s, srv := newStub(t, 200, `{"key":"sk","base_url":"x","name":"lucrum","unlimited_quota":false}`)
	c := New(srv.URL).WithBearerToken("u")
	if _, err := c.GetLLMToken(context.Background(), &LLMTokenOptions{Name: "lucrum"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotQuery != "name=lucrum" {
		t.Fatalf("query = %q, want name=lucrum", s.gotQuery)
	}
}

func TestGetLLMToken_NotProvisioned(t *testing.T) {
	_, srv := newStub(t, 503, `{"error":"account_not_provisioned","message":"NewAPI mirror not yet created for this account; retry in a few seconds"}`)
	c := New(srv.URL).WithBearerToken("u")
	_, err := c.GetLLMToken(context.Background(), nil)
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err type: %v", err)
	}
	if pe.Code != "account_not_provisioned" || pe.Status != 503 {
		t.Fatalf("pe = %+v", pe)
	}
	if !pe.IsRetriable() {
		t.Fatalf("503 should be retriable")
	}
}

// ── Account lookups ------------------------------------------------------

func TestGetAccountByID_Success(t *testing.T) {
	s, srv := newStub(t, 200, `{"id":1,"lurus_id":"LU0000001","email":"a@b.com","created_at":"2026-05-01T00:00:00Z"}`)
	c := New(srv.URL).WithInternalKey("ik")
	a, err := c.GetAccountByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a.ID != 1 || a.LurusID != "LU0000001" || a.Email != "a@b.com" {
		t.Fatalf("unexpected: %+v", a)
	}
	if s.gotPath != "/internal/v1/accounts/1" {
		t.Fatalf("path = %q", s.gotPath)
	}
}

func TestGetAccountByID_NotFound(t *testing.T) {
	_, srv := newStub(t, 404, `{"error":"not_found","message":"Account not found"}`)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 999)
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err type: %v", err)
	}
	if !pe.IsNotFound() {
		t.Fatalf("IsNotFound false; pe = %+v", pe)
	}
}

func TestGetAccountByID_RejectsZeroID(t *testing.T) {
	c := New("http://x").WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "id must be > 0") {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestGetAccountByEmail_PathEscaped(t *testing.T) {
	s, srv := newStub(t, 200, `{"id":2,"lurus_id":"LU0000002","email":"foo+bar@x.com","created_at":"2026-05-01T00:00:00Z"}`)
	c := New(srv.URL).WithInternalKey("ik")
	if _, err := c.GetAccountByEmail(context.Background(), "foo+bar@x.com"); err != nil {
		t.Fatalf("err: %v", err)
	}
	want := "/internal/v1/accounts/by-email/foo+bar@x.com"
	if s.gotPath != want {
		t.Fatalf("path = %q, want %q", s.gotPath, want)
	}
}

func TestGetAccountByEmail_RejectsEmpty(t *testing.T) {
	c := New("http://x").WithInternalKey("ik")
	_, err := c.GetAccountByEmail(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "email must not be empty") {
		t.Fatalf("err = %v", err)
	}
}

// ── Wallet ---------------------------------------------------------------

func TestGetWalletBalance_Success(t *testing.T) {
	s, srv := newStub(t, 200, `{"balance":123.45,"frozen":10}`)
	c := New(srv.URL).WithInternalKey("ik")
	bal, err := c.GetWalletBalance(context.Background(), 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if bal.Balance != 123.45 || bal.Frozen != 10 {
		t.Fatalf("unexpected: %+v", bal)
	}
	if s.gotPath != "/internal/v1/accounts/5/wallet/balance" {
		t.Fatalf("path = %q", s.gotPath)
	}
}

func TestDebitWallet_Success(t *testing.T) {
	s, srv := newStub(t, 200, `{"success":true,"balance_after":89.5}`)
	c := New(srv.URL).WithInternalKey("ik")
	out, err := c.DebitWallet(context.Background(), 5, &DebitRequest{
		Amount: 10.5, Type: "usage", ProductID: "lucrum", Description: "monthly fee",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Success || out.BalanceAfter != 89.5 {
		t.Fatalf("unexpected: %+v", out)
	}
	if s.gotMethod != http.MethodPost {
		t.Fatalf("method = %s", s.gotMethod)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(s.gotBody), &sent); err != nil {
		t.Fatalf("body decode: %v; body=%s", err, s.gotBody)
	}
	if sent["amount"].(float64) != 10.5 || sent["type"].(string) != "usage" || sent["product_id"].(string) != "lucrum" {
		t.Fatalf("unexpected body: %+v", sent)
	}
}

func TestDebitWallet_InsufficientBalance(t *testing.T) {
	_, srv := newStub(t, 400, `{"error":"insufficient_balance","message":"Wallet balance is insufficient for this debit"}`)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.DebitWallet(context.Background(), 5, &DebitRequest{Amount: 999, Type: "usage"})
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err type: %v", err)
	}
	if !pe.IsInsufficient() {
		t.Fatalf("IsInsufficient = false; pe = %+v", pe)
	}
	if pe.IsRetriable() {
		t.Fatalf("insufficient_balance should NOT be retriable")
	}
}

func TestDebitWallet_RejectsBadInputs(t *testing.T) {
	c := New("http://x").WithInternalKey("ik")
	cases := []struct {
		name string
		acct int64
		req  *DebitRequest
		want string
	}{
		{"zero account", 0, &DebitRequest{Amount: 1, Type: "usage"}, "accountID must be > 0"},
		{"nil req", 1, nil, "req must not be nil"},
		{"zero amount", 1, &DebitRequest{Amount: 0, Type: "usage"}, "amount must be > 0"},
		{"empty type", 1, &DebitRequest{Amount: 1, Type: ""}, "type must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.DebitWallet(context.Background(), tc.acct, tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// ── Entitlements ---------------------------------------------------------

func TestGetEntitlements_Success(t *testing.T) {
	_, srv := newStub(t, 200, `{"plan_code":"pro","max_seats":"10"}`)
	c := New(srv.URL).WithInternalKey("ik")
	got, err := c.GetEntitlements(context.Background(), 1, "lucrum")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["plan_code"] != "pro" || got["max_seats"] != "10" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestGetEntitlements_FreeDefault(t *testing.T) {
	// Server returns free-plan default for unsubbed accounts; this is NOT
	// a 404, and the SDK does NOT synthesise one.
	_, srv := newStub(t, 200, `{"plan_code":"free"}`)
	c := New(srv.URL).WithInternalKey("ik")
	got, err := c.GetEntitlements(context.Background(), 1, "lucrum")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["plan_code"] != "free" {
		t.Fatalf("plan_code = %q", got["plan_code"])
	}
}

// ── Error paths ----------------------------------------------------------

func TestPlatformError_BadRequestEnvelope(t *testing.T) {
	_, srv := newStub(t, 400, `{"error":"invalid_parameter","message":"Account ID must be a positive integer"}`)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err: %v", err)
	}
	if pe.Status != 400 || pe.Code != "invalid_parameter" || !strings.Contains(pe.Message, "positive integer") {
		t.Fatalf("pe = %+v", pe)
	}
	if pe.IsRetriable() {
		t.Fatalf("400 with non-retriable code should NOT be retriable")
	}
}

func TestPlatformError_NonJSONBody(t *testing.T) {
	// e.g. a raw 502 from a reverse proxy with HTML body — must still
	// produce a useful PlatformError with a status-derived Code.
	_, srv := newStub(t, 502, `<html><body>Bad Gateway</body></html>`)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err: %v", err)
	}
	if pe.Status != 502 || pe.Code != "upstream_failed" {
		t.Fatalf("pe = %+v", pe)
	}
	if !strings.Contains(pe.RawBody, "Bad Gateway") {
		t.Fatalf("RawBody should retain HTML, got %q", pe.RawBody)
	}
	if !pe.IsRetriable() {
		t.Fatalf("502 should be retriable")
	}
}

func TestPlatformError_RawBodyTruncated(t *testing.T) {
	// 8 KiB body should be truncated to 4 KiB in RawBody.
	big := strings.Repeat("a", rawBodyTruncateBytes*2)
	_, srv := newStub(t, 500, big)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	pe, ok := AsPlatformError(err)
	if !ok {
		t.Fatalf("err: %v", err)
	}
	if len(pe.RawBody) != rawBodyTruncateBytes {
		t.Fatalf("RawBody len = %d, want %d", len(pe.RawBody), rawBodyTruncateBytes)
	}
}

func TestPlatformError_5xxRetriable(t *testing.T) {
	_, srv := newStub(t, 500, `{"error":"internal_error","message":"db hiccup"}`)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	pe, _ := AsPlatformError(err)
	if !pe.IsRetriable() {
		t.Fatalf("500 should be retriable")
	}
}

func TestPlatformError_429Retriable(t *testing.T) {
	_, srv := newStub(t, 429, `{"error":"rate_limited","message":"slow down"}`)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	pe, _ := AsPlatformError(err)
	if !pe.IsRateLimited() || !pe.IsRetriable() {
		t.Fatalf("429 should be rate-limited+retriable; pe=%+v", pe)
	}
}

func TestPlatformError_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1").WithInternalKey("ik") // refuses
	_, err := c.GetAccountByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected network error")
	}
	if _, ok := AsPlatformError(err); ok {
		t.Fatalf("network error should NOT be a *PlatformError, got %T", err)
	}
}

func TestPlatformError_ServerHangsUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Skip("hijack not supported")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}))
	defer srv.Close()
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error from mid-response disconnect")
	}
}

func TestPlatformError_ContextCancelled(t *testing.T) {
	_, srv := newStub(t, 200, `{}`)
	c := New(srv.URL).WithInternalKey("ik")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.GetAccountByID(ctx, 1)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled wrapped", err)
	}
}

// ── PlatformError methods ------------------------------------------------

func TestPlatformError_NilSafe(t *testing.T) {
	var pe *PlatformError
	if pe.IsNotFound() || pe.IsUnauthorized() || pe.IsForbidden() ||
		pe.IsInsufficient() || pe.IsRateLimited() || pe.IsUpstreamFailed() ||
		pe.IsRetriable() {
		t.Fatal("nil *PlatformError should report all sentinels false")
	}
	if pe.Error() == "" {
		t.Fatal("nil pe.Error() should still produce a string")
	}
}

func TestPlatformError_MessageFallback(t *testing.T) {
	// Empty body → Code from status, Message from http.StatusText.
	_, srv := newStub(t, 418, ``)
	c := New(srv.URL).WithInternalKey("ik")
	_, err := c.GetAccountByID(context.Background(), 1)
	pe, _ := AsPlatformError(err)
	if pe.Status != 418 || pe.Message == "" {
		t.Fatalf("pe = %+v; want non-empty fallback message", pe)
	}
}

func TestAsPlatformError_NonMatch(t *testing.T) {
	if pe, ok := AsPlatformError(errors.New("plain error")); ok || pe != nil {
		t.Fatalf("AsPlatformError on plain error: pe=%v ok=%v", pe, ok)
	}
}
