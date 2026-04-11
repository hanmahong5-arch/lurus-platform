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

// OnAccountCreated sends a welcome notification (in-app + email) when a new account is created.
func (m *NotificationModule) OnAccountCreated(ctx context.Context, account *entity.Account) error {
	return m.send(ctx, notifyRequest{
		AccountID: account.ID,
		EventType: "identity.account.created",
		EventID:   fmt.Sprintf("welcome_%d", account.ID),
		Channels:  []string{"in_app", "email"},
		Vars: map[string]string{
			"display_name": account.DisplayName,
			"lurus_id":     account.LurusID,
		},
		EmailAddr: account.Email,
	})
}

// OnCheckin sends an in-app notification on daily check-in, with milestone alerts.
func (m *NotificationModule) OnCheckin(ctx context.Context, accountID int64, streak int) error {
	// Regular check-in notification.
	req := notifyRequest{
		AccountID: accountID,
		EventType: "identity.checkin.success",
		EventID:   fmt.Sprintf("checkin_%d_%s", accountID, time.Now().Format("2006-01-02")),
		Channels:  []string{"in_app"},
		Vars: map[string]string{
			"streak": fmt.Sprintf("%d", streak),
		},
	}

	if err := m.send(ctx, req); err != nil {
		return err
	}

	// Milestone notifications: 7, 30, 100 day streaks.
	switch streak {
	case 7, 30, 100:
		return m.send(ctx, notifyRequest{
			AccountID: accountID,
			EventType: "identity.checkin.milestone",
			EventID:   fmt.Sprintf("checkin_milestone_%d_%d", accountID, streak),
			Channels:  []string{"in_app"},
			Vars: map[string]string{
				"streak":    fmt.Sprintf("%d", streak),
				"milestone": fmt.Sprintf("%d", streak),
			},
		})
	}
	return nil
}

// OnReferralSignup notifies the referrer when their invited user signs up.
func (m *NotificationModule) OnReferralSignup(ctx context.Context, referrerAccountID int64, referredName string) error {
	return m.send(ctx, notifyRequest{
		AccountID: referrerAccountID,
		EventType: "identity.referral.signup",
		Channels:  []string{"in_app"},
		Vars: map[string]string{
			"referred_name": referredName,
		},
	})
}

// OnReconciliationIssue sends a critical alert when a payment reconciliation issue is found.
func (m *NotificationModule) OnReconciliationIssue(ctx context.Context, issue *entity.ReconciliationIssue) error {
	// Only alert on critical issues (missing_credit, missed_webhook).
	if issue.Severity != "critical" {
		return nil
	}
	channels := []string{"in_app"}
	// Also send email if the issue has an account (for admin notification, channel "email"
	// is sent to the platform admin, not the user account).
	vars := map[string]string{
		"issue_type":  issue.IssueType,
		"order_no":    issue.OrderNo,
		"provider":    issue.Provider,
		"description": issue.Description,
		"severity":    issue.Severity,
	}
	if issue.ExpectedAmount != nil {
		vars["expected_amount"] = fmt.Sprintf("%.2f", *issue.ExpectedAmount)
	}
	accountID := int64(0) // admin-level alert
	if issue.AccountID != nil {
		accountID = *issue.AccountID
	}
	return m.send(ctx, notifyRequest{
		AccountID: accountID,
		EventType: "billing.reconciliation.critical",
		EventID:   fmt.Sprintf("recon_%s_%s", issue.IssueType, issue.OrderNo),
		Channels:  channels,
		Vars:      vars,
	})
}

// Register registers all notification module hooks into the module registry.
func (m *NotificationModule) Register(r *Registry) {
	r.OnAccountCreated(m.OnAccountCreated)
	r.OnCheckin(m.OnCheckin)
	r.OnReferralSignup(m.OnReferralSignup)
	r.OnReconciliationIssue(m.OnReconciliationIssue)
	slog.Info("module registered", "module", "notification",
		"service_url", m.cfg.ServiceURL,
		"hooks", "account_created,checkin,referral_signup,reconciliation_issue")
}

type notifyRequest struct {
	AccountID int64             `json:"account_id"`
	EventType string            `json:"event_type"`
	EventID   string            `json:"event_id,omitempty"`
	Channels  []string          `json:"channels"`
	Vars      map[string]string `json:"vars"`
	EmailAddr string            `json:"email_addr,omitempty"`
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
