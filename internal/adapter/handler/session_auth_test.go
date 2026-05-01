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

const testSessionSecret = "test-session-secret-32-bytes-long!"

func mintTestToken(t *testing.T, accountID int64, ttl time.Duration) string {
	t.Helper()
	tok, err := auth.IssueSessionToken(accountID, ttl, testSessionSecret)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	return tok
}

// runSessionAuth wraps the middleware in a single-shot router so each
// case exercises the full c.Abort/c.Next branch instead of a hand-built
// gin.Context (which can mask bugs in middleware sequencing).
func runSessionAuth(deps SessionAuthDeps, setupReq func(r *http.Request)) (status int, accountID int64, hit bool) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/probe", RequireSession(deps), func(c *gin.Context) {
		hit = true
		if v, ok := c.Get(auth.ContextKeyAccountID); ok {
			if id, ok2 := v.(int64); ok2 {
				accountID = id
			}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if setupReq != nil {
		setupReq(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, accountID, hit
}

func TestRequireSession_NoSecret_503(t *testing.T) {
	code, _, hit := runSessionAuth(SessionAuthDeps{Secret: ""}, nil)
	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", code)
	}
	if hit {
		t.Error("handler should not run when middleware 503s")
	}
}

func TestRequireSession_NoToken_401(t *testing.T) {
	code, _, hit := runSessionAuth(SessionAuthDeps{Secret: testSessionSecret}, nil)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", code)
	}
	if hit {
		t.Error("handler should not run on 401")
	}
}

func TestRequireSession_BadToken_401(t *testing.T) {
	code, _, hit := runSessionAuth(
		SessionAuthDeps{Secret: testSessionSecret},
		func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer not.a.real-token")
		},
	)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", code)
	}
	if hit {
		t.Error("handler should not run with bad token")
	}
}

func TestRequireSession_ValidBearer_SetsAccountID(t *testing.T) {
	tok := mintTestToken(t, 4242, time.Hour)
	code, accountID, hit := runSessionAuth(
		SessionAuthDeps{Secret: testSessionSecret},
		func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+tok)
		},
	)
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if !hit {
		t.Error("handler should have run")
	}
	if accountID != 4242 {
		t.Errorf("account_id seeded = %d, want 4242", accountID)
	}
}

func TestRequireSession_ValidCookie_SetsAccountID(t *testing.T) {
	tok := mintTestToken(t, 7, time.Hour)
	code, accountID, hit := runSessionAuth(
		SessionAuthDeps{Secret: testSessionSecret},
		func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tok})
		},
	)
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if !hit {
		t.Error("handler should have run")
	}
	if accountID != 7 {
		t.Errorf("account_id seeded = %d, want 7", accountID)
	}
}

func TestRequireSession_ExpiredToken_401(t *testing.T) {
	tok := mintTestToken(t, 1, -time.Second)
	code, _, hit := runSessionAuth(
		SessionAuthDeps{Secret: testSessionSecret},
		func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+tok)
		},
	)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", code)
	}
	if hit {
		t.Error("handler should not run on expired token")
	}
}

func TestRequireSession_RevokedToken_401(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	revoker := auth.NewSessionRevoker(rdb)

	tok := mintTestToken(t, 99, time.Hour)
	if err := revoker.Revoke(t.Context(), tok, time.Hour); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	code, _, hit := runSessionAuth(
		SessionAuthDeps{Secret: testSessionSecret, Revoker: revoker},
		func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+tok)
		},
	)
	if code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for revoked token", code)
	}
	if hit {
		t.Error("revoked token must not reach handler")
	}
}

func TestRequireSession_NilRevoker_AllowsValid(t *testing.T) {
	// nil revoker is a feature: deployments without Redis should still
	// authenticate. The token check stays strict; only the revoke layer
	// is skipped.
	tok := mintTestToken(t, 11, time.Hour)
	code, accountID, hit := runSessionAuth(
		SessionAuthDeps{Secret: testSessionSecret, Revoker: nil},
		func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+tok)
		},
	)
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if !hit || accountID != 11 {
		t.Errorf("hit=%v account_id=%d, want true/11", hit, accountID)
	}
}
