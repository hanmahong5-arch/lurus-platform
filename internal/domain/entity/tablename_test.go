package entity_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestTableNames(t *testing.T) {
	tests := []struct {
		name  string
		model interface{ TableName() string }
		want  string
	}{
		{"OAuthBinding", entity.OAuthBinding{}, "identity.account_oauth_bindings"},
		{"Wallet", entity.Wallet{}, "billing.wallets"},
		{"WalletTransaction", entity.WalletTransaction{}, "billing.wallet_transactions"},
		{"PaymentOrder", entity.PaymentOrder{}, "billing.payment_orders"},
		{"RedemptionCode", entity.RedemptionCode{}, "billing.redemption_codes"},
		{"Product", entity.Product{}, "identity.products"},
		{"ProductPlan", entity.ProductPlan{}, "identity.product_plans"},
		{"AccountVIP", entity.AccountVIP{}, "identity.account_vip"},
		{"VIPLevelConfig", entity.VIPLevelConfig{}, "identity.vip_level_configs"},
		{"Invoice", entity.Invoice{}, "billing.invoices"},
		{"Refund", entity.Refund{}, "billing.refunds"},
		{"ReferralRewardEvent", entity.ReferralRewardEvent{}, "billing.referral_reward_events"},
		{"Organization", entity.Organization{}, "identity.organizations"},
		{"OrgMember", entity.OrgMember{}, "identity.org_members"},
		{"OrgAPIKey", entity.OrgAPIKey{}, "identity.org_api_keys"},
		{"OrgWallet", entity.OrgWallet{}, "billing.org_wallets"},
		{"AdminSetting", entity.AdminSetting{}, "admin.settings"},
		{"Checkin", entity.Checkin{}, "identity.checkins"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.model.TableName(); got != tc.want {
				t.Errorf("%s.TableName() = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
