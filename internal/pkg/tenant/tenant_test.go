package tenant

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAccountIDFromContext_Missing(t *testing.T) {
	_, ok := AccountIDFromContext(context.Background())
	if ok {
		t.Fatal("expected missing account id")
	}
}

func TestAccountIDFromContext_RoundTrip(t *testing.T) {
	ctx := WithAccountID(context.Background(), 42)
	got, ok := AccountIDFromContext(ctx)
	if !ok || got != 42 {
		t.Fatalf("got (%d, %v), want (42, true)", got, ok)
	}
}

func TestWithAccountID_RejectsZeroOrNegative(t *testing.T) {
	ctx := context.Background()
	for _, id := range []int64{0, -1, -999} {
		out := WithAccountID(ctx, id)
		if _, ok := AccountIDFromContext(out); ok {
			t.Fatalf("id=%d: WithAccountID stored a non-positive id", id)
		}
	}
}

func TestOrgIDFromContext_RoundTrip(t *testing.T) {
	ctx := WithOrgID(context.Background(), 77)
	got, ok := OrgIDFromContext(ctx)
	if !ok || got != 77 {
		t.Fatalf("got (%d, %v), want (77, true)", got, ok)
	}
}

// TestWithTenant_RunsFn exercises the tx wrapper against sqlite.
// set_config does not exist in sqlite so SetSessionVars fails — we verify
// the error path. Real RLS enforcement requires PostgreSQL (see
// migrations/018_rls_org_foundation.sql and its functional test).
func TestWithTenant_RunsFn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// No tenant in context → SetSessionVars is a no-op → fn runs cleanly.
	var called bool
	err = WithTenant(context.Background(), db, func(tx *gorm.DB) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenant (no tenant): %v", err)
	}
	if !called {
		t.Fatal("fn not invoked")
	}

	// With tenant in context on sqlite, SetSessionVars fails because
	// set_config is PostgreSQL-specific — verifies the error is surfaced.
	ctx := WithAccountID(context.Background(), 123)
	err = WithTenant(ctx, db, func(tx *gorm.DB) error {
		t.Fatal("fn should not run when SetSessionVars errors")
		return nil
	})
	if err == nil {
		t.Fatal("expected error from set_config on sqlite, got nil")
	}
}
