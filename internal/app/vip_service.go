package app

import (
	"context"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// VIPService calculates and applies VIP levels.
// Level = MAX(yearly_sub_grant, spend_grant).
type VIPService struct {
	vip     vipStore
	wallets walletStore
}

func NewVIPService(vip vipStore, wallets walletStore) *VIPService {
	return &VIPService{vip: vip, wallets: wallets}
}

// RecalculateFromWallet recomputes spend_grant based on lifetime_topup and updates the VIP row.
func (s *VIPService) RecalculateFromWallet(ctx context.Context, accountID int64) error {
	w, err := s.wallets.GetByAccountID(ctx, accountID)
	if err != nil || w == nil {
		return nil // wallet not yet created, skip
	}
	configs, err := s.vip.ListConfigs(ctx)
	if err != nil {
		return fmt.Errorf("list vip configs: %w", err)
	}

	var spendGrant int16
	for _, c := range configs {
		if w.LifetimeTopup >= c.MinSpendCNY {
			spendGrant = c.Level
		}
	}

	v, err := s.vip.GetOrCreate(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get vip: %w", err)
	}
	v.SpendGrant = spendGrant
	v.Level = maxInt16(v.YearlySubGrant, v.SpendGrant)
	v.LevelName = s.levelName(configs, v.Level)
	return s.vip.Update(ctx, v)
}

// GrantYearlySub sets the yearly_sub_grant level (called when a yearly subscription activates).
func (s *VIPService) GrantYearlySub(ctx context.Context, accountID int64, grantLevel int16) error {
	configs, err := s.vip.ListConfigs(ctx)
	if err != nil {
		return err
	}
	v, err := s.vip.GetOrCreate(ctx, accountID)
	if err != nil {
		return err
	}
	v.YearlySubGrant = grantLevel
	v.Level = maxInt16(v.YearlySubGrant, v.SpendGrant)
	v.LevelName = s.levelName(configs, v.Level)
	return s.vip.Update(ctx, v)
}

// AdminSet forcibly sets an account's VIP level.
func (s *VIPService) AdminSet(ctx context.Context, accountID int64, level int16) error {
	configs, err := s.vip.ListConfigs(ctx)
	if err != nil {
		return err
	}
	v, err := s.vip.GetOrCreate(ctx, accountID)
	if err != nil {
		return err
	}
	v.Level = level
	v.LevelName = s.levelName(configs, level)
	return s.vip.Update(ctx, v)
}

func (s *VIPService) Get(ctx context.Context, accountID int64) (*entity.AccountVIP, error) {
	v, err := s.vip.GetOrCreate(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *VIPService) levelName(configs []entity.VIPLevelConfig, level int16) string {
	for _, c := range configs {
		if c.Level == level {
			return c.Name
		}
	}
	return "Standard"
}

func maxInt16(a, b int16) int16 {
	if a > b {
		return a
	}
	return b
}
