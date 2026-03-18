package app

import (
	"context"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/repo"
)

// DeviceService manages FCM device token registration.
type DeviceService struct {
	repo *repo.DeviceTokenRepo
}

// NewDeviceService creates a DeviceService.
func NewDeviceService(repo *repo.DeviceTokenRepo) *DeviceService {
	return &DeviceService{repo: repo}
}

// RegisterToken registers or re-activates a device token for an account.
func (s *DeviceService) RegisterToken(ctx context.Context, accountID int64, platform, token string) error {
	if err := s.repo.Upsert(ctx, accountID, platform, token); err != nil {
		return fmt.Errorf("register device token: %w", err)
	}
	return nil
}

// UnregisterToken deactivates a device token.
func (s *DeviceService) UnregisterToken(ctx context.Context, accountID int64, token string) error {
	if err := s.repo.Deactivate(ctx, accountID, token); err != nil {
		return fmt.Errorf("unregister device token: %w", err)
	}
	return nil
}

// GetActiveTokens returns all active device tokens for an account.
func (s *DeviceService) GetActiveTokens(ctx context.Context, accountID int64) ([]string, error) {
	return s.repo.GetActiveTokens(ctx, accountID)
}
