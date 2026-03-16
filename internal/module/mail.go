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

// MailConfig holds configuration for the mail module.
type MailConfig struct {
	Enabled          bool   `yaml:"enabled"`
	StalwartAdminURL string `yaml:"stalwart_admin_url"`
	StalwartUser     string `yaml:"stalwart_admin_user"`
	StalwartPassword string `yaml:"stalwart_admin_password"`
	DefaultQuotaMB   int    `yaml:"default_quota_mb"`
}

// MailModule integrates Stalwart mail server with the core account lifecycle.
type MailModule struct {
	cfg    MailConfig
	client *http.Client
}

// NewMailModule creates a mail module instance.
func NewMailModule(cfg MailConfig) *MailModule {
	if cfg.DefaultQuotaMB <= 0 {
		cfg.DefaultQuotaMB = 1024
	}
	return &MailModule{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// OnAccountCreated provisions a mailbox in Stalwart when a new account is created.
func (m *MailModule) OnAccountCreated(ctx context.Context, account *entity.Account) error {
	if account.Email == "" {
		return nil
	}
	slog.Info("mail module: provisioning mailbox", "email", account.Email)
	return m.stalwartRequest(ctx, http.MethodPost, "/api/account", map[string]any{
		"name":     account.Email,
		"type":     "individual",
		"quota":    m.cfg.DefaultQuotaMB * 1024 * 1024,
		"emails":   []string{account.Email},
		"password": "", // Stalwart uses OIDC auth, no local password needed
	})
}

// OnAccountDeleted deprovisions the mailbox when an account is deleted.
func (m *MailModule) OnAccountDeleted(ctx context.Context, account *entity.Account) error {
	if account.Email == "" {
		return nil
	}
	slog.Info("mail module: deprovisioning mailbox", "email", account.Email)
	return m.stalwartRequest(ctx, http.MethodDelete, "/api/account/"+account.Email, nil)
}

// OnPlanChanged adjusts mailbox quota based on the new plan's mail_quota_mb feature.
func (m *MailModule) OnPlanChanged(ctx context.Context, account *entity.Account, plan *entity.ProductPlan) error {
	if account.Email == "" {
		return nil
	}
	quotaMB := m.cfg.DefaultQuotaMB
	if len(plan.Features) > 0 {
		var feats map[string]any
		if err := json.Unmarshal(plan.Features, &feats); err == nil {
			if v, ok := feats["mail_quota_mb"]; ok {
				if q, qok := v.(float64); qok && q > 0 {
					quotaMB = int(q)
				}
			}
		}
	}
	slog.Info("mail module: updating quota", "email", account.Email, "quota_mb", quotaMB)
	return m.stalwartRequest(ctx, http.MethodPatch, "/api/account/"+account.Email, map[string]any{
		"quota": quotaMB * 1024 * 1024,
	})
}

// Register registers all mail module hooks into the module registry.
func (m *MailModule) Register(r *Registry) {
	r.OnAccountCreated(m.OnAccountCreated)
	r.OnAccountDeleted(m.OnAccountDeleted)
	r.OnPlanChanged(m.OnPlanChanged)
	slog.Info("module registered", "module", "mail", "stalwart_url", m.cfg.StalwartAdminURL)
}

func (m *MailModule) stalwartRequest(ctx context.Context, method, path string, body any) error {
	var bodyReader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("mail module: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	url := m.cfg.StalwartAdminURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("mail module: create request: %w", err)
	}
	req.SetBasicAuth(m.cfg.StalwartUser, m.cfg.StalwartPassword)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail module: stalwart request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("mail module: stalwart returned %d for %s %s", resp.StatusCode, method, path)
	}
	return nil
}
