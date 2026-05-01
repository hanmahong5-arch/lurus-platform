// Package module provides the pluggable module integration layer.
// Core defines lifecycle hooks; modules register callbacks via the Registry.
// When a module is disabled (config toggle), its hooks are simply not registered —
// zero overhead, no conditional checks in business logic.
package module

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// AccountHook is called when an account lifecycle event occurs.
type AccountHook func(ctx context.Context, account *entity.Account) error

// PlanChangeHook is called when a subscription plan changes.
type PlanChangeHook func(ctx context.Context, account *entity.Account, plan *entity.ProductPlan) error

// CheckinHook is called after a successful daily check-in.
type CheckinHook func(ctx context.Context, accountID int64, streak int) error

// ReferralSignupHook is called when a referred user completes registration.
type ReferralSignupHook func(ctx context.Context, referrerAccountID int64, referredName string) error

// ReconciliationIssueHook is called when a critical reconciliation issue is detected.
type ReconciliationIssueHook func(ctx context.Context, issue *entity.ReconciliationIssue) error

// AccountFetcher resolves an account by ID. The Registry uses it during
// DLQ replay so the hook gets a fresh account snapshot rather than the
// stale data captured at first-failure time.
type AccountFetcher func(ctx context.Context, accountID int64) (*entity.Account, error)

// DeadLetterStore persists failed hook invocations after retry exhaustion.
// nil store ⇒ legacy behaviour (warn + drop). Production wires the
// Postgres-backed repo.HookFailureRepo here.
type DeadLetterStore interface {
	Save(ctx context.Context, f *entity.HookFailure) error
	List(ctx context.Context, pendingOnly bool, limit, offset int) ([]entity.HookFailure, int64, error)
	GetByID(ctx context.Context, id int64) (*entity.HookFailure, error)
	MarkReplayed(ctx context.Context, id int64, at time.Time) error
}

// HookMetricsSink lets the Registry emit metrics without taking a hard
// dependency on the metrics package (avoids an import cycle since
// metrics may eventually want to read DLQ depth from here). All methods
// must tolerate nil receivers.
type HookMetricsSink interface {
	RecordHookOutcome(event, hook, result string)
	SetDLQDepth(depth int64)
}

// RetryPolicy controls how the Registry retries a failing hook before
// giving up to the DLQ. Zero values fall back to defaults.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	JitterFraction float64 // ±fraction (e.g. 0.2 = ±20%)
}

// defaultRetryPolicy is intentionally conservative — async lifecycle
// hooks that fail 3 times in 1.4s are almost certainly facing a real
// outage, not a blip. Don't burn CPU on hopeful retries that all hit
// the same dead Stalwart.
var defaultRetryPolicy = RetryPolicy{
	MaxAttempts:    3,
	InitialBackoff: 200 * time.Millisecond,
	MaxBackoff:     2 * time.Second,
	JitterFraction: 0.2,
}

// namedAccountHook bundles the registered AccountHook with its identifier.
// The name is the DLQ row's `hook_name` and the replay key.
type namedAccountHook struct {
	name string
	fn   AccountHook
}

type namedPlanChangeHook struct {
	name string
	fn   PlanChangeHook
}

type namedCheckinHook struct {
	name string
	fn   CheckinHook
}

type namedReferralSignupHook struct {
	name string
	fn   ReferralSignupHook
}

type namedReconciliationIssueHook struct {
	name string
	fn   ReconciliationIssueHook
}

// Registry holds module hooks registered at startup.
//
// Breaking change (P1-9, 2026-05-01): every On* method now takes a
// `name` parameter. The name identifies the subscriber in the DLQ
// (`module.hook_failures.hook_name`) and is the key the replay endpoint
// uses to re-locate the hook. Names must be stable across deploys —
// renaming a hook strands its DLQ rows.
type Registry struct {
	onAccountCreated      []namedAccountHook
	onAccountDeleted      []namedAccountHook
	onPlanChanged         []namedPlanChangeHook
	onCheckin             []namedCheckinHook
	onReferralSignup      []namedReferralSignupHook
	onReconciliationIssue []namedReconciliationIssueHook

	dlq         DeadLetterStore // nil ⇒ legacy warn-and-drop fallback
	accountGet  AccountFetcher  // for replay
	metrics     HookMetricsSink // nil-safe
	retryPolicy RetryPolicy
}

// NewRegistry creates an empty module registry with default retry policy.
func NewRegistry() *Registry {
	return &Registry{retryPolicy: defaultRetryPolicy}
}

// WithDLQ wires a dead-letter store. Returns the receiver for chaining.
// nil is allowed and equivalent to the legacy fallback behaviour.
func (r *Registry) WithDLQ(dlq DeadLetterStore) *Registry {
	r.dlq = dlq
	return r
}

// WithAccountFetcher wires the account loader used by replay. Required
// for replay of account-scoped events; pass nil to disable replay (the
// admin endpoint will then 503 cleanly).
func (r *Registry) WithAccountFetcher(f AccountFetcher) *Registry {
	r.accountGet = f
	return r
}

// WithMetrics wires the metrics sink. nil is allowed.
func (r *Registry) WithMetrics(m HookMetricsSink) *Registry {
	r.metrics = m
	return r
}

// WithRetryPolicy overrides the default retry policy. Mostly for tests
// (zero backoff for fast failure tests).
func (r *Registry) WithRetryPolicy(p RetryPolicy) *Registry {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = defaultRetryPolicy.MaxAttempts
	}
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = defaultRetryPolicy.InitialBackoff
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = defaultRetryPolicy.MaxBackoff
	}
	r.retryPolicy = p
	return r
}

// OnAccountCreated registers a hook for the account-created event.
// `name` must be non-empty and stable across deploys.
func (r *Registry) OnAccountCreated(name string, hook AccountHook) {
	mustHookName(name)
	r.onAccountCreated = append(r.onAccountCreated, namedAccountHook{name, hook})
}

// OnAccountDeleted registers a hook for the account-deleted event.
func (r *Registry) OnAccountDeleted(name string, hook AccountHook) {
	mustHookName(name)
	r.onAccountDeleted = append(r.onAccountDeleted, namedAccountHook{name, hook})
}

// OnPlanChanged registers a hook for subscription plan changes.
func (r *Registry) OnPlanChanged(name string, hook PlanChangeHook) {
	mustHookName(name)
	r.onPlanChanged = append(r.onPlanChanged, namedPlanChangeHook{name, hook})
}

// OnCheckin registers a hook for daily check-in events.
func (r *Registry) OnCheckin(name string, hook CheckinHook) {
	mustHookName(name)
	r.onCheckin = append(r.onCheckin, namedCheckinHook{name, hook})
}

// OnReferralSignup registers a hook for referral sign-up events.
func (r *Registry) OnReferralSignup(name string, hook ReferralSignupHook) {
	mustHookName(name)
	r.onReferralSignup = append(r.onReferralSignup, namedReferralSignupHook{name, hook})
}

// OnReconciliationIssue registers a hook for critical reconciliation issues.
func (r *Registry) OnReconciliationIssue(name string, hook ReconciliationIssueHook) {
	mustHookName(name)
	r.onReconciliationIssue = append(r.onReconciliationIssue, namedReconciliationIssueHook{name, hook})
}

// FireAccountCreated invokes all registered account-created hooks with
// retry-and-DLQ semantics. Hook failures never block the caller.
func (r *Registry) FireAccountCreated(ctx context.Context, account *entity.Account) {
	if account == nil {
		return
	}
	payload := accountPayload(account)
	for _, h := range r.onAccountCreated {
		r.runAccountHook(ctx, "account_created", h, account, payload)
	}
}

// FireAccountDeleted invokes all registered account-deleted hooks.
func (r *Registry) FireAccountDeleted(ctx context.Context, account *entity.Account) {
	if account == nil {
		return
	}
	payload := accountPayload(account)
	for _, h := range r.onAccountDeleted {
		r.runAccountHook(ctx, "account_deleted", h, account, payload)
	}
}

// FirePlanChanged invokes all registered plan-changed hooks.
func (r *Registry) FirePlanChanged(ctx context.Context, account *entity.Account, plan *entity.ProductPlan) {
	if account == nil || plan == nil {
		return
	}
	payload := planChangePayload(account, plan)
	accountID := account.ID
	for _, h := range r.onPlanChanged {
		r.runHook(ctx, "plan_changed", h.name, &accountID, payload, func(ctx context.Context) error {
			return h.fn(ctx, account, plan)
		})
	}
}

// FireCheckin invokes all registered check-in hooks.
func (r *Registry) FireCheckin(ctx context.Context, accountID int64, streak int) {
	payload, _ := json.Marshal(map[string]any{
		"account_id": accountID,
		"streak":     streak,
	})
	for _, h := range r.onCheckin {
		acctID := accountID
		r.runHook(ctx, "checkin", h.name, &acctID, payload, func(ctx context.Context) error {
			return h.fn(ctx, accountID, streak)
		})
	}
}

// FireReferralSignup invokes all registered referral sign-up hooks.
func (r *Registry) FireReferralSignup(ctx context.Context, referrerAccountID int64, referredName string) {
	payload, _ := json.Marshal(map[string]any{
		"referrer_account_id": referrerAccountID,
		"referred_name":       referredName,
	})
	for _, h := range r.onReferralSignup {
		acctID := referrerAccountID
		r.runHook(ctx, "referral_signup", h.name, &acctID, payload, func(ctx context.Context) error {
			return h.fn(ctx, referrerAccountID, referredName)
		})
	}
}

// FireReconciliationIssue invokes all registered reconciliation issue hooks.
func (r *Registry) FireReconciliationIssue(ctx context.Context, issue *entity.ReconciliationIssue) {
	if issue == nil {
		return
	}
	payload, _ := json.Marshal(issue)
	var acctIDPtr *int64
	if issue.AccountID != nil {
		v := *issue.AccountID
		acctIDPtr = &v
	}
	for _, h := range r.onReconciliationIssue {
		r.runHook(ctx, "reconciliation_issue", h.name, acctIDPtr, payload, func(ctx context.Context) error {
			return h.fn(ctx, issue)
		})
	}
}

// runAccountHook is the specialization for hooks that take *entity.Account.
func (r *Registry) runAccountHook(ctx context.Context, event string, h namedAccountHook, account *entity.Account, payload json.RawMessage) {
	acctID := account.ID
	r.runHook(ctx, event, h.name, &acctID, payload, func(ctx context.Context) error {
		return h.fn(ctx, account)
	})
}

// runHook is the central retry-and-DLQ engine. Tries up to MaxAttempts
// with exponential-jittered backoff. On terminal failure, persists a
// row in the DLQ (if configured) and emits metrics. The original error
// is logged at WARN on each transient failure, ERROR on terminal.
//
// Caller-supplied closure is responsible for wiring its own params; we
// only own the retry/log/metric/DLQ choreography.
func (r *Registry) runHook(
	ctx context.Context,
	event, hookName string,
	accountID *int64,
	payload json.RawMessage,
	invoke func(ctx context.Context) error,
) {
	var lastErr error
	policy := r.retryPolicy
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := invoke(ctx); err != nil {
			lastErr = err
			if attempt == policy.MaxAttempts {
				break
			}
			slog.WarnContext(ctx, "module hook transient failure",
				"event", event,
				"hook", hookName,
				"attempt", attempt,
				"err", err,
			)
			sleep := backoff(policy, attempt)
			// Respect ctx cancellation between retries.
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				goto giveup
			case <-time.After(sleep):
			}
			continue
		}
		// Success.
		result := "succeeded_first_try"
		if attempt > 1 {
			result = "retry_succeeded"
		}
		if r.metrics != nil {
			r.metrics.RecordHookOutcome(event, hookName, result)
		}
		return
	}

giveup:
	slog.ErrorContext(ctx, "module hook permanently failed — DLQ",
		"event", event,
		"hook", hookName,
		"attempts", policy.MaxAttempts,
		"err", lastErr,
	)
	if r.metrics != nil {
		r.metrics.RecordHookOutcome(event, hookName, "dlq")
	}
	if r.dlq != nil {
		failure := &entity.HookFailure{
			Event:     event,
			HookName:  hookName,
			AccountID: accountID,
			Payload:   payload,
			Error:     lastErr.Error(),
			Attempts:  policy.MaxAttempts,
		}
		// Save in a fresh background ctx so a request-scoped cancel
		// doesn't lose the DLQ row. 5s is generous; the DLQ insert is
		// a single tiny upsert.
		dlqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.dlq.Save(dlqCtx, failure); err != nil {
			slog.ErrorContext(ctx, "module hook DLQ save failed — failure lost",
				"event", event,
				"hook", hookName,
				"err", err,
			)
		}
	}
}

// Replay re-invokes a single named hook for a DLQ row. The fresh
// account is loaded via the wired AccountFetcher (ensures we don't
// retry against a snapshot that's been purged or merged in the
// meantime). Success → mark replayed_at on the row + emit
// "replay_succeeded" metric. Failure → return the error so the admin
// handler can surface it; the row is also re-saved which bumps
// attempts and refreshes last_failed_at.
//
// Currently account-scoped events only (account_created /
// account_deleted / plan_changed / checkin / referral_signup). Replay
// of reconciliation_issue is not supported because the original issue
// payload may already be stale and the hook can't be safely re-run
// from a cold record without re-fetching the issue from the DB; do
// that resync from the reconciliation reconciler's own admin tooling
// instead.
func (r *Registry) Replay(ctx context.Context, f *entity.HookFailure) error {
	if f == nil {
		return errors.New("replay: nil failure row")
	}
	switch f.Event {
	case "account_created", "account_deleted":
		if r.accountGet == nil {
			return errors.New("replay: account fetcher not configured")
		}
		if f.AccountID == nil {
			return errors.New("replay: missing account_id on row")
		}
		acct, err := r.accountGet(ctx, *f.AccountID)
		if err != nil {
			return err
		}
		if acct == nil {
			// Account purged since failure — record this as a
			// successful replay (the right thing is for the row to
			// stop showing up in pending) and return a typed sentinel
			// so the admin UI can render "account already gone".
			return ErrAccountAlreadyGone
		}
		hook, ok := r.lookupAccountHook(f.Event, f.HookName)
		if !ok {
			return errors.New("replay: hook not registered (renamed or unwired)")
		}
		return hook(ctx, acct)
	default:
		return errors.New("replay: event type not supported by replay endpoint — re-run the source workflow")
	}
}

// ErrAccountAlreadyGone is returned by Replay when the target account
// no longer exists. Admin handler maps this to a 200 "marked replayed"
// response so the row drops out of the pending list.
var ErrAccountAlreadyGone = errors.New("module: account no longer exists")

func (r *Registry) lookupAccountHook(event, name string) (AccountHook, bool) {
	var pool []namedAccountHook
	switch event {
	case "account_created":
		pool = r.onAccountCreated
	case "account_deleted":
		pool = r.onAccountDeleted
	default:
		return nil, false
	}
	for _, h := range pool {
		if h.name == name {
			return h.fn, true
		}
	}
	return nil, false
}

// HookCount returns the total number of registered hooks (useful for startup logging).
func (r *Registry) HookCount() int {
	return len(r.onAccountCreated) + len(r.onAccountDeleted) + len(r.onPlanChanged) +
		len(r.onCheckin) + len(r.onReferralSignup) + len(r.onReconciliationIssue)
}

// --- helpers ---

func mustHookName(name string) {
	if name == "" {
		panic("module.Registry: hook name must be non-empty (used as DLQ key)")
	}
}

func accountPayload(a *entity.Account) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"account_id":   a.ID,
		"username":     a.Username,
		"email":        a.Email,
		"display_name": a.DisplayName,
		"lurus_id":     a.LurusID,
	})
	return b
}

func planChangePayload(a *entity.Account, p *entity.ProductPlan) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"account_id": a.ID,
		"plan_id":    p.ID,
		"plan_code":  p.Code,
		"product_id": p.ProductID,
	})
	return b
}

// backoff = base * 2^(attempt-1), clipped to MaxBackoff, with ±jitter.
func backoff(p RetryPolicy, attempt int) time.Duration {
	mult := 1 << (attempt - 1)
	d := time.Duration(mult) * p.InitialBackoff
	if d > p.MaxBackoff {
		d = p.MaxBackoff
	}
	if p.JitterFraction > 0 {
		jitter := (rand.Float64()*2 - 1) * p.JitterFraction // -frac..+frac
		d = time.Duration(float64(d) * (1 + jitter))
		if d < 0 {
			d = 0
		}
	}
	return d
}
