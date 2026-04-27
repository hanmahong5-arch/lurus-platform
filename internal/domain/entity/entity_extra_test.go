package entity_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── ValidateUsername ─────────────────────────────────────────────────────────

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid usernames
		{"valid alphanum", "alice", false},
		{"valid with underscore", "alice_bob", false},
		{"min length 3", "abc", false},
		{"max length 32", "abcdefghijklmnopqrstuvwxyz123456", false},
		{"all digits alphanumeric", "user123", false},
		{"uppercase", "Alice", false},
		{"mixed case underscore", "Alice_Bob_123", false},
		// Valid phone number (China mobile)
		{"valid china phone 13x", "13800138000", false},
		{"valid china phone 18x", "18912345678", false},
		{"valid china phone 19x", "19900001234", false},
		// Invalid cases
		{"empty string", "", true},
		{"too short 1 char", "ab", true},
		{"spaces in name", "alice bob", true},
		{"hyphen not allowed", "alice-bob", true},
		{"special chars", "alice@bob", true},
		{"too long 33 chars", "abcdefghijklmnopqrstuvwxyz1234567", true},
		// Invalid phone numbers
		{"phone starts with 0", "01234567890", true},
		{"phone starts with 2x", "22800138000", true},
		{"phone too short 10 digits", "1380013800", true},
		{"phone too long 12 digits", "138001380001", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := entity.ValidateUsername(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateUsername(%q) error = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

// ── IsPhoneNumber ────────────────────────────────────────────────────────────

func TestIsPhoneNumber(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"13800138000", true},
		{"18600001234", true},
		{"19900009999", true},
		{"15012345678", true},
		// Invalid
		{"12345678901", false}, // starts with 12
		{"08012345678", false}, // starts with 0
		{"1380013800", false},  // 10 digits
		{"138001380001", false}, // 12 digits
		{"", false},
		{"abcdefghijk", false},
		{"13800-38000", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := entity.IsPhoneNumber(tc.input)
			if got != tc.want {
				t.Errorf("IsPhoneNumber(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ── ServiceAPIKey ────────────────────────────────────────────────────────────

func TestServiceAPIKey_TableName(t *testing.T) {
	k := entity.ServiceAPIKey{}
	if got := k.TableName(); got != "identity.service_api_keys" {
		t.Errorf("TableName() = %q, want identity.service_api_keys", got)
	}
}

func TestAllScopes_ReturnsAllConstants(t *testing.T) {
	scopes := entity.AllScopes()
	expected := []string{
		entity.ScopeAccountRead,
		entity.ScopeAccountWrite,
		entity.ScopeWalletRead,
		entity.ScopeWalletDebit,
		entity.ScopeWalletCredit,
		entity.ScopeEntitlement,
		entity.ScopeCheckout,
	}
	if len(scopes) != len(expected) {
		t.Errorf("AllScopes() len = %d, want %d", len(scopes), len(expected))
	}
	inScopes := make(map[string]bool)
	for _, s := range scopes {
		inScopes[s] = true
	}
	for _, exp := range expected {
		if !inScopes[exp] {
			t.Errorf("AllScopes() missing %q", exp)
		}
	}
}

func TestServiceAPIKey_HasScope(t *testing.T) {
	tests := []struct {
		name   string
		scopes entity.StringList
		check  string
		want   bool
	}{
		{"has scope", entity.StringList{entity.ScopeAccountRead, entity.ScopeWalletRead}, entity.ScopeAccountRead, true},
		{"missing scope", entity.StringList{entity.ScopeAccountRead}, entity.ScopeWalletDebit, false},
		{"empty scopes", entity.StringList{}, entity.ScopeAccountRead, false},
		{"multiple scopes has last", entity.StringList{entity.ScopeWalletRead, entity.ScopeCheckout}, entity.ScopeCheckout, true},
		{"case sensitive", entity.StringList{"Account:Read"}, entity.ScopeAccountRead, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := &entity.ServiceAPIKey{Scopes: tc.scopes}
			got := k.HasScope(tc.check)
			if got != tc.want {
				t.Errorf("HasScope(%q) = %v, want %v", tc.check, got, tc.want)
			}
		})
	}
}

func TestServiceAPIKey_IsActive(t *testing.T) {
	tests := []struct {
		status int16
		want   bool
	}{
		{entity.ServiceKeyActive, true},
		{entity.ServiceKeySuspended, false},
		{entity.ServiceKeyRevoked, false},
		{0, false},
	}
	for _, tc := range tests {
		k := &entity.ServiceAPIKey{Status: tc.status}
		got := k.IsActive()
		if got != tc.want {
			t.Errorf("IsActive() for status %d = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ── ReconciliationIssue ──────────────────────────────────────────────────────

func TestReconciliationIssue_TableName(t *testing.T) {
	r := entity.ReconciliationIssue{}
	if got := r.TableName(); got != "billing.reconciliation_issues" {
		t.Errorf("TableName() = %q, want billing.reconciliation_issues", got)
	}
}

func TestReconciliationIssue_Constants(t *testing.T) {
	// Verify constant values are stable contract.
	if entity.ReconIssueMissingCredit != "missing_credit" {
		t.Errorf("ReconIssueMissingCredit = %q, want missing_credit", entity.ReconIssueMissingCredit)
	}
	if entity.ReconIssueOrphanPayment != "orphan_payment" {
		t.Errorf("ReconIssueOrphanPayment = %q, want orphan_payment", entity.ReconIssueOrphanPayment)
	}
	if entity.ReconIssueAmountMismatch != "amount_mismatch" {
		t.Errorf("ReconIssueAmountMismatch = %q, want amount_mismatch", entity.ReconIssueAmountMismatch)
	}
	if entity.ReconStatusOpen != "open" {
		t.Errorf("ReconStatusOpen = %q, want open", entity.ReconStatusOpen)
	}
	if entity.ReconStatusResolved != "resolved" {
		t.Errorf("ReconStatusResolved = %q, want resolved", entity.ReconStatusResolved)
	}
	if entity.ReconStatusIgnored != "ignored" {
		t.Errorf("ReconStatusIgnored = %q, want ignored", entity.ReconStatusIgnored)
	}
}

// ── UserPreference ───────────────────────────────────────────────────────────

func TestUserPreference_TableName(t *testing.T) {
	p := entity.UserPreference{}
	if got := p.TableName(); got != "identity.user_preferences" {
		t.Errorf("TableName() = %q, want identity.user_preferences", got)
	}
}

// ── WalletPreAuthorization ───────────────────────────────────────────────────

func TestWalletPreAuthorization_TableName(t *testing.T) {
	p := entity.WalletPreAuthorization{}
	if got := p.TableName(); got != "billing.wallet_pre_authorizations" {
		t.Errorf("TableName() = %q, want billing.wallet_pre_authorizations", got)
	}
}

func TestPreAuthStatus_Constants(t *testing.T) {
	if entity.PreAuthStatusActive != "active" {
		t.Errorf("PreAuthStatusActive = %q", entity.PreAuthStatusActive)
	}
	if entity.PreAuthStatusSettled != "settled" {
		t.Errorf("PreAuthStatusSettled = %q", entity.PreAuthStatusSettled)
	}
	if entity.PreAuthStatusReleased != "released" {
		t.Errorf("PreAuthStatusReleased = %q", entity.PreAuthStatusReleased)
	}
	if entity.PreAuthStatusExpired != "expired" {
		t.Errorf("PreAuthStatusExpired = %q", entity.PreAuthStatusExpired)
	}
}

// ── Wallet ───────────────────────────────────────────────────────────────────

func TestWallet_TableName(t *testing.T) {
	w := entity.Wallet{}
	if got := w.TableName(); got != "billing.wallets" {
		t.Errorf("TableName() = %q, want billing.wallets", got)
	}
}

// ── TxType constants ─────────────────────────────────────────────────────────

func TestTxTypeConstants(t *testing.T) {
	types := map[string]string{
		"topup":            entity.TxTypeTopup,
		"subscription":     entity.TxTypeSubscription,
		"product_purchase": entity.TxTypeProductPurchase,
		"refund":           entity.TxTypeRefund,
		"bonus":            entity.TxTypeBonus,
		"referral_reward":  entity.TxTypeReferralReward,
		"redemption":       entity.TxTypeRedemption,
		"checkin_reward":   entity.TxTypeCheckinReward,
		"preauth_settle":   entity.TxTypePreAuthSettle,
		"preauth_release":  entity.TxTypePreAuthRelease,
		"currency_exchange": entity.TxTypeCurrencyExchange,
	}
	for want, got := range types {
		if got != want {
			t.Errorf("TxType constant = %q, want %q", got, want)
		}
	}
}

// ── OrderStatus constants ────────────────────────────────────────────────────

func TestOrderStatusConstants(t *testing.T) {
	tests := []struct {
		constant string
		want     string
	}{
		{entity.OrderStatusPending, "pending"},
		{entity.OrderStatusPaid, "paid"},
		{entity.OrderStatusFailed, "failed"},
		{entity.OrderStatusCancelled, "cancelled"},
		{entity.OrderStatusExpired, "expired"},
		{entity.OrderStatusRefunded, "refunded"},
	}
	for _, tc := range tests {
		if tc.constant != tc.want {
			t.Errorf("OrderStatus = %q, want %q", tc.constant, tc.want)
		}
	}
}

// ── Subscription ─────────────────────────────────────────────────────────────

func TestSubscription_IsLive(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{entity.SubStatusActive, true},
		{entity.SubStatusGrace, true},
		{entity.SubStatusTrial, true},
		{entity.SubStatusPending, false},
		{entity.SubStatusExpired, false},
		{entity.SubStatusCancelled, false},
		{entity.SubStatusSuspended, false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			s := &entity.Subscription{Status: tc.status}
			got := s.IsLive()
			if got != tc.want {
				t.Errorf("IsLive() for status %q = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestSubscriptionStatus_Constants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"pending", entity.SubStatusPending, "pending"},
		{"trial", entity.SubStatusTrial, "trial"},
		{"active", entity.SubStatusActive, "active"},
		{"grace", entity.SubStatusGrace, "grace"},
		{"expired", entity.SubStatusExpired, "expired"},
		{"cancelled", entity.SubStatusCancelled, "cancelled"},
		{"suspended", entity.SubStatusSuspended, "suspended"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.constant != tc.want {
				t.Errorf("%s constant = %q, want %q", tc.name, tc.constant, tc.want)
			}
		})
	}
}
