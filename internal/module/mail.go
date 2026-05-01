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
	MailDomain       string `yaml:"mail_domain"` // e.g. "lurus.cn" — each user gets username@domain
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
	if cfg.MailDomain == "" {
		cfg.MailDomain = "lurus.cn"
	}
	return &MailModule{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// lurusEmail derives the platform mailbox address from the account's username.
// Every Lurus user gets username@lurus.cn (or configured domain).
// Returns "" if the account has no usable username.
func (m *MailModule) lurusEmail(account *entity.Account) string {
	username := account.Username
	if username == "" {
		return ""
	}
	// Phone-number usernames are not valid email local parts.
	if entity.IsPhoneNumber(username) {
		return ""
	}
	return username + "@" + m.cfg.MailDomain
}

// OnAccountCreated provisions a mailbox in Stalwart when a new account is created.
// Each user gets a username@lurus.cn mailbox automatically.
func (m *MailModule) OnAccountCreated(ctx context.Context, account *entity.Account) error {
	mailAddr := m.lurusEmail(account)
	if mailAddr == "" {
		return nil
	}
	slog.Info("mail module: provisioning mailbox", "email", mailAddr, "account_id", account.ID)

	// Collect aliases: the platform mailbox + the user's personal email (if different).
	emails := []string{mailAddr}
	if account.Email != "" && account.Email != mailAddr {
		emails = append(emails, account.Email)
	}

	return m.stalwartRequest(ctx, http.MethodPost, "/api/account", map[string]any{
		"name":     mailAddr,
		"type":     "individual",
		"quota":    m.cfg.DefaultQuotaMB * 1024 * 1024,
		"emails":   emails,
		"password": "", // Stalwart uses OIDC auth, no local password needed
	})
}

// OnAccountDeleted deprovisions the mailbox when an account is deleted.
func (m *MailModule) OnAccountDeleted(ctx context.Context, account *entity.Account) error {
	mailAddr := m.lurusEmail(account)
	if mailAddr == "" {
		return nil
	}
	slog.Info("mail module: deprovisioning mailbox", "email", mailAddr, "account_id", account.ID)
	return m.stalwartRequest(ctx, http.MethodDelete, "/api/account/"+mailAddr, nil)
}

// OnPlanChanged adjusts mailbox quota based on the new plan's mail_quota_mb feature.
func (m *MailModule) OnPlanChanged(ctx context.Context, account *entity.Account, plan *entity.ProductPlan) error {
	mailAddr := m.lurusEmail(account)
	if mailAddr == "" {
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
	slog.Info("mail module: updating quota", "email", mailAddr, "quota_mb", quotaMB)
	return m.stalwartRequest(ctx, http.MethodPatch, "/api/account/"+mailAddr, map[string]any{
		"quota": quotaMB * 1024 * 1024,
	})
}

// Register registers all mail module hooks into the module registry.
// Hook name "mail" is the DLQ key — stable across deploys.
func (m *MailModule) Register(r *Registry) {
	r.OnAccountCreated("mail", m.OnAccountCreated)
	r.OnAccountDeleted("mail", m.OnAccountDeleted)
	r.OnPlanChanged("mail", m.OnPlanChanged)
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
