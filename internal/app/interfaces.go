package app

import (
	"context"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// accountStore is the minimal DB interface required by AccountService.
type accountStore interface {
	Create(ctx context.Context, a *entity.Account) error
	Update(ctx context.Context, a *entity.Account) error
	GetByID(ctx context.Context, id int64) (*entity.Account, error)
	GetByEmail(ctx context.Context, email string) (*entity.Account, error)
	GetByZitadelSub(ctx context.Context, sub string) (*entity.Account, error)
	GetByLurusID(ctx context.Context, lurusID string) (*entity.Account, error)
	GetByAffCode(ctx context.Context, code string) (*entity.Account, error)
	GetByPhone(ctx context.Context, phone string) (*entity.Account, error)
	List(ctx context.Context, keyword string, page, pageSize int) ([]*entity.Account, int64, error)
	UpsertOAuthBinding(ctx context.Context, b *entity.OAuthBinding) error
	GetByUsername(ctx context.Context, username string) (*entity.Account, error)
	// GetByOAuthBinding looks up an account via its OAuth provider binding.
	GetByOAuthBinding(ctx context.Context, provider, providerID string) (*entity.Account, error)
}

// walletStore is the minimal DB interface required by WalletService and VIPService.
type walletStore interface {
	GetOrCreate(ctx context.Context, accountID int64) (*entity.Wallet, error)
	GetByAccountID(ctx context.Context, accountID int64) (*entity.Wallet, error)
	Credit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error)
	Debit(ctx context.Context, accountID int64, amount float64, txType, desc, refType, refID, productID string) (*entity.WalletTransaction, error)
	ListTransactions(ctx context.Context, accountID int64, page, pageSize int) ([]entity.WalletTransaction, int64, error)
	CreatePaymentOrder(ctx context.Context, o *entity.PaymentOrder) error
	UpdatePaymentOrder(ctx context.Context, o *entity.PaymentOrder) error
	GetPaymentOrderByNo(ctx context.Context, orderNo string) (*entity.PaymentOrder, error)
	GetRedemptionCode(ctx context.Context, code string) (*entity.RedemptionCode, error)
	UpdateRedemptionCode(ctx context.Context, rc *entity.RedemptionCode) error
	ListOrders(ctx context.Context, accountID int64, page, pageSize int) ([]entity.PaymentOrder, int64, error)
	// MarkPaymentOrderPaid atomically transitions pending→paid. didTransition=true if this call did the flip.
	MarkPaymentOrderPaid(ctx context.Context, orderNo string) (order *entity.PaymentOrder, didTransition bool, err error)
	// RedeemCode validates + credits inside a single DB transaction (TOCTOU safe).
	RedeemCode(ctx context.Context, accountID int64, code string) (*entity.WalletTransaction, error)
	// ExpireStalePendingOrders marks pending orders older than maxAge as expired.
	ExpireStalePendingOrders(ctx context.Context, maxAge time.Duration) (int64, error)
	// GetPendingOrderByIdempotencyKey returns a pending order matching the key, or nil.
	GetPendingOrderByIdempotencyKey(ctx context.Context, key string) (*entity.PaymentOrder, error)

	// Pre-authorization methods for streaming API calls.
	CreatePreAuth(ctx context.Context, pa *entity.WalletPreAuthorization) error
	GetPreAuthByID(ctx context.Context, id int64) (*entity.WalletPreAuthorization, error)
	GetPreAuthByReference(ctx context.Context, productID, referenceID string) (*entity.WalletPreAuthorization, error)
	// SettlePreAuth atomically transitions active→settled with actual amount.
	SettlePreAuth(ctx context.Context, id int64, actualAmount float64) (*entity.WalletPreAuthorization, error)
	// ReleasePreAuth atomically transitions active→released, unfreezing the hold.
	ReleasePreAuth(ctx context.Context, id int64) (*entity.WalletPreAuthorization, error)
	// ExpireStalePreAuths marks active pre-auths past their deadline as expired and unfreezes.
	ExpireStalePreAuths(ctx context.Context) (int64, error)
	// CountActivePreAuths returns the number of active pre-authorizations for an account.
	CountActivePreAuths(ctx context.Context, accountID int64) (int64, error)
	// CountPendingOrders returns the number of pending payment orders for an account.
	CountPendingOrders(ctx context.Context, accountID int64) (int64, error)

	// Reconciliation
	FindStalePendingOrders(ctx context.Context, minAge time.Duration) ([]entity.PaymentOrder, error)
	FindPaidTopupOrdersWithoutCredit(ctx context.Context) ([]entity.PaidOrderWithoutCredit, error)
	CreateReconciliationIssue(ctx context.Context, issue *entity.ReconciliationIssue) error
	ListReconciliationIssues(ctx context.Context, status string, page, pageSize int) ([]entity.ReconciliationIssue, int64, error)
	ResolveReconciliationIssue(ctx context.Context, id int64, status, resolution string) error
}

// vipStore is the minimal DB interface required by VIPService.
type vipStore interface {
	GetOrCreate(ctx context.Context, accountID int64) (*entity.AccountVIP, error)
	Update(ctx context.Context, v *entity.AccountVIP) error
	ListConfigs(ctx context.Context) ([]entity.VIPLevelConfig, error)
}

// subscriptionStore is the minimal DB interface required by SubscriptionService.
type subscriptionStore interface {
	Create(ctx context.Context, s *entity.Subscription) error
	Update(ctx context.Context, s *entity.Subscription) error
	GetByID(ctx context.Context, id int64) (*entity.Subscription, error)
	GetActive(ctx context.Context, accountID int64, productID string) (*entity.Subscription, error)
	ListByAccount(ctx context.Context, accountID int64) ([]entity.Subscription, error)
	ListActiveExpired(ctx context.Context) ([]entity.Subscription, error)
	ListGraceExpired(ctx context.Context) ([]entity.Subscription, error)
	// ListDueForRenewal returns active subscriptions that are due for auto-renewal.
	ListDueForRenewal(ctx context.Context) ([]entity.Subscription, error)
	// UpdateRenewalState persists renewal attempt counter and next retry time.
	UpdateRenewalState(ctx context.Context, subID int64, attempts int, nextAt *time.Time) error
	UpsertEntitlement(ctx context.Context, e *entity.AccountEntitlement) error
	GetEntitlements(ctx context.Context, accountID int64, productID string) ([]entity.AccountEntitlement, error)
	DeleteEntitlements(ctx context.Context, accountID int64, productID string) error
}

// planStore is the minimal DB interface for product/plan lookups.
type planStore interface {
	GetPlanByID(ctx context.Context, id int64) (*entity.ProductPlan, error)
	ListActive(ctx context.Context) ([]entity.Product, error)
	ListPlans(ctx context.Context, productID string) ([]entity.ProductPlan, error)
	GetByID(ctx context.Context, id string) (*entity.Product, error)
	Create(ctx context.Context, p *entity.Product) error
	Update(ctx context.Context, p *entity.Product) error
	CreatePlan(ctx context.Context, p *entity.ProductPlan) error
	UpdatePlan(ctx context.Context, p *entity.ProductPlan) error
}

// entitlementCache is the minimal cache interface for entitlement lookups.
// Uses map[string]string to avoid importing the cache package (cache.EntitlementMap is that type).
type entitlementCache interface {
	Get(ctx context.Context, accountID int64, productID string) (map[string]string, error)
	Set(ctx context.Context, accountID int64, productID string, em map[string]string) error
	Invalidate(ctx context.Context, accountID int64, productID string) error
}

// invoiceStore defines persistence operations for invoices.
type invoiceStore interface {
	Create(ctx context.Context, inv *entity.Invoice) error
	GetByOrderNo(ctx context.Context, orderNo string) (*entity.Invoice, error)
	GetByInvoiceNo(ctx context.Context, invoiceNo string) (*entity.Invoice, error)
	ListByAccount(ctx context.Context, accountID int64, page, pageSize int) ([]entity.Invoice, int64, error)
	AdminList(ctx context.Context, filterAccountID int64, page, pageSize int) ([]entity.Invoice, int64, error)
}

// redemptionCodeStore defines persistence operations for bulk redemption code creation.
type redemptionCodeStore interface {
	BulkCreate(ctx context.Context, codes []entity.RedemptionCode) error
}

// refundStore defines persistence operations for refunds.
type refundStore interface {
	Create(ctx context.Context, r *entity.Refund) error
	GetByRefundNo(ctx context.Context, refundNo string) (*entity.Refund, error)
	GetPendingByOrderNo(ctx context.Context, orderNo string) (*entity.Refund, error)
	// UpdateStatus atomically transitions fromStatus→toStatus; fails if the row is not in fromStatus.
	UpdateStatus(ctx context.Context, refundNo, fromStatus, toStatus, reviewNote, reviewedBy string, reviewedAt *time.Time) error
	MarkCompleted(ctx context.Context, refundNo string, completedAt time.Time) error
	ListByAccount(ctx context.Context, accountID int64, page, pageSize int) ([]entity.Refund, int64, error)
}

// overviewCache is the minimal cache interface required by OverviewService.
// Stores and retrieves serialized AccountOverview JSON bytes to avoid cross-package imports.
type overviewCache interface {
	Get(ctx context.Context, accountID int64, productID string) ([]byte, error)
	Set(ctx context.Context, accountID int64, productID string, data []byte) error
	Invalidate(ctx context.Context, accountID int64, productID string) error
}

// referralStatsStore supports querying aggregated referral reward statistics.
type referralStatsStore interface {
	GetReferralStats(ctx context.Context, referrerAccountID int64) (totalReferrals int, totalRewardedLB float64, err error)
}

// rewardEventStore records referral reward events with UNIQUE dedup.
type rewardEventStore interface {
	// CreateRewardEvent inserts a reward event. Returns false if a duplicate (same referrer+referee+event_type) exists.
	CreateRewardEvent(ctx context.Context, ev *entity.ReferralRewardEvent) (created bool, err error)
}

// accountPurgeStore is the minimal DB interface required by
// AccountService for the GDPR-grade account purge flow. Implemented
// by repo.AccountPurgeRepo.
type accountPurgeStore interface {
	BeginPurge(ctx context.Context, p *entity.AccountPurge) error
	MarkCompleted(ctx context.Context, purgeID, approvedBy int64, completedAt time.Time) error
	MarkFailed(ctx context.Context, purgeID int64, errMsg string, completedAt time.Time) error
}

// ErrAccountAlreadyPurged is returned by AccountService.BeginPurge
// when the target account is already in status=Deleted. Callers map
// this to an idempotent 200 response (the desired end state already
// holds), not 409.
var ErrAccountAlreadyPurged = errorString("account already purged")

// ErrPurgeInFlight is returned by AccountService.BeginPurge when
// another purge attempt is already running for the same account.
// Callers map this to 409 Conflict so the second admin sees a clear
// signal rather than a silent duplicate cascade.
var ErrPurgeInFlight = errorString("account purge already in flight")

// errorString is a lightweight typed-string error so callers can
// errors.Is-match the sentinels above without depending on a heavier
// errors package import surface.
type errorString string

func (e errorString) Error() string { return string(e) }

// orgStore is the minimal DB interface required by OrganizationService.
type orgStore interface {
	// organization CRUD
	Create(ctx context.Context, org *entity.Organization) error
	GetByID(ctx context.Context, id int64) (*entity.Organization, error)
	GetBySlug(ctx context.Context, slug string) (*entity.Organization, error)
	ListByAccountID(ctx context.Context, accountID int64) ([]entity.Organization, error)
	UpdateStatus(ctx context.Context, id int64, status string) error
	ListAll(ctx context.Context, limit, offset int) ([]entity.Organization, error)
	// members
	AddMember(ctx context.Context, m *entity.OrgMember) error
	RemoveMember(ctx context.Context, orgID, accountID int64) error
	GetMember(ctx context.Context, orgID, accountID int64) (*entity.OrgMember, error)
	ListMembers(ctx context.Context, orgID int64) ([]entity.OrgMember, error)
	// api keys
	CreateAPIKey(ctx context.Context, k *entity.OrgAPIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*entity.OrgAPIKey, error)
	ListAPIKeys(ctx context.Context, orgID int64) ([]entity.OrgAPIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) error
	TouchAPIKey(ctx context.Context, id int64) error
	// wallet
	GetOrCreateWallet(ctx context.Context, orgID int64) (*entity.OrgWallet, error)
}
