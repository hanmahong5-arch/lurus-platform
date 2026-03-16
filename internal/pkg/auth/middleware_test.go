package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAuth_LurusSessionToken_SetsAccountID(t *testing.T) {
	secret := "test-session-secret-32bytes--!!"
	token, err := IssueSessionToken(77, time.Hour, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	// nil validator is safe: lurus token path returns before reaching m.validator.Validate
	m := NewJWTMiddleware(nil, nil, secret)
	r := gin.New()
	var capturedID int64
	r.GET("/test", m.Auth(), func(c *gin.Context) {
		capturedID = GetAccountID(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if capturedID != 77 {
		t.Errorf("account_id = %d, want 77", capturedID)
	}
}

func TestAuth_ZitadelFallback_Calls_Lookup(t *testing.T) {
	key := generateTestRSAKey(t)
	v := newTestValidator(t, key)

	lookupCalled := false
	lookup := func(_ context.Context, _ *Claims) (int64, error) {
		lookupCalled = true
		return 55, nil
	}
	// Empty sessionSecret forces Zitadel path for every token
	m := NewJWTMiddleware(v, lookup, "")
	token := buildJWT(t, key, testKid, validClaims())

	r := gin.New()
	var capturedID int64
	r.GET("/test", m.Auth(), func(c *gin.Context) {
		capturedID = GetAccountID(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !lookupCalled {
		t.Error("AccountLookup was not called for Zitadel token")
	}
	if capturedID != 55 {
		t.Errorf("account_id = %d, want 55", capturedID)
	}
}

func TestAuth_MissingToken_401(t *testing.T) {
	// nil validator is safe: extractBearerToken aborts before validator is touched
	m := NewJWTMiddleware(nil, nil, "")
	r := gin.New()
	r.GET("/test", m.Auth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_InvalidToken_401(t *testing.T) {
	key := generateTestRSAKey(t)
	v := newTestValidator(t, key)
	m := NewJWTMiddleware(v, nil, "")

	r := gin.New()
	r.GET("/test", m.Auth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAdminAuth_ValidAdminToken_OK(t *testing.T) {
	key := generateTestRSAKey(t)
	v := newTestValidator(t, key)
	lookup := func(_ context.Context, _ *Claims) (int64, error) { return 10, nil }
	m := NewJWTMiddleware(v, lookup, "")

	// Build a Zitadel token with the admin role
	c := validClaims()
	c["urn:zitadel:iam:org:project:roles"] = map[string]interface{}{
		"admin": map[string]string{"org-1": "lurus"},
	}
	token := buildJWT(t, key, testKid, c)

	r := gin.New()
	r.GET("/admin", m.AdminAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for valid admin token", w.Code)
	}
}

func TestAdminAuth_ValidTokenNoAdminRole_403(t *testing.T) {
	key := generateTestRSAKey(t)
	v := newTestValidator(t, key)
	lookup := func(_ context.Context, _ *Claims) (int64, error) { return 20, nil }
	m := NewJWTMiddleware(v, lookup, "")

	// Valid Zitadel token but no admin role
	token := buildJWT(t, key, testKid, validClaims())

	r := gin.New()
	r.GET("/admin", m.AdminAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for token without admin role", w.Code)
	}
}

func TestAdminAuth_LurusToken_Rejected(t *testing.T) {
	secret := "test-session-secret-32bytes--!!"
	token, err := IssueSessionToken(42, time.Hour, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	// AdminAuth never checks session tokens; lurus iss="lurus-platform" mismatches testIssuer → 401
	key := generateTestRSAKey(t)
	v := newTestValidator(t, key)
	m := NewJWTMiddleware(v, nil, secret)

	r := gin.New()
	r.GET("/admin", m.AdminAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 401 or 403 (lurus token must not pass AdminAuth)", w.Code)
	}
}

func TestSafeTokenPrefix_ShortToken(t *testing.T) {
	// covers len(token) <= 16 branch
	got := safeTokenPrefix("abc")
	if got == "" {
		t.Error("expected non-empty result for short token")
	}
	// ensure the long-token path is also exercised
	long := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.long"
	got2 := safeTokenPrefix(long)
	if len(got2) == 0 {
		t.Error("expected non-empty result for long token")
	}
}
