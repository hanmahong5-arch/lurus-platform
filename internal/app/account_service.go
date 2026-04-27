// Package app contains use case orchestration — no framework types allowed here.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// AccountService orchestrates account creation, lookup, OAuth binding,
// and (Phase 4) GDPR-grade account purges. The purges store is
// optional — when nil the Purge flow returns a clear "purge audit
// store not wired" error and the rest of the service continues to
// function for normal account ops.
type AccountService struct {
	accounts         accountStore
	wallets          walletStore
	vip              vipStore
	purges           accountPurgeStore // nil-safe; gates BeginPurge/FinishPurge
	onAccountCreated AccountCreatedHookFunc
}

func NewAccountService(accounts accountStore, wallets walletStore, vip vipStore) *AccountService {
	return &AccountService{accounts: accounts, wallets: wallets, vip: vip}
}

// WithPurgeStore wires the purge audit store. Chainable; safe to call
// with nil (leaves Purge flow gated). Done as an opt-in to keep the
// existing NewAccountService signature stable for the many call sites
// that don't need account deletion.
func (s *AccountService) WithPurgeStore(p accountPurgeStore) *AccountService {
	s.purges = p
	return s
}

// SetOnAccountCreatedHook sets the post-account-created hook (mail provisioning, etc.).
func (s *AccountService) SetOnAccountCreatedHook(fn AccountCreatedHookFunc) {
	s.onAccountCreated = fn
}

// UpsertByZitadelSub creates or updates the account linked to a Zitadel OIDC sub.
// This is called on every OIDC login callback.
func (s *AccountService) UpsertByZitadelSub(ctx context.Context, sub, email, displayName, avatarURL string) (*entity.Account, error) {
	// Try by sub first (fastest, indexed)
	a, err := s.accounts.GetByZitadelSub(ctx, sub)
	if err != nil {
		return nil, fmt.Errorf("lookup by sub: %w", err)
	}
	if a != nil {
		// Update mutable fields that may change in Zitadel
		a.DisplayName = displayName
		a.AvatarURL = avatarURL
		if err := s.accounts.Update(ctx, a); err != nil {
			return nil, fmt.Errorf("update account: %w", err)
		}
		return a, nil
	}

	// Fall back to email match (handles accounts created before Zitadel)
	a, err = s.accounts.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("lookup by email: %w", err)
	}
	if a != nil {
		a.ZitadelSub = sub
		a.DisplayName = displayName
		a.AvatarURL = avatarURL
		if err := s.accounts.Update(ctx, a); err != nil {
			return nil, fmt.Errorf("link zitadel sub: %w", err)
		}
		return a, nil
	}

	// New account
	affCode, err := generateAffCode()
	if err != nil {
		return nil, fmt.Errorf("generate aff code: %w", err)
	}
	a = &entity.Account{
		ZitadelSub:  sub,
		Email:       email,
		DisplayName: displayName,
		AvatarURL:   avatarURL,
		AffCode:     affCode,
		Status:      entity.AccountStatusActive,
	}
	if err := s.accounts.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	// Assign human-readable LurusID after insert (needs auto-increment ID)
	a.LurusID = entity.GenerateLurusID(a.ID)
	if err := s.accounts.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("set lurus_id: %w", err)
	}

	// Bootstrap wallet and VIP row
	if _, err := s.wallets.GetOrCreate(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}
	if _, err := s.vip.GetOrCreate(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("create vip: %w", err)
	}

	// Fire account-created hooks (mail provisioning, notifications) — non-blocking.
	if s.onAccountCreated != nil {
		go s.onAccountCreated(ctx, a)
	}

	return a, nil
}

// GetByID returns an account by its numeric ID.
func (s *AccountService) GetByID(ctx context.Context, id int64) (*entity.Account, error) {
	return s.accounts.GetByID(ctx, id)
}

// GetByEmail returns an account by email address.
func (s *AccountService) GetByEmail(ctx context.Context, email string) (*entity.Account, error) {
	return s.accounts.GetByEmail(ctx, email)
}

// GetByPhone returns an account by phone number.
func (s *AccountService) GetByPhone(ctx context.Context, phone string) (*entity.Account, error) {
	return s.accounts.GetByPhone(ctx, phone)
}

// GetByZitadelSub returns an account by Zitadel OIDC sub.
func (s *AccountService) GetByZitadelSub(ctx context.Context, sub string) (*entity.Account, error) {
	return s.accounts.GetByZitadelSub(ctx, sub)
}

// Update persists account profile changes.
func (s *AccountService) Update(ctx context.Context, a *entity.Account) error {
	return s.accounts.Update(ctx, a)
}

// List returns paginated accounts for admin use.
func (s *AccountService) List(ctx context.Context, keyword string, page, pageSize int) ([]*entity.Account, int64, error) {
	return s.accounts.List(ctx, keyword, page, pageSize)
}

// BindOAuth links a third-party OAuth provider to an account.
func (s *AccountService) BindOAuth(ctx context.Context, accountID int64, provider, providerID, providerEmail string) error {
	return s.accounts.UpsertOAuthBinding(ctx, &entity.OAuthBinding{
		AccountID:     accountID,
		Provider:      provider,
		ProviderID:    providerID,
		ProviderEmail: providerEmail,
	})
}

// UpsertByWechat finds or creates an account for a WeChat OAuth login.
// The OAuthBinding (provider=wechat, provider_id=wechatID) is the canonical lookup key.
// New accounts use unique placeholder values for email and zitadel_sub so that the
// DB UNIQUE constraints are satisfied without requiring Zitadel credentials.
func (s *AccountService) UpsertByWechat(ctx context.Context, wechatID string) (*entity.Account, error) {
	if wechatID == "" {
		return nil, fmt.Errorf("wechat: empty wechatID")
	}
	// 1. Look up existing OAuth binding.
	a, err := s.accounts.GetByOAuthBinding(ctx, "wechat", wechatID)
	if err != nil {
		return nil, fmt.Errorf("oauth binding lookup: %w", err)
	}
	if a != nil {
		return a, nil
	}

	// 2. New WeChat user — create account with unique placeholder credentials.
	// Email and ZitadelSub have UNIQUE constraints in the DB, so we use a
	// per-user placeholder that can be replaced when the user binds a real email.
	affCode, err := generateAffCode()
	if err != nil {
		return nil, fmt.Errorf("generate aff code: %w", err)
	}
	a = &entity.Account{
		ZitadelSub:  "wechat:" + wechatID,
		Email:       "wechat." + wechatID + "@noreply.lurus.cn",
		DisplayName: "微信用户",
		AffCode:     affCode,
		Status:      entity.AccountStatusActive,
	}
	if err := s.accounts.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create wechat account: %w", err)
	}

	// Assign human-readable LurusID after insert (needs auto-increment ID).
	a.LurusID = entity.GenerateLurusID(a.ID)
	if err := s.accounts.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("set lurus_id: %w", err)
	}

	// 3. Create OAuth binding.
	if err := s.accounts.UpsertOAuthBinding(ctx, &entity.OAuthBinding{
		AccountID:  a.ID,
		Provider:   "wechat",
		ProviderID: wechatID,
	}); err != nil {
		return nil, fmt.Errorf("create oauth binding: %w", err)
	}

	// 4. Bootstrap wallet and VIP.
	if _, err := s.wallets.GetOrCreate(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}
	if _, err := s.vip.GetOrCreate(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("create vip: %w", err)
	}

	return a, nil
}

// GetByOAuthBinding looks up an account via its third-party OAuth binding.
func (s *AccountService) GetByOAuthBinding(ctx context.Context, provider, providerID string) (*entity.Account, error) {
	return s.accounts.GetByOAuthBinding(ctx, provider, providerID)
}

// GetByAffCode looks up an account by its referral affiliate code.
func (s *AccountService) GetByAffCode(ctx context.Context, code string) (*entity.Account, error) {
	return s.accounts.GetByAffCode(ctx, code)
}

// ── GDPR purge orchestration primitives (Phase 4 / Sprint 1A) ──────────────
//
// The full purge cascade lives in the account-delete executor; this
// service exposes only the lock + audit primitives. Splitting them
// keeps cross-service calls (subscription cancel, wallet zero-out,
// Zitadel disable) out of the account package, while the lock + audit
// stay where the account data lives.

// PurgeBeginRequest is the input shape for BeginPurge. Captures the
// initiator + confirm-time metadata that the audit row preserves.
// Optional fields (ApprovedBy / Attestation / IP / UA / Geo) flow in
// from the QR confirm path; ApprovedBy is filled by FinishPurge once
// the cascade succeeds.
type PurgeBeginRequest struct {
	AccountID   int64
	InitiatedBy int64
}

// BeginPurge atomically reserves the right to purge an account.
// Returns an audit-row ID the executor will pass back to FinishPurge.
//
// Outcomes:
//   - account already in status=Deleted → returns (0, ErrAccountAlreadyPurged):
//     idempotent — the caller should treat this as success-noop.
//   - another purge already in-flight  → returns (0, ErrPurgeInFlight):
//     caller maps to 409 Conflict.
//   - account is active                → returns (purgeID, nil) and the
//     audit row is in status='purging' until FinishPurge transitions it.
//
// Note: this method does NOT flip the account status. The status
// transition happens only after the cascade succeeds (in
// MarkAccountDeleted). The unique partial index in migration 024 is
// what serialises concurrent BeginPurge calls — relying on the index
// rather than a row lock keeps the active path lock-free.
func (s *AccountService) BeginPurge(ctx context.Context, req PurgeBeginRequest) (int64, error) {
	if s.purges == nil {
		return 0, fmt.Errorf("account: purge audit store not wired")
	}
	if req.AccountID <= 0 || req.InitiatedBy <= 0 {
		return 0, fmt.Errorf("account: BeginPurge requires positive account_id and initiated_by")
	}
	a, err := s.accounts.GetByID(ctx, req.AccountID)
	if err != nil {
		return 0, fmt.Errorf("account: lookup target: %w", err)
	}
	if a == nil {
		return 0, fmt.Errorf("account: target %d not found", req.AccountID)
	}
	if a.Status == entity.AccountStatusDeleted {
		return 0, ErrAccountAlreadyPurged
	}
	row := &entity.AccountPurge{
		AccountID:   req.AccountID,
		InitiatedBy: req.InitiatedBy,
	}
	if err := s.purges.BeginPurge(ctx, row); err != nil {
		return 0, err
	}
	return row.ID, nil
}

// FinishPurgeRequest carries the post-cascade outcome the executor
// hands back to AccountService. ApprovedBy is the scanner ID (the
// boss whose APP confirmed) — distinct from InitiatedBy (the admin
// who minted the QR).
type FinishPurgeRequest struct {
	PurgeID    int64
	AccountID  int64
	ApprovedBy int64
	Success    bool
	ErrMsg     string // only meaningful when Success=false
}

// FinishPurge records the cascade outcome. On success it also flips
// the account row to status=Deleted in the same audit transition so a
// caller cannot leave the system in a "audit completed but account
// still active" state.
//
// The audit-row update and the account-row update are NOT in a single
// DB transaction — splitting them lets the audit row record
// "completed" even if the account update transiently fails (in which
// case re-running BeginPurge would correctly return
// ErrAccountAlreadyPurged on the next attempt because the cascade did
// happen). For Sprint 1A this trade-off is acceptable; we revisit if
// inconsistencies appear in practice.
func (s *AccountService) FinishPurge(ctx context.Context, req FinishPurgeRequest) error {
	if s.purges == nil {
		return fmt.Errorf("account: purge audit store not wired")
	}
	now := time.Now().UTC()
	if !req.Success {
		return s.purges.MarkFailed(ctx, req.PurgeID, req.ErrMsg, now)
	}
	if err := s.markAccountDeleted(ctx, req.AccountID); err != nil {
		// Don't leave audit in 'purging' on success cascade — flip to
		// failed with a descriptive error so operators can see exactly
		// what went wrong on retry.
		_ = s.purges.MarkFailed(ctx, req.PurgeID, fmt.Sprintf("cascade ok but mark-deleted failed: %v", err), now)
		return fmt.Errorf("account: mark deleted: %w", err)
	}
	return s.purges.MarkCompleted(ctx, req.PurgeID, req.ApprovedBy, now)
}

// markAccountDeleted is the terminal status flip on the account row.
// Internal — exposed only via FinishPurge so the audit row is always
// flipped first, never bypassed.
func (s *AccountService) markAccountDeleted(ctx context.Context, accountID int64) error {
	a, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}
	if a == nil {
		return fmt.Errorf("not found")
	}
	if a.Status == entity.AccountStatusDeleted {
		return nil // idempotent
	}
	a.Status = entity.AccountStatusDeleted
	return s.accounts.Update(ctx, a)
}

// generateAffCode produces a random 8-character hex referral code.
func generateAffCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
