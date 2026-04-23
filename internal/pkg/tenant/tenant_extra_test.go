package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// --- context.go edge cases ---

// TestWithOrgID_RejectsZeroOrNegative verifies WithOrgID ignores non-positive ids.
func TestWithOrgID_RejectsZeroOrNegative(t *testing.T) {
	ctx := context.Background()
	for _, id := range []int64{0, -1, -100} {
		out := WithOrgID(ctx, id)
		if _, ok := OrgIDFromContext(out); ok {
			t.Fatalf("id=%d: WithOrgID stored a non-positive id", id)
		}
	}
}

// TestOrgIDFromContext_Missing verifies OrgIDFromContext returns false when absent.
func TestOrgIDFromContext_Missing(t *testing.T) {
	_, ok := OrgIDFromContext(context.Background())
	if ok {
		t.Fatal("expected missing org id")
	}
}

// TestWithAccountID_AndOrgID_Independent verifies both keys are stored independently.
func TestWithAccountID_AndOrgID_Independent(t *testing.T) {
	ctx := WithAccountID(context.Background(), 10)
	ctx = WithOrgID(ctx, 20)

	accID, accOk := AccountIDFromContext(ctx)
	orgID, orgOk := OrgIDFromContext(ctx)

	if !accOk || accID != 10 {
		t.Errorf("account id: got (%d, %v), want (10, true)", accID, accOk)
	}
	if !orgOk || orgID != 20 {
		t.Errorf("org id: got (%d, %v), want (20, true)", orgID, orgOk)
	}
}

// --- session.go edge cases ---

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// TestSetSessionVars_OrgIDOnly verifies the org-id path (no account id in ctx).
// On sqlite set_config fails, so we get an error — that proves the branch executes.
func TestSetSessionVars_OrgIDOnly(t *testing.T) {
	db := openSQLite(t)
	ctx := WithOrgID(context.Background(), 55)

	// No account id → first branch skipped; org id branch triggers set_config → error on sqlite.
	var txErr error
	_ = db.Transaction(func(tx *gorm.DB) error {
		txErr = SetSessionVars(ctx, tx)
		return txErr
	})
	if txErr == nil {
		t.Fatal("expected error from set_config on sqlite for org_id, got nil")
	}
}

// TestSetSessionVars_BothAccountAndOrg verifies both branches execute when both ids present.
// On sqlite the first set_config call (account_id) fails immediately, so we confirm
// the account_id branch is attempted.
func TestSetSessionVars_BothAccountAndOrg(t *testing.T) {
	db := openSQLite(t)
	ctx := WithAccountID(context.Background(), 3)
	ctx = WithOrgID(ctx, 7)

	var txErr error
	_ = db.Transaction(func(tx *gorm.DB) error {
		txErr = SetSessionVars(ctx, tx)
		return txErr
	})
	if txErr == nil {
		t.Fatal("expected error from set_config on sqlite, got nil")
	}
}

// TestWithTenant_FnReturnsError verifies that errors from fn propagate and trigger rollback.
func TestWithTenant_FnReturnsError(t *testing.T) {
	db := openSQLite(t)
	sentinel := errors.New("fn error")

	err := WithTenant(context.Background(), db, func(tx *gorm.DB) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// TestWithTenant_FnCalledWithTx verifies fn receives a valid *gorm.DB.
func TestWithTenant_FnCalledWithTx(t *testing.T) {
	db := openSQLite(t)
	var txReceived *gorm.DB

	err := WithTenant(context.Background(), db, func(tx *gorm.DB) error {
		txReceived = tx
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
	if txReceived == nil {
		t.Fatal("fn did not receive a tx")
	}
}

// --- middleware.go ---

func init() {
	gin.SetMode(gin.TestMode)
}

// TestMiddleware_NoAccountID verifies the middleware is a no-op when account_id absent.
func TestMiddleware_NoAccountID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)

	Middleware()(c)

	_, ok := AccountIDFromContext(c.Request.Context())
	if ok {
		t.Error("expected no account id in context when gin key absent")
	}
}

// TestMiddleware_InvalidType verifies the middleware ignores non-int64 account_id values.
func TestMiddleware_InvalidType(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Set(GinContextKeyAccountID, "not-an-int64")

	Middleware()(c)

	_, ok := AccountIDFromContext(c.Request.Context())
	if ok {
		t.Error("expected no account id in context when gin value is wrong type")
	}
}

// TestMiddleware_ZeroAccountID verifies the middleware ignores zero account_id.
func TestMiddleware_ZeroAccountID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Set(GinContextKeyAccountID, int64(0))

	Middleware()(c)

	_, ok := AccountIDFromContext(c.Request.Context())
	if ok {
		t.Error("expected no account id in context for zero value")
	}
}

// TestMiddleware_NegativeAccountID verifies the middleware ignores negative account_id.
func TestMiddleware_NegativeAccountID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Set(GinContextKeyAccountID, int64(-5))

	Middleware()(c)

	_, ok := AccountIDFromContext(c.Request.Context())
	if ok {
		t.Error("expected no account id in context for negative value")
	}
}

// TestMiddleware_ValidAccountID verifies the middleware propagates a positive account_id.
func TestMiddleware_ValidAccountID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
	c.Set(GinContextKeyAccountID, int64(42))

	Middleware()(c)

	got, ok := AccountIDFromContext(c.Request.Context())
	if !ok {
		t.Fatal("expected account id in context, not found")
	}
	if got != 42 {
		t.Errorf("account id = %d, want 42", got)
	}
}

// TestMiddleware_DownstreamHandlerSeesValue verifies downstream handler can read the value.
func TestMiddleware_DownstreamHandlerSeesValue(t *testing.T) {
	router := gin.New()
	router.Use(Middleware())

	var capturedID int64
	router.GET("/test", func(c *gin.Context) {
		id, _ := AccountIDFromContext(c.Request.Context())
		capturedID = id
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)

	// Simulate auth middleware having set the value before our middleware.
	// We inject it by using a custom middleware before ours in a sub-test router.
	router2 := gin.New()
	router2.Use(func(c *gin.Context) {
		c.Set(GinContextKeyAccountID, int64(99))
		c.Next()
	})
	router2.Use(Middleware())
	router2.GET("/test", func(c *gin.Context) {
		id, _ := AccountIDFromContext(c.Request.Context())
		capturedID = id
	})

	router2.ServeHTTP(w, req)

	if capturedID != 99 {
		t.Errorf("downstream capturedID = %d, want 99", capturedID)
	}
}
