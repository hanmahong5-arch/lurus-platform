package tenant

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// SetSessionVars applies the tenant context to the given PostgreSQL
// transaction via set_config(..., is_local=true), so that RLS policies
// referencing app.current_account_id() / app.current_org_id() enforce
// isolation for queries issued on tx.
//
// Must be invoked inside a transaction — set_config with is_local=true scopes
// the value to the current transaction and reverts on COMMIT/ROLLBACK, which
// makes it safe to use with a shared connection pool.
//
// No-op when the context carries no tenant identity; policies fall back to
// their NULL-bypass clause.
func SetSessionVars(ctx context.Context, tx *gorm.DB) error {
	if accountID, ok := AccountIDFromContext(ctx); ok {
		if err := tx.Exec(
			"SELECT set_config('app.current_account_id', ?, true)",
			fmt.Sprint(accountID),
		).Error; err != nil {
			return fmt.Errorf("tenant: set current_account_id: %w", err)
		}
	}
	if orgID, ok := OrgIDFromContext(ctx); ok {
		if err := tx.Exec(
			"SELECT set_config('app.current_org_id', ?, true)",
			fmt.Sprint(orgID),
		).Error; err != nil {
			return fmt.Errorf("tenant: set current_org_id: %w", err)
		}
	}
	return nil
}

// WithTenant runs fn inside a transaction that has tenant session vars set.
// Repositories that need RLS-enforced access should route their queries
// through this wrapper.
//
//	err := tenant.WithTenant(ctx, db, func(tx *gorm.DB) error {
//	    return tx.Where("org_id = ?", orgID).Find(&wallets).Error
//	})
func WithTenant(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := SetSessionVars(ctx, tx); err != nil {
			return err
		}
		return fn(tx)
	})
}
