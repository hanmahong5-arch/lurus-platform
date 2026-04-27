package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

// AccountPurgeOrchestrator is the subset of AccountService the executor
// needs. Declared as an interface so unit tests can provide an
// in-memory fake without standing up the full account/wallet/vip
// store stack. Mirrors the pattern QROrgService uses inside
// qr_handler.go for OrganizationService.
//
// app.AccountService satisfies this interface — wiring at boot stays
// `NewAccountDeleteExecutor(accountSvc, ...)` with no extra adapter.
type AccountPurgeOrchestrator interface {
	BeginPurge(ctx context.Context, req app.PurgeBeginRequest) (int64, error)
	FinishPurge(ctx context.Context, req app.FinishPurgeRequest) error
	GetByID(ctx context.Context, id int64) (*entity.Account, error)
}

// AccountSubscriptionLister is the optional subscription side of the
// cascade. Allows tests to inject a fake; production passes
// *app.SubscriptionService which already has these methods.
type AccountSubscriptionLister interface {
	ListByAccount(ctx context.Context, accountID int64) ([]entity.Subscription, error)
	Cancel(ctx context.Context, accountID int64, productID string) error
}

// AccountWalletDebiter is the optional wallet side of the cascade.
// Production passes *app.WalletService.
type AccountWalletDebiter interface {
	GetBalance(ctx context.Context, accountID int64) (*entity.Wallet, error)
	Debit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error)
}

// AccountZitadelDeactivator is the optional Zitadel side of the
// cascade. Production passes *zitadel.Client.
type AccountZitadelDeactivator interface {
	DeactivateUser(ctx context.Context, userID string) error
}

// AccountDeleteExecutor implements QRDelegateExecutor for the
// delete_account op (Phase 4 / Sprint 1A — GDPR-grade account purge).
//
// Wiring: the executor is registered with QRHandler at boot via
// qrH.WithDelegateExecutor(NewAccountDeleteExecutor(...)). Once
// registered it accepts ops listed in SupportedOps; mismatches surface
// as ErrUnsupportedDelegateOp so callers can return 400.
//
// The cascade order is documented inline: it's tuned so that a
// partial failure leaves the user in a "still has data, cannot log
// in" state rather than the inverse (logged in but data gone).
type AccountDeleteExecutor struct {
	accounts AccountPurgeOrchestrator
	subs     AccountSubscriptionLister
	wallets  AccountWalletDebiter
	// zitadel disables the OIDC user so subsequent login attempts are
	// rejected. nil-tolerant — when Zitadel is not wired (single-node
	// dev), the cascade emits a warn log and proceeds without the
	// disable step rather than failing the whole purge.
	zitadel AccountZitadelDeactivator
}

// NewAccountDeleteExecutor wires the executor. accounts is required;
// subs / wallets / zitadel may be nil and the cascade degrades each
// step individually with a warn-level audit line. The concrete
// production types — *app.AccountService, *app.SubscriptionService,
// *app.WalletService, *zitadel.Client — all satisfy these interfaces
// structurally, so callers pass them in directly.
func NewAccountDeleteExecutor(
	accounts AccountPurgeOrchestrator,
	subs AccountSubscriptionLister,
	wallets AccountWalletDebiter,
	zit AccountZitadelDeactivator,
) *AccountDeleteExecutor {
	return &AccountDeleteExecutor{
		accounts: accounts,
		subs:     subs,
		wallets:  wallets,
		zitadel:  zit,
	}
}

// Compile-time check: production types satisfy the executor's
// interfaces. Misses on this line catch interface drift at build time
// rather than at boot when main.go panics on a typed-nil assertion.
var (
	_ AccountPurgeOrchestrator   = (*app.AccountService)(nil)
	_ AccountSubscriptionLister  = (*app.SubscriptionService)(nil)
	_ AccountWalletDebiter       = (*app.WalletService)(nil)
	_ AccountZitadelDeactivator  = (*zitadel.Client)(nil)
)

// SupportedOps returns the delegate ops this executor handles.
func (e *AccountDeleteExecutor) SupportedOps() []string {
	return []string{qrDelegateOpDeleteAccount}
}

// ExecuteDelegate runs the GDPR purge cascade. callerID is the boss's
// scanner account (the approver) — distinct from the audit-row's
// InitiatedBy which was captured at QR mint time.
//
// Returned errors are wrapped with context but never include PII —
// the audit row carries the full target (account_id) and the slog
// audit line records the cascade outcome.
func (e *AccountDeleteExecutor) ExecuteDelegate(ctx context.Context, params QRDelegateParams, callerID int64) error {
	if params.Op != qrDelegateOpDeleteAccount {
		return fmt.Errorf("%w: %q", ErrUnsupportedDelegateOp, params.Op)
	}
	if e.accounts == nil {
		return errors.New("account_delete: accounts service not wired")
	}
	if params.AccountID <= 0 {
		return errors.New("account_delete: missing target account_id")
	}

	// 1. Look up the audit row started by AccountAdminHandler at mint
	//    time. That handler called accounts.BeginPurge which inserted
	//    the row and locked out concurrent attempts via the partial
	//    UNIQUE index. We re-call BeginPurge here because the executor
	//    on the APP confirm path is the source of truth — if the same
	//    initiator scanned twice, the second confirm should hit
	//    ErrPurgeInFlight rather than silently double-running.
	purgeID, err := e.accounts.BeginPurge(ctx, app.PurgeBeginRequest{
		AccountID:   params.AccountID,
		InitiatedBy: callerID, // best available — scanner is also the approver in MVP
	})
	if err != nil {
		if errors.Is(err, app.ErrAccountAlreadyPurged) {
			// Idempotent: the desired end-state already holds. Emit a
			// warn-level audit so re-confirm attempts are visible.
			slog.WarnContext(ctx, "account_delete.idempotent_already_purged",
				"account_id", params.AccountID, "approver", callerID)
			return nil
		}
		return fmt.Errorf("account_delete: begin: %w", err)
	}

	// 2. Run the cascade. Each step logs its own warn line on
	//    failure but does NOT abort the whole flow — the more steps
	//    that complete, the less data the user has lingering. After
	//    the cascade we record the aggregate success/failure on the
	//    audit row.
	cascadeErr := e.runCascade(ctx, params.AccountID)

	// 3. Persist outcome. FinishPurge atomically flips the audit row
	//    to completed/failed AND (on success) flips the account row
	//    to status=Deleted.
	finishReq := app.FinishPurgeRequest{
		PurgeID:    purgeID,
		AccountID:  params.AccountID,
		ApprovedBy: callerID,
		Success:    cascadeErr == nil,
	}
	if cascadeErr != nil {
		finishReq.ErrMsg = cascadeErr.Error()
	}
	if err := e.accounts.FinishPurge(ctx, finishReq); err != nil {
		// Cascade-end logging trumps the finish error — the cascade
		// outcome is the user-visible one. Wrap so operators see both.
		slog.ErrorContext(ctx, "account_delete.finish_purge_failed",
			"account_id", params.AccountID, "purge_id", purgeID, "err", err)
		if cascadeErr != nil {
			return fmt.Errorf("account_delete: cascade %w; finish: %v", cascadeErr, err)
		}
		return fmt.Errorf("account_delete: finish: %w", err)
	}
	if cascadeErr != nil {
		return fmt.Errorf("account_delete: cascade: %w", cascadeErr)
	}
	slog.InfoContext(ctx, "account_delete.completed",
		"account_id", params.AccountID,
		"purge_id", purgeID,
		"approver", callerID)
	return nil
}

// runCascade executes the per-domain cleanup steps. Returns the first
// error encountered — but every step that CAN run does run, so even a
// failure in (say) the wallet step doesn't block the Zitadel
// disable. This trades "first error wins" for "best-effort wipe",
// which matches GDPR's "as much as possible, as soon as possible"
// stance better than a strict abort-on-first-error chain.
func (e *AccountDeleteExecutor) runCascade(ctx context.Context, accountID int64) error {
	a, err := e.accounts.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("lookup target: %w", err)
	}
	if a == nil {
		return fmt.Errorf("target %d not found", accountID)
	}

	var firstErr error
	captureErr := func(step string, err error) {
		if err == nil {
			return
		}
		slog.WarnContext(ctx, "account_delete.step_failed",
			"step", step, "account_id", accountID, "err", err)
		if firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", step, err)
		}
	}

	// Cancel all active subscriptions. Iterates per-product so each
	// Cancel call hits the existing subSvc.Cancel idempotency.
	if e.subs != nil {
		all, listErr := e.subs.ListByAccount(ctx, accountID)
		if listErr != nil {
			captureErr("subs.list", listErr)
		} else {
			for _, sub := range all {
				if sub.Status != entity.SubStatusActive && sub.Status != entity.SubStatusGrace {
					continue
				}
				if err := e.subs.Cancel(ctx, accountID, sub.ProductID); err != nil {
					captureErr("subs.cancel:"+sub.ProductID, err)
				}
			}
		}
	}

	// Zero out wallet balance. Skips when balance is already 0 so a
	// re-run is a clean no-op.
	if e.wallets != nil {
		w, walletErr := e.wallets.GetBalance(ctx, accountID)
		if walletErr != nil {
			captureErr("wallet.balance", walletErr)
		} else if w != nil && w.Balance > 0 {
			_, err := e.wallets.Debit(ctx, accountID, w.Balance,
				"purge", "account purge zero-out", "account_purge", fmt.Sprintf("%d", accountID), "")
			captureErr("wallet.debit", err)
		}
	}

	// Disable Zitadel user so OIDC logins start failing immediately.
	// We need the zitadel_sub from the account row — looked up above.
	if e.zitadel != nil {
		if a.ZitadelSub != "" {
			captureErr("zitadel.deactivate", e.zitadel.DeactivateUser(ctx, a.ZitadelSub))
		} else {
			slog.InfoContext(ctx, "account_delete.skip_zitadel_no_sub",
				"account_id", accountID)
		}
	}

	return firstErr
}
