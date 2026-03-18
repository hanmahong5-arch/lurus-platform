package grpc

import (
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestAccountToProto(t *testing.T) {
	now := time.Now().UTC()
	a := &entity.Account{
		ID:          42,
		LurusID:     "LU0000042",
		ZitadelSub:  "sub-123",
		DisplayName: "Alice",
		AvatarURL:   "https://example.com/avatar.png",
		Email:       "alice@lurus.cn",
		Status:      1,
		Locale:      "zh-CN",
		AffCode:     "abc123",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	pb := accountToProto(a)

	if pb.Id != 42 {
		t.Errorf("Id = %d, want 42", pb.Id)
	}
	if pb.LurusId != "LU0000042" {
		t.Errorf("LurusId = %q, want %q", pb.LurusId, "LU0000042")
	}
	if pb.ZitadelSub != "sub-123" {
		t.Errorf("ZitadelSub = %q, want %q", pb.ZitadelSub, "sub-123")
	}
	if pb.Email != "alice@lurus.cn" {
		t.Errorf("Email = %q, want %q", pb.Email, "alice@lurus.cn")
	}
	if pb.Status != 1 {
		t.Errorf("Status = %d, want 1", pb.Status)
	}
	if pb.CreatedAt == nil {
		t.Error("CreatedAt should not be nil")
	}
}

func TestOverviewToProto(t *testing.T) {
	expires := time.Now().Add(30 * 24 * time.Hour)
	ov := &app.AccountOverview{
		Account: app.AccountSummary{
			ID:          1,
			LurusID:     "LU0000001",
			DisplayName: "Bob",
		},
		VIP: app.VIPSummary{
			Level:     2,
			LevelName: "Gold",
			LevelEN:   "Gold",
			Points:    500,
		},
		Wallet: app.WalletSummary{
			Balance:      100.50,
			DiscountRate: 0.95,
			DiscountTier: "silver_holder",
		},
		Subscription: &app.SubscriptionSummary{
			ProductID: "lucrum",
			PlanCode:  "pro",
			Status:    "active",
			ExpiresAt: &expires,
			AutoRenew: true,
		},
		TopupURL: "https://identity.lurus.cn/wallet/topup",
	}

	pb := overviewToProto(ov)

	if pb.Account.Id != 1 {
		t.Errorf("Account.Id = %d, want 1", pb.Account.Id)
	}
	if pb.Vip.Level != 2 {
		t.Errorf("VIP.Level = %d, want 2", pb.Vip.Level)
	}
	if pb.Wallet.Balance != 100.50 {
		t.Errorf("Wallet.Balance = %f, want 100.50", pb.Wallet.Balance)
	}
	if pb.Subscription == nil {
		t.Fatal("Subscription should not be nil")
	}
	if pb.Subscription.PlanCode != "pro" {
		t.Errorf("Subscription.PlanCode = %q, want %q", pb.Subscription.PlanCode, "pro")
	}
	if !pb.Subscription.AutoRenew {
		t.Error("Subscription.AutoRenew should be true")
	}
}

func TestOverviewToProto_NoSubscription(t *testing.T) {
	ov := &app.AccountOverview{
		Account: app.AccountSummary{ID: 1},
		VIP:     app.VIPSummary{},
		Wallet:  app.WalletSummary{},
	}

	pb := overviewToProto(ov)
	if pb.Subscription != nil {
		t.Error("Subscription should be nil when no subscription")
	}
}
