// Package tenant provides request-scoped tenant identity (account id, org id)
// stored in context.Context and the tooling to propagate it to PostgreSQL
// session variables so Row-Level Security policies can enforce isolation.
//
// See migrations/018_rls_org_foundation.sql for the SQL side.
package tenant

import "context"

type ctxKey struct{ name string }

var (
	accountIDKey = ctxKey{"account_id"}
	orgIDKey     = ctxKey{"org_id"}
)

// WithAccountID returns a copy of ctx carrying the given account id.
// Should be called by authentication middleware after validating credentials.
func WithAccountID(ctx context.Context, accountID int64) context.Context {
	if accountID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, accountIDKey, accountID)
}

// AccountIDFromContext returns the account id attached by WithAccountID.
// ok is false if no account id is present.
func AccountIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(accountIDKey).(int64)
	return v, ok
}

// WithOrgID returns a copy of ctx carrying the given org id. Used when a
// request is authenticated via an org API key rather than a personal account.
func WithOrgID(ctx context.Context, orgID int64) context.Context {
	if orgID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, orgIDKey, orgID)
}

// OrgIDFromContext returns the org id attached by WithOrgID.
func OrgIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(orgIDKey).(int64)
	return v, ok
}
