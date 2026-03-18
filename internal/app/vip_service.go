package app

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
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
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "vip.recalculate")
	defer span.End()
	span.SetAttributes(attribute.Int64("account.id", accountID))

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
	oldLevel := v.Level
	v.SpendGrant = spendGrant
	v.Level = maxInt16(v.YearlySubGrant, v.SpendGrant)
	v.LevelName = s.levelName(configs, v.Level)
	if err := s.vip.Update(ctx, v); err != nil {
		slog.Error("vip/recalculate: update failed", "account_id", accountID, "err", err)
		return err
	}
	if v.Level != oldLevel {
		slog.Info("vip/tier-changed", "account_id", accountID, "from_level", oldLevel, "to_level", v.Level, "level_name", v.LevelName, "lifetime_topup", w.LifetimeTopup)
	}
	return nil
}

// GrantYearlySub sets the yearly_sub_grant level (called when a yearly subscription activates).
func (s *VIPService) GrantYearlySub(ctx context.Context, accountID int64, grantLevel int16) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "vip.grant_yearly_sub")
	defer span.End()
	span.SetAttributes(attribute.Int64("account.id", accountID))

	configs, err := s.vip.ListConfigs(ctx)
	if err != nil {
		return err
	}
	v, err := s.vip.GetOrCreate(ctx, accountID)
	if err != nil {
		return err
	}
	oldLevel := v.Level
	v.YearlySubGrant = grantLevel
	v.Level = maxInt16(v.YearlySubGrant, v.SpendGrant)
	v.LevelName = s.levelName(configs, v.Level)
	if err := s.vip.Update(ctx, v); err != nil {
		slog.Error("vip/grant-yearly-sub: update failed", "account_id", accountID, "err", err)
		return err
	}
	slog.Info("vip/grant-yearly-sub", "account_id", accountID, "grant_level", grantLevel, "from_level", oldLevel, "to_level", v.Level, "level_name", v.LevelName)
	return nil
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
	oldLevel := v.Level
	v.Level = level
	v.LevelName = s.levelName(configs, level)
	if err := s.vip.Update(ctx, v); err != nil {
		slog.Error("vip/admin-set: update failed", "account_id", accountID, "err", err)
		return err
	}
	slog.Info("vip/admin-set", "account_id", accountID, "from_level", oldLevel, "to_level", level, "level_name", v.LevelName)
	return nil
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
