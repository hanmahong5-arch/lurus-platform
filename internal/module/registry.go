// Package module provides the pluggable module integration layer.
// Core defines lifecycle hooks; modules register callbacks via the Registry.
// When a module is disabled (config toggle), its hooks are simply not registered —
// zero overhead, no conditional checks in business logic.
package module

import (
	"context"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// AccountHook is called when an account lifecycle event occurs.
type AccountHook func(ctx context.Context, account *entity.Account) error

// PlanChangeHook is called when a subscription plan changes.
type PlanChangeHook func(ctx context.Context, account *entity.Account, plan *entity.ProductPlan) error

// Registry holds module hooks registered at startup.
type Registry struct {
	onAccountCreated []AccountHook
	onAccountDeleted []AccountHook
	onPlanChanged    []PlanChangeHook
}

// NewRegistry creates an empty module registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// OnAccountCreated registers a hook for the account-created event.
func (r *Registry) OnAccountCreated(hook AccountHook) {
	r.onAccountCreated = append(r.onAccountCreated, hook)
}

// OnAccountDeleted registers a hook for the account-deleted event.
func (r *Registry) OnAccountDeleted(hook AccountHook) {
	r.onAccountDeleted = append(r.onAccountDeleted, hook)
}

// OnPlanChanged registers a hook for subscription plan changes.
func (r *Registry) OnPlanChanged(hook PlanChangeHook) {
	r.onPlanChanged = append(r.onPlanChanged, hook)
}

// FireAccountCreated invokes all registered account-created hooks.
// Hook failures are logged but do not block account creation (graceful degradation).
func (r *Registry) FireAccountCreated(ctx context.Context, account *entity.Account) {
	for _, hook := range r.onAccountCreated {
		if err := hook(ctx, account); err != nil {
			slog.Warn("module hook failed",
				"event", "account_created",
				"account_id", account.ID,
				"error", err,
			)
		}
	}
}

// FireAccountDeleted invokes all registered account-deleted hooks.
func (r *Registry) FireAccountDeleted(ctx context.Context, account *entity.Account) {
	for _, hook := range r.onAccountDeleted {
		if err := hook(ctx, account); err != nil {
			slog.Warn("module hook failed",
				"event", "account_deleted",
				"account_id", account.ID,
				"error", err,
			)
		}
	}
}

// FirePlanChanged invokes all registered plan-changed hooks.
func (r *Registry) FirePlanChanged(ctx context.Context, account *entity.Account, plan *entity.ProductPlan) {
	for _, hook := range r.onPlanChanged {
		if err := hook(ctx, account, plan); err != nil {
			slog.Warn("module hook failed",
				"event", "plan_changed",
				"account_id", account.ID,
				"plan_code", plan.Code,
				"error", err,
			)
		}
	}
}

// HookCount returns the total number of registered hooks (useful for startup logging).
func (r *Registry) HookCount() int {
	return len(r.onAccountCreated) + len(r.onAccountDeleted) + len(r.onPlanChanged)
}
