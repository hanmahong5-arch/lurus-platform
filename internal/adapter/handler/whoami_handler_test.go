package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
)

// maskPhone is the only piece of whoami logic that can be exercised
// without a full DB stack — the rest of the handler delegates to
// auth.ValidateSessionToken + AccountService.GetByID, which are
// covered by their own packages.
//
// Cookie/header path is exercised here too via a thin gin context.

func TestMaskPhone(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"+8613811112222", "+86138****2222"},
		{"13811112222", "138****2222"},
		{"+11234567890", "+1123***7890"}, // US/CA = 10-digit body → 3 stars in middle
		{"123456", "123456"}, // too short to mask
	}
	for _, tc := range cases {
		got := maskPhone(tc.in)
		if got != tc.want {
			t.Errorf("maskPhone(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mk := func(setup func(*http.Request)) *gin.Context {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if setup != nil {
			setup(req)
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		return c
	}

	t.Run("cookie wins", func(t *testing.T) {
		c := mk(func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "from-cookie"})
			r.Header.Set("Authorization", "Bearer from-bearer")
		})
		if got := ReadSessionToken(c); got != "from-cookie" {
			t.Errorf("expected cookie token, got %q", got)
		}
	})

	t.Run("bearer fallback", func(t *testing.T) {
		c := mk(func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer from-bearer")
		})
		if got := ReadSessionToken(c); got != "from-bearer" {
			t.Errorf("expected bearer token, got %q", got)
		}
	})

	t.Run("empty cookie falls through to bearer", func(t *testing.T) {
		c := mk(func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: ""})
			r.Header.Set("Authorization", "Bearer from-bearer")
		})
		if got := ReadSessionToken(c); got != "from-bearer" {
			t.Errorf("expected bearer fallback, got %q", got)
		}
	})

	t.Run("nothing", func(t *testing.T) {
		c := mk(nil)
		if got := ReadSessionToken(c); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("non-bearer header ignored", func(t *testing.T) {
		c := mk(func(r *http.Request) {
			r.Header.Set("Authorization", "Basic abc123")
		})
		if got := ReadSessionToken(c); got != "" {
			t.Errorf("expected empty (Basic ignored), got %q", got)
		}
	})
}

func TestWhoami_NoSessionSecret_503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWhoamiHandler(nil, "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	h.Whoami(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (no session secret), got %d", w.Code)
	}
}

func TestWhoami_NoToken_401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWhoamiHandler(nil, "secret")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	h.Whoami(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (no token), got %d", w.Code)
	}
}

func TestWhoami_BadToken_401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWhoamiHandler(nil, "secret")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	c.Request = req
	h.Whoami(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (bad token), got %d", w.Code)
	}
}

// P1-5: Logout must revoke a valid token server-side so a stolen Bearer
// can't be replayed for the rest of its TTL. The test wires a real
// miniredis-backed revoker so it exercises the SHA-256 hash path end-to-end.
func TestWhoami_Logout_RevokesToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	revoker := auth.NewSessionRevoker(rdb)

	const secret = "test-session-secret-32-bytes-long!"
	tok, err := auth.IssueSessionToken(123, time.Hour, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	h := NewWhoamiHandler(nil, secret).WithRevoker(revoker)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	c.Request = req

	h.Logout(c, "")

	if w.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", w.Code)
	}
	if !revoker.IsRevoked(c.Request.Context(), tok) {
		t.Fatal("token should be on the revoke list after logout")
	}
}

// Logout without a revoker must still 200 + clear the cookie. Documents
// that revocation is opt-in: pre-P1-5 deployments keep working unchanged.
func TestWhoami_Logout_NoRevoker_StillSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewWhoamiHandler(nil, "secret") // WithRevoker NOT called

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)

	h.Logout(c, "")

	if w.Code != http.StatusOK {
		t.Errorf("logout without revoker should be 200, got %d", w.Code)
	}
}
