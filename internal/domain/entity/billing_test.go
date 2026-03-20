package entity_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// ── Invoice ─────────────────────────────────────────────────────────────────

func TestInvoiceStatusConstants(t *testing.T) {
	if entity.InvoiceStatusDraft != "draft" {
		t.Errorf("InvoiceStatusDraft = %q, want 'draft'", entity.InvoiceStatusDraft)
	}
	if entity.InvoiceStatusIssued != "issued" {
		t.Errorf("InvoiceStatusIssued = %q, want 'issued'", entity.InvoiceStatusIssued)
	}
	if entity.InvoiceStatusDraft == entity.InvoiceStatusIssued {
		t.Error("InvoiceStatusDraft and InvoiceStatusIssued must be different")
	}
}

func TestInvoice_ZeroValue(t *testing.T) {
	var inv entity.Invoice
	if inv.TotalCNY != 0 {
		t.Errorf("zero Invoice.TotalCNY = %f, want 0", inv.TotalCNY)
	}
	if inv.SubtotalCNY != 0 {
		t.Errorf("zero Invoice.SubtotalCNY = %f, want 0", inv.SubtotalCNY)
	}
	if inv.LineItems != nil {
		t.Error("zero Invoice.LineItems should be nil")
	}
}

// ── Refund ───────────────────────────────────────────────────────────────────

func TestRefundStatusConstants_Unique(t *testing.T) {
	statuses := []entity.RefundStatus{
		entity.RefundStatusPending,
		entity.RefundStatusApproved,
		entity.RefundStatusRejected,
		entity.RefundStatusCompleted,
	}

	seen := make(map[entity.RefundStatus]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("RefundStatus constant is empty string")
		}
		if seen[s] {
			t.Errorf("duplicate RefundStatus constant: %q", s)
		}
		seen[s] = true
	}
}

func TestRefundStatusConstants_Values(t *testing.T) {
	tests := []struct {
		name string
		got  entity.RefundStatus
		want string
	}{
		{"Pending", entity.RefundStatusPending, "pending"},
		{"Approved", entity.RefundStatusApproved, "approved"},
		{"Rejected", entity.RefundStatusRejected, "rejected"},
		{"Completed", entity.RefundStatusCompleted, "completed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("RefundStatus%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestRefund_ZeroValue(t *testing.T) {
	var r entity.Refund
	if r.AmountCNY != 0 {
		t.Errorf("zero Refund.AmountCNY = %f, want 0", r.AmountCNY)
	}
	if r.ReviewedAt != nil {
		t.Error("zero Refund.ReviewedAt should be nil")
	}
	if r.CompletedAt != nil {
		t.Error("zero Refund.CompletedAt should be nil")
	}
}

// ── Product ──────────────────────────────────────────────────────────────────

func TestBillingCycleConstants_Unique(t *testing.T) {
	cycles := []string{
		entity.BillingCycleForever,
		entity.BillingCycleWeekly,
		entity.BillingCycleMonthly,
		entity.BillingCycleQuarterly,
		entity.BillingCycleYearly,
		entity.BillingCycleOneTime,
	}
	seen := make(map[string]bool)
	for _, c := range cycles {
		if c == "" {
			t.Error("BillingCycle constant is empty string")
		}
		if seen[c] {
			t.Errorf("duplicate BillingCycle constant: %q", c)
		}
		seen[c] = true
	}
}

func TestBillingModelConstants_Unique(t *testing.T) {
	models := []string{
		entity.BillingModelFree,
		entity.BillingModelQuota,
		entity.BillingModelSubscription,
		entity.BillingModelHybrid,
		entity.BillingModelOneTime,
		entity.BillingModelSeat,
		entity.BillingModelUsage,
	}
	seen := make(map[string]bool)
	for _, m := range models {
		if m == "" {
			t.Error("BillingModel constant is empty string")
		}
		if seen[m] {
			t.Errorf("duplicate BillingModel constant: %q", m)
		}
		seen[m] = true
	}
}

// ── Referral ─────────────────────────────────────────────────────────────────

func TestReferralEventConstants_Unique(t *testing.T) {
	events := []string{
		entity.ReferralEventSignup,
		entity.ReferralEventFirstTopup,
		entity.ReferralEventFirstSubscription,
		entity.ReferralEventRenewal,
	}
	seen := make(map[string]bool)
	for _, e := range events {
		if e == "" {
			t.Error("ReferralEvent constant is empty string")
		}
		if seen[e] {
			t.Errorf("duplicate ReferralEvent constant: %q", e)
		}
		seen[e] = true
	}
}

// ── VIP ──────────────────────────────────────────────────────────────────────

func TestAccountVIP_ZeroValue(t *testing.T) {
	var v entity.AccountVIP
	if v.Level != 0 {
		t.Errorf("zero AccountVIP.Level = %d, want 0", v.Level)
	}
	if v.Points != 0 {
		t.Errorf("zero AccountVIP.Points = %d, want 0", v.Points)
	}
	if v.LevelExpiresAt != nil {
		t.Error("zero AccountVIP.LevelExpiresAt should be nil")
	}
}

// ── Organization ─────────────────────────────────────────────────────────────

func TestOrgWallet_ZeroValue(t *testing.T) {
	var ow entity.OrgWallet
	if ow.Balance != 0 {
		t.Errorf("zero OrgWallet.Balance = %f, want 0", ow.Balance)
	}
	if ow.Frozen != 0 {
		t.Errorf("zero OrgWallet.Frozen = %f, want 0", ow.Frozen)
	}
}

// ── Checkin ──────────────────────────────────────────────────────────────────

func TestCheckin_ZeroValue(t *testing.T) {
	var c entity.Checkin
	if c.RewardValue != 0 {
		t.Errorf("zero Checkin.RewardValue = %f, want 0", c.RewardValue)
	}
}
