package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/config"
)

// adminSettingStore is the minimal DB interface required by AdminConfigService.
type adminSettingStore interface {
	GetAll(ctx context.Context) ([]entity.AdminSetting, error)
	Set(ctx context.Context, key, value, updatedBy string) error
}

// AdminConfigService manages runtime-configurable admin settings.
// DB values take priority over env vars; an empty DB value falls back to the env var.
// The in-memory sync.Map cache is refreshed on every Set call.
type AdminConfigService struct {
	repo  adminSettingStore
	cache sync.Map // string -> entity.AdminSetting
}

// NewAdminConfigService creates the service. Call Load() before using GetEffective.
func NewAdminConfigService(repo adminSettingStore) *AdminConfigService {
	return &AdminConfigService{repo: repo}
}

// Load fetches all settings from DB and populates the in-memory cache.
// Must be called once at startup.
func (s *AdminConfigService) Load(ctx context.Context) error {
	settings, err := s.repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("admin config: load: %w", err)
	}
	for _, st := range settings {
		cp := st
		s.cache.Store(st.Key, cp)
	}
	slog.Info("admin config loaded", "count", len(settings))
	return nil
}

// GetEffective returns the effective value: DB value if non-empty, else envVal.
func (s *AdminConfigService) GetEffective(key, envVal string) string {
	if v, ok := s.cache.Load(key); ok {
		if st, ok := v.(entity.AdminSetting); ok && st.Value != "" {
			return st.Value
		}
	}
	return envVal
}

// Get returns the raw stored value for key (may be empty string if not set).
// Falls through to DB if the key is not in cache.
func (s *AdminConfigService) Get(ctx context.Context, key string) (string, error) {
	if v, ok := s.cache.Load(key); ok {
		if st, ok := v.(entity.AdminSetting); ok {
			return st.Value, nil
		}
	}
	// Cache miss — reload from DB
	settings, err := s.repo.GetAll(ctx)
	if err != nil {
		return "", fmt.Errorf("admin config: get: %w", err)
	}
	for _, st := range settings {
		if st.Key == key {
			cp := st
			s.cache.Store(key, cp)
			return st.Value, nil
		}
	}
	return "", nil
}

// Set updates a setting in DB and refreshes the in-memory cache entry.
func (s *AdminConfigService) Set(ctx context.Context, key, value, updatedBy string) error {
	if err := s.repo.Set(ctx, key, value, updatedBy); err != nil {
		return fmt.Errorf("admin config: set %q: %w", key, err)
	}
	// Refresh cache — preserve is_secret from existing entry
	st := entity.AdminSetting{
		Key:       key,
		Value:     value,
		UpdatedBy: updatedBy,
		UpdatedAt: time.Now(),
	}
	if v, ok := s.cache.Load(key); ok {
		if old, ok := v.(entity.AdminSetting); ok {
			st.IsSecret = old.IsSecret
		}
	}
	s.cache.Store(key, st)
	slog.Info("admin config updated", "key", key, "updated_by", updatedBy)
	return nil
}

// LoadAll returns all settings from DB (for admin API responses).
func (s *AdminConfigService) LoadAll(ctx context.Context) ([]entity.AdminSetting, error) {
	settings, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin config: load all: %w", err)
	}
	return settings, nil
}

// ApplyToConfig overlays non-empty DB values onto cfg.
// Empty DB values leave the existing env var defaults in place.
// Call this at startup after Load(), before initialising payment providers.
func (s *AdminConfigService) ApplyToConfig(cfg *config.Config) {
	if v := s.GetEffective("epay_partner_id", cfg.EpayPartnerID); v != "" {
		cfg.EpayPartnerID = v
	}
	if v := s.GetEffective("epay_key", cfg.EpayKey); v != "" {
		cfg.EpayKey = v
	}
	if v := s.GetEffective("epay_gateway_url", cfg.EpayGatewayURL); v != "" {
		cfg.EpayGatewayURL = v
	}
	if v := s.GetEffective("epay_notify_url", cfg.EpayNotifyURL); v != "" {
		cfg.EpayNotifyURL = v
	}
	if v := s.GetEffective("stripe_secret_key", cfg.StripeSecretKey); v != "" {
		cfg.StripeSecretKey = v
	}
	if v := s.GetEffective("stripe_webhook_secret", cfg.StripeWebhookSecret); v != "" {
		cfg.StripeWebhookSecret = v
	}
	if v := s.GetEffective("creem_api_key", cfg.CreemAPIKey); v != "" {
		cfg.CreemAPIKey = v
	}
	if v := s.GetEffective("creem_webhook_secret", cfg.CreemWebhookSecret); v != "" {
		cfg.CreemWebhookSecret = v
	}
}
