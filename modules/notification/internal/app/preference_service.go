package app

import (
	"context"
	"fmt"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

// PreferenceService manages per-account notification channel preferences.
type PreferenceService struct {
	repo *repo.PreferenceRepo
}

// NewPreferenceService creates a PreferenceService.
func NewPreferenceService(repo *repo.PreferenceRepo) *PreferenceService {
	return &PreferenceService{repo: repo}
}

// GetByAccount returns all channel preferences for an account.
// Missing channels default to enabled.
func (s *PreferenceService) GetByAccount(ctx context.Context, accountID int64) ([]PreferenceView, error) {
	prefs, err := s.repo.GetByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get preferences: %w", err)
	}

	// Build a map of existing preferences.
	prefMap := make(map[entity.Channel]bool, len(prefs))
	for _, p := range prefs {
		prefMap[p.Channel] = p.Enabled
	}

	// Return all known channels, defaulting to enabled if no record exists.
	allChannels := []entity.Channel{entity.ChannelInApp, entity.ChannelEmail, entity.ChannelFCM}
	views := make([]PreferenceView, 0, len(allChannels))
	for _, ch := range allChannels {
		enabled := true
		if v, exists := prefMap[ch]; exists {
			enabled = v
		}
		views = append(views, PreferenceView{
			Channel: ch,
			Enabled: enabled,
		})
	}
	return views, nil
}

// PreferenceView is the API response view for a single channel preference.
type PreferenceView struct {
	Channel entity.Channel `json:"channel"`
	Enabled bool           `json:"enabled"`
}

// PreferenceUpdate is a single channel preference update request.
type PreferenceUpdate struct {
	Channel string `json:"channel" binding:"required"`
	Enabled bool   `json:"enabled"`
}

// BatchUpdate applies a batch of preference updates for an account.
func (s *PreferenceService) BatchUpdate(ctx context.Context, accountID int64, updates []PreferenceUpdate) error {
	for _, u := range updates {
		ch := entity.Channel(u.Channel)
		if !isValidChannel(ch) {
			return fmt.Errorf("invalid channel: %s", u.Channel)
		}
		if err := s.repo.Upsert(ctx, accountID, ch, u.Enabled); err != nil {
			return fmt.Errorf("upsert preference for %s: %w", u.Channel, err)
		}
	}
	return nil
}

func isValidChannel(ch entity.Channel) bool {
	switch ch {
	case entity.ChannelInApp, entity.ChannelEmail, entity.ChannelFCM:
		return true
	default:
		return false
	}
}
