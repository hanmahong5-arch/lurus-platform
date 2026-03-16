package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// topupURL is the deep-link destination for wallet top-up across all products.
const topupURL = "https://identity.lurus.cn/wallet/topup"

// vipLevelEN maps VIP level numbers to their English display names.
var vipLevelEN = map[int16]string{
	0: "Standard",
	1: "Silver",
	2: "Gold",
	3: "Platinum",
	4: "Diamond",
}

// AccountSummary carries the identity fields returned in AccountOverview.
type AccountSummary struct {
	ID          int64  `json:"id"`
	LurusID     string `json:"lurus_id"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

// VIPSummary carries the VIP tier fields returned in AccountOverview.
type VIPSummary struct {
	Level          int16      `json:"level"`
	LevelName      string     `json:"level_name"`
	LevelEN        string     `json:"level_en"`
	Points         int64      `json:"points"`
	LevelExpiresAt *time.Time `json:"level_expires_at"`
}

// WalletSummary carries the wallet balance fields returned in AccountOverview.
type WalletSummary struct {
	Balance      float64 `json:"balance"`
	Frozen       float64 `json:"frozen"`
	DiscountRate float64 `json:"discount_rate"`   // e.g. 0.95 = 5% off
	DiscountTier string  `json:"discount_tier"`   // none / silver_holder / gold_holder / diamond_holder
}

// holderDiscountTier maps LB balance thresholds to discount tiers.
// Balances are evaluated at request time (no historical average needed).
type holderTier struct {
	minBalance   float64
	discountRate float64
	tier         string
}

var holderTiers = []holderTier{
	{minBalance: 2000, discountRate: 0.85, tier: "diamond_holder"},
	{minBalance: 500, discountRate: 0.90, tier: "gold_holder"},
	{minBalance: 100, discountRate: 0.95, tier: "silver_holder"},
	{minBalance: 0, discountRate: 1.00, tier: "none"},
}

// computeDiscount returns the discount rate and tier for a given wallet balance.
func computeDiscount(balance float64) (rate float64, tier string) {
	for _, t := range holderTiers {
		if balance >= t.minBalance {
			return t.discountRate, t.tier
		}
	}
	return 1.00, "none"
}

// SubscriptionSummary carries the subscription fields returned in AccountOverview.
type SubscriptionSummary struct {
	ProductID string     `json:"product_id"`
	PlanCode  string     `json:"plan_code"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at"`
	AutoRenew bool       `json:"auto_renew"`
}

// AccountOverview is the aggregated read model served by the overview endpoint.
// Subscription is nil when no product_id is provided or no active subscription exists.
type AccountOverview struct {
	Account      AccountSummary       `json:"account"`
	VIP          VIPSummary           `json:"vip"`
	Wallet       WalletSummary        `json:"wallet"`
	Subscription *SubscriptionSummary `json:"subscription"`
	TopupURL     string               `json:"topup_url"`
}

// OverviewService aggregates account identity, VIP, wallet and subscription data
// into a single cacheable read model for the overview endpoint.
type OverviewService struct {
	accounts accountStore
	vip      *VIPService
	wallets  walletStore
	subs     *SubscriptionService
	plans    planStore
	cache    overviewCache
}

// NewOverviewService creates a new OverviewService with the given dependencies.
func NewOverviewService(
	accounts accountStore,
	vip *VIPService,
	wallets walletStore,
	subs *SubscriptionService,
	plans planStore,
	cache overviewCache,
) *OverviewService {
	return &OverviewService{
		accounts: accounts,
		vip:      vip,
		wallets:  wallets,
		subs:     subs,
		plans:    plans,
		cache:    cache,
	}
}

// Get returns the aggregated AccountOverview for the given account.
// When productID is non-empty the active subscription for that product is included.
// Results are cached in Redis for 2 minutes; a cache miss triggers a full DB round-trip.
func (s *OverviewService) Get(ctx context.Context, accountID int64, productID string) (*AccountOverview, error) {
	// Cache read — treat any cache error as a miss to stay resilient.
	if cached, err := s.cache.Get(ctx, accountID, productID); err == nil && cached != nil {
		var ov AccountOverview
		if json.Unmarshal(cached, &ov) == nil {
			return &ov, nil
		}
	}

	ov, err := s.compute(ctx, accountID, productID)
	if err != nil {
		return nil, err
	}

	// Cache write is best-effort; failures are silently ignored.
	if b, merr := json.Marshal(ov); merr == nil {
		_ = s.cache.Set(ctx, accountID, productID, b)
	}

	return ov, nil
}

func (s *OverviewService) compute(ctx context.Context, accountID int64, productID string) (*AccountOverview, error) {
	a, err := s.accounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("overview get account: %w", err)
	}
	if a == nil {
		return nil, fmt.Errorf("overview: account %d not found", accountID)
	}

	vipInfo, err := s.vip.Get(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("overview get vip: %w", err)
	}

	wallet, err := s.wallets.GetOrCreate(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("overview get wallet: %w", err)
	}

	levelEN := vipLevelEN[vipInfo.Level]
	if levelEN == "" {
		levelEN = "Standard"
	}

	ov := &AccountOverview{
		Account: AccountSummary{
			ID:          a.ID,
			LurusID:     a.LurusID,
			DisplayName: a.DisplayName,
			AvatarURL:   a.AvatarURL,
		},
		VIP: VIPSummary{
			Level:          vipInfo.Level,
			LevelName:      vipInfo.LevelName,
			LevelEN:        levelEN,
			Points:         vipInfo.Points,
			LevelExpiresAt: vipInfo.LevelExpiresAt,
		},
		Wallet: func() WalletSummary {
			rate, tier := computeDiscount(wallet.Balance)
			return WalletSummary{
				Balance:      wallet.Balance,
				Frozen:       wallet.Frozen,
				DiscountRate: rate,
				DiscountTier: tier,
			}
		}(),
		TopupURL: topupURL,
	}

	if productID != "" {
		sub, err := s.subs.GetActive(ctx, accountID, productID)
		if err != nil {
			return nil, fmt.Errorf("overview get subscription: %w", err)
		}
		if sub != nil {
			planCode := ""
			if plan, perr := s.plans.GetPlanByID(ctx, sub.PlanID); perr == nil && plan != nil {
				planCode = plan.Code
			}
			ov.Subscription = &SubscriptionSummary{
				ProductID: sub.ProductID,
				PlanCode:  planCode,
				Status:    sub.Status,
				ExpiresAt: sub.ExpiresAt,
				AutoRenew: sub.AutoRenew,
			}
		}
	}

	return ov, nil
}
