package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// NotificationConfig holds configuration for the notification module.
type NotificationConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ServiceURL string `yaml:"service_url"`
	APIKey     string `yaml:"api_key"`
}

// NotificationModule integrates the notification service with core account events.
type NotificationModule struct {
	cfg    NotificationConfig
	client *http.Client
}

// NewNotificationModule creates a notification module instance.
func NewNotificationModule(cfg NotificationConfig) *NotificationModule {
	return &NotificationModule{
		cfg: cfg,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// OnAccountCreated sends a welcome notification when a new account is created.
func (m *NotificationModule) OnAccountCreated(ctx context.Context, account *entity.Account) error {
	return m.send(ctx, notifyRequest{
		AccountID: account.ID,
		EventType: "account.welcome",
		Channels:  []string{"in_app"},
		Vars: map[string]string{
			"display_name": account.DisplayName,
		},
	})
}

// Register registers all notification module hooks into the module registry.
func (m *NotificationModule) Register(r *Registry) {
	r.OnAccountCreated(m.OnAccountCreated)
	slog.Info("module registered", "module", "notification", "service_url", m.cfg.ServiceURL)
}

type notifyRequest struct {
	AccountID int64             `json:"account_id"`
	EventType string            `json:"event_type"`
	Channels  []string          `json:"channels"`
	Vars      map[string]string `json:"vars"`
}

func (m *NotificationModule) send(ctx context.Context, req notifyRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("notification module: marshal: %w", err)
	}

	url := m.cfg.ServiceURL + "/internal/v1/notify"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("notification module: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)

	resp, err := m.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("notification module: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification module: service returned %d", resp.StatusCode)
	}
	return nil
}
