package entity_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestWallet_ZeroValue(t *testing.T) {
	var w entity.Wallet
	if w.Balance != 0 {
		t.Errorf("zero Wallet.Balance = %f, want 0", w.Balance)
	}
	if w.Frozen != 0 {
		t.Errorf("zero Wallet.Frozen = %f, want 0", w.Frozen)
	}
	if w.LifetimeTopup != 0 {
		t.Errorf("zero Wallet.LifetimeTopup = %f, want 0", w.LifetimeTopup)
	}
	if w.LifetimeSpend != 0 {
		t.Errorf("zero Wallet.LifetimeSpend = %f, want 0", w.LifetimeSpend)
	}
}

func TestTxTypeConstants_Unique(t *testing.T) {
	types := []string{
		entity.TxTypeTopup,
		entity.TxTypeSubscription,
		entity.TxTypeProductPurchase,
		entity.TxTypeRefund,
		entity.TxTypeBonus,
		entity.TxTypeReferralReward,
		entity.TxTypeRedemption,
		entity.TxTypeCheckinReward,
		entity.TxTypePreAuthSettle,
		entity.TxTypePreAuthRelease,
		entity.TxTypeCurrencyExchange,
	}

	seen := make(map[string]bool)
	for _, typ := range types {
		if typ == "" {
			t.Error("TxType constant is empty string")
		}
		if seen[typ] {
			t.Errorf("duplicate TxType constant: %q", typ)
		}
		seen[typ] = true
	}
}

func TestOrderStatusConstants_Unique(t *testing.T) {
	statuses := []string{
		entity.OrderStatusPending,
		entity.OrderStatusPaid,
		entity.OrderStatusFailed,
		entity.OrderStatusCancelled,
		entity.OrderStatusExpired,
		entity.OrderStatusRefunded,
	}

	seen := make(map[string]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("OrderStatus constant is empty string")
		}
		if seen[s] {
			t.Errorf("duplicate OrderStatus constant: %q", s)
		}
		seen[s] = true
	}
}

func TestPreAuthStatusConstants_Unique(t *testing.T) {
	statuses := []string{
		entity.PreAuthStatusActive,
		entity.PreAuthStatusSettled,
		entity.PreAuthStatusReleased,
		entity.PreAuthStatusExpired,
	}

	seen := make(map[string]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("PreAuthStatus constant is empty string")
		}
		if seen[s] {
			t.Errorf("duplicate PreAuthStatus constant: %q", s)
		}
		seen[s] = true
	}
}

func TestWalletPreAuthorization_ZeroValue(t *testing.T) {
	var pa entity.WalletPreAuthorization
	if pa.Amount != 0 {
		t.Errorf("zero PreAuth.Amount = %f, want 0", pa.Amount)
	}
	if pa.ActualAmount != nil {
		t.Error("zero PreAuth.ActualAmount should be nil")
	}
	if pa.SettledAt != nil {
		t.Error("zero PreAuth.SettledAt should be nil")
	}
}

func TestRedemptionCode_ZeroValue(t *testing.T) {
	var rc entity.RedemptionCode
	if rc.MaxUses != 0 {
		t.Errorf("zero RedemptionCode.MaxUses = %d, want 0 (Go default)", rc.MaxUses)
	}
	if rc.UsedCount != 0 {
		t.Errorf("zero RedemptionCode.UsedCount = %d, want 0", rc.UsedCount)
	}
}
