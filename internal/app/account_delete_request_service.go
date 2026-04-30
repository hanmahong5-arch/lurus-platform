package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// CoolingOffPeriod is the time the user has to change their mind
// after submitting a self-delete request. 30 days matches industry
// norm (Apple, Google, Meta) and PIPL §47 implementation guidance —
// long enough to recover from impulse, short enough to comply with
// the "without undue delay" clause.
const CoolingOffPeriod = 30 * 24 * time.Hour

// accountDeleteRequestStore is the persistence interface
// AccountDeleteRequestService depends on. Implemented by
// repo.AccountDeleteRequestRepo. Declared here so unit tests can pass
// an in-memory fake without standing up gorm + Postgres.
type accountDeleteRequestStore interface {
	Create(ctx context.Context, req *entity.AccountDeleteRequest) error
	GetPending(ctx context.Context, accountID int64) (*entity.AccountDeleteRequest, error)
}

// ErrDeleteRequestPending is the app-layer sentinel matched by the
// handler to recognise the idempotent re-submit case. Returned by
// RequestSelfDelete when a pending row already exists for the account.
// Distinct from the repo's ErrDeleteRequestPending only so the handler
// import surface stays in /app.
var ErrDeleteRequestPending = errors.New("account: delete request already pending")

// ErrAccountAlreadyDeleted signals the account is already in the
// terminal Deleted status — there is nothing to request. The handler
// surfaces this as a 200 idempotent response with status "already_deleted".
var ErrAccountAlreadyDeleted = errors.New("account: already deleted")

// AccountDeleteRequestService orchestrates user-self-initiated account
// deletion requests. Exposes the request primitive only — the actual
// purge cascade is dispatched by a separate worker (Sprint 1B) which
// reads pending rows whose CoolingOffUntil < now() and reuses the
// existing AccountDeleteExecutor.
//
// Designed as a sibling to AccountService (rather than methods on it)
// so the user-self flow can ship without touching every existing
// AccountService caller. AccountService is already wired into
// 30+ handlers; widening its surface for one new endpoint would
// violate Karpathy's "surgical changes" rule.
type AccountDeleteRequestService struct {
	store    accountDeleteRequestStore
	accounts *AccountService
	subs     *SubscriptionService
}

// NewAccountDeleteRequestService wires the service. accounts is required
// for the pre-flight account lookup; subs is optional — when nil the
// "open subscriptions" 409 check is skipped (single-node dev / tests).
func NewAccountDeleteRequestService(store accountDeleteRequestStore, accounts *AccountService) *AccountDeleteRequestService {
	return &AccountDeleteRequestService{store: store, accounts: accounts}
}

// WithSubscriptionGuard wires the optional 409-on-active-subs check.
// Chainable. Pass nil to disable.
func (s *AccountDeleteRequestService) WithSubscriptionGuard(subs *SubscriptionService) *AccountDeleteRequestService {
	s.subs = subs
	return s
}

// SelfDeleteRequest is the input shape for RequestSelfDelete. Reason
// validation is the handler's job; this layer trusts the caller.
type SelfDeleteRequest struct {
	AccountID  int64
	Reason     string
	ReasonText string
}

// SelfDeleteResult is the success shape returned to the handler.
// Idempotent flag tells the handler whether to log "created" or
// "already_pending" — both surface as 200 OK to the client, but the
// audit log distinguishes them.
type SelfDeleteResult struct {
	RequestID       int64
	Status          string
	CoolingOffUntil time.Time
	Idempotent      bool
}

// ErrAccountHasActiveSubscription signals the account still has a
// non-cancelled subscription. The handler maps this to 409 Conflict
// with an actionable message ("cancel your subscription first") so the
// user is not left wondering why the destructive button did nothing.
var ErrAccountHasActiveSubscription = errors.New("account: has active subscriptions")

// RequestSelfDelete registers the user's intent to delete their account
// and returns the request id. Idempotent: re-submitting while a request
// is pending returns the existing row with Idempotent=true rather than
// creating a duplicate.
func (s *AccountDeleteRequestService) RequestSelfDelete(ctx context.Context, req SelfDeleteRequest) (*SelfDeleteResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("account_delete_request: store not wired")
	}
	if req.AccountID <= 0 {
		return nil, fmt.Errorf("account_delete_request: missing account_id")
	}

	a, err := s.accounts.GetByID(ctx, req.AccountID)
	if err != nil {
		return nil, fmt.Errorf("lookup account: %w", err)
	}
	if a == nil {
		return nil, fmt.Errorf("account: %d not found", req.AccountID)
	}
	if a.Status == entity.AccountStatusDeleted {
		return nil, ErrAccountAlreadyDeleted
	}

	// Idempotency check: surface the existing pending row instead of
	// hitting the unique-index error path. Both branches succeed; this
	// just keeps the happy path in the SELECT half of the connection
	// pool rather than burning an INSERT to discover the conflict.
	if existing, err := s.store.GetPending(ctx, req.AccountID); err != nil {
		return nil, fmt.Errorf("lookup pending: %w", err)
	} else if existing != nil {
		return &SelfDeleteResult{
			RequestID:       existing.ID,
			Status:          existing.Status,
			CoolingOffUntil: existing.CoolingOffUntil,
			Idempotent:      true,
		}, nil
	}

	// Active-subscription guard. Skipped when the subscription service
	// is not wired (tests / minimal deployments). Treats "active" and
	// "grace" as blocking — both states still entitle the user to the
	// product so a self-delete would silently terminate paid access.
	if s.subs != nil {
		all, err := s.subs.ListByAccount(ctx, req.AccountID)
		if err != nil {
			return nil, fmt.Errorf("list subs: %w", err)
		}
		for _, sub := range all {
			if sub.Status == entity.SubStatusActive || sub.Status == entity.SubStatusGrace {
				return nil, ErrAccountHasActiveSubscription
			}
		}
	}

	now := time.Now().UTC()
	row := &entity.AccountDeleteRequest{
		AccountID:       req.AccountID,
		RequestedBy:     req.AccountID, // self-service: requester == target
		Status:          entity.AccountDeleteRequestStatusPending,
		Reason:          req.Reason,
		ReasonText:      req.ReasonText,
		CoolingOffUntil: now.Add(CoolingOffPeriod),
		RequestedAt:     now,
	}
	if err := s.store.Create(ctx, row); err != nil {
		// Race: another concurrent submission won the unique-index
		// insert between our GetPending and Create. Re-read and surface
		// the winner row so the user sees a coherent idempotent shape.
		// We treat any "already pending" signal from the repo as the
		// race outcome — the partial unique index has only one possible
		// origin.
		if isAlreadyPendingErr(err) {
			if existing, gerr := s.store.GetPending(ctx, req.AccountID); gerr == nil && existing != nil {
				return &SelfDeleteResult{
					RequestID:       existing.ID,
					Status:          existing.Status,
					CoolingOffUntil: existing.CoolingOffUntil,
					Idempotent:      true,
				}, nil
			}
			return nil, ErrDeleteRequestPending
		}
		return nil, fmt.Errorf("create request: %w", err)
	}

	return &SelfDeleteResult{
		RequestID:       row.ID,
		Status:          row.Status,
		CoolingOffUntil: row.CoolingOffUntil,
		Idempotent:      false,
	}, nil
}

// isAlreadyPendingErr matches both the repo's typed sentinel and any
// future implementation that wraps it. Kept tolerant so a swap of the
// underlying repo (in-memory test fake → gorm) does not silently break
// the race-loser path above.
func isAlreadyPendingErr(err error) bool {
	if err == nil {
		return false
	}
	// Match the repo sentinel by string to avoid an /app → /repo import
	// cycle. The repo's ErrDeleteRequestPending message is stable.
	if errors.Is(err, ErrDeleteRequestPending) {
		return true
	}
	return err.Error() == "repo: account delete request already pending"
}
