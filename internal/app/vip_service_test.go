package app

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func TestMaxInt16(t *testing.T) {
	tests := []struct {
		a, b int16
		want int16
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0, 2, 2},
		{3, 3, 3},
		{4, 2, 4},
	}
	for _, tc := range tests {
		got := maxInt16(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("maxInt16(%d,%d)=%d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVIPServiceLevelName(t *testing.T) {
	svc := &VIPService{}
	configs := []entity.VIPLevelConfig{
		{Level: 0, Name: "Standard"},
		{Level: 1, Name: "Silver"},
		{Level: 2, Name: "Gold"},
	}
	tests := []struct {
		level int16
		want  string
	}{
		{0, "Standard"},
		{1, "Silver"},
		{2, "Gold"},
		{9, "Standard"}, // unknown level falls back to "Standard"
	}
	for _, tc := range tests {
		got := svc.levelName(configs, tc.level)
		if got != tc.want {
			t.Errorf("levelName(level=%d)=%q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestVIPSpendGrant_Logic(t *testing.T) {
	configs := []entity.VIPLevelConfig{
		{Level: 0, MinSpendCNY: 0},
		{Level: 1, MinSpendCNY: 500},
		{Level: 2, MinSpendCNY: 2000},
		{Level: 3, MinSpendCNY: 10000},
	}
	computeGrant := func(lifetimeTopup float64) int16 {
		var grant int16
		for _, c := range configs {
			if lifetimeTopup >= c.MinSpendCNY {
				grant = c.Level
			}
		}
		return grant
	}
	tests := []struct {
		topup float64
		want  int16
	}{
		{0, 0},
		{499, 0},
		{500, 1},
		{1999, 1},
		{2000, 2},
		{9999, 2},
		{10000, 3},
		{999999, 3},
	}
	for _, tc := range tests {
		got := computeGrant(tc.topup)
		if got != tc.want {
			t.Errorf("topup=%.0f → grant=%d, want %d", tc.topup, got, tc.want)
		}
	}
}

// ── VIPService.RecalculateFromWallet (with mock) ─────────────────────────────

func defaultVIPConfigs() []entity.VIPLevelConfig {
	return []entity.VIPLevelConfig{
		{Level: 0, Name: "Standard", MinSpendCNY: 0},
		{Level: 1, Name: "Silver", MinSpendCNY: 500},
		{Level: 2, Name: "Gold", MinSpendCNY: 2000},
		{Level: 3, Name: "Platinum", MinSpendCNY: 10000},
	}
}

func TestVIPService_RecalculateFromWallet(t *testing.T) {
	tests := []struct {
		name          string
		lifetimeTopup float64
		wantLevel     int16
		wantName      string
	}{
		{"no spend → standard", 0, 0, "Standard"},
		{"just below silver", 499, 0, "Standard"},
		{"exactly silver", 500, 1, "Silver"},
		{"gold threshold", 2000, 2, "Gold"},
		{"platinum", 10000, 3, "Platinum"},
		{"beyond platinum", 50000, 3, "Platinum"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vipStore := newMockVIPStore(defaultVIPConfigs())
			wStore := newMockWalletStore()
			ctx := context.Background()

			// Pre-seed wallet with the desired lifetime_topup
			_, _ = wStore.GetOrCreate(ctx, 1)
			wStore.wallets[1].LifetimeTopup = tc.lifetimeTopup

			svc := NewVIPService(vipStore, wStore)
			if err := svc.RecalculateFromWallet(ctx, 1); err != nil {
				t.Fatalf("RecalculateFromWallet error: %v", err)
			}

			v, _ := svc.Get(ctx, 1)
			if v.Level != tc.wantLevel {
				t.Errorf("Level=%d, want %d", v.Level, tc.wantLevel)
			}
			if v.LevelName != tc.wantName {
				t.Errorf("LevelName=%q, want %q", v.LevelName, tc.wantName)
			}
			if v.SpendGrant != tc.wantLevel {
				t.Errorf("SpendGrant=%d, want %d", v.SpendGrant, tc.wantLevel)
			}
		})
	}
}

func TestVIPService_GrantYearlySub(t *testing.T) {
	vipStore := newMockVIPStore(defaultVIPConfigs())
	wStore := newMockWalletStore()
	svc := NewVIPService(vipStore, wStore)
	ctx := context.Background()

	if err := svc.GrantYearlySub(ctx, 1, 2); err != nil {
		t.Fatalf("GrantYearlySub error: %v", err)
	}
	v, _ := svc.Get(ctx, 1)
	if v.YearlySubGrant != 2 {
		t.Errorf("YearlySubGrant=%d, want 2", v.YearlySubGrant)
	}
	// SpendGrant = 0, YearlySubGrant = 2 → Level = 2
	if v.Level != 2 {
		t.Errorf("Level=%d, want 2", v.Level)
	}
}

func TestVIPService_LevelIsMaxOfBoth(t *testing.T) {
	// If spend_grant=1 and yearly_sub_grant=3, level must be 3
	vipStore := newMockVIPStore(defaultVIPConfigs())
	wStore := newMockWalletStore()
	ctx := context.Background()

	// seed wallet with spend that would give grant=1 (500 CNY)
	_, _ = wStore.GetOrCreate(ctx, 42)
	wStore.wallets[42].LifetimeTopup = 500

	svc := NewVIPService(vipStore, wStore)
	_ = svc.GrantYearlySub(ctx, 42, 3)       // yearly → level 3
	_ = svc.RecalculateFromWallet(ctx, 42)    // spend → level 1

	v, _ := svc.Get(ctx, 42)
	if v.Level != 3 {
		t.Errorf("Level=%d, want 3 (MAX of yearly=3, spend=1)", v.Level)
	}
}
