package app

import (
	"testing"
)

func TestComputeDiscount(t *testing.T) {
	tests := []struct {
		name         string
		balance      float64
		wantRate     float64
		wantTier     string
	}{
		{"below_silver",     0,      1.00, "none"},
		{"at_zero",          0,      1.00, "none"},
		{"just_below_silver", 99.99, 1.00, "none"},
		{"at_silver",        100,    0.95, "silver_holder"},
		{"mid_silver",       300,    0.95, "silver_holder"},
		{"just_below_gold",  499.99, 0.95, "silver_holder"},
		{"at_gold",          500,    0.90, "gold_holder"},
		{"mid_gold",         1000,   0.90, "gold_holder"},
		{"just_below_diamond", 1999.99, 0.90, "gold_holder"},
		{"at_diamond",       2000,   0.85, "diamond_holder"},
		{"above_diamond",    10000,  0.85, "diamond_holder"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, tier := computeDiscount(tt.balance)
			if rate != tt.wantRate {
				t.Errorf("computeDiscount(%.2f) rate = %.2f, want %.2f", tt.balance, rate, tt.wantRate)
			}
			if tier != tt.wantTier {
				t.Errorf("computeDiscount(%.2f) tier = %q, want %q", tt.balance, tier, tt.wantTier)
			}
		})
	}
}
