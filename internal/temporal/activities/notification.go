package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
)

// NotificationActivities wraps email sending for Temporal.
type NotificationActivities struct {
	Mailer   email.Sender
	Accounts *app.AccountService
}

// SendReminderInput is the input for the SendExpiryReminder activity.
type SendReminderInput struct {
	AccountID      int64
	SubscriptionID int64
	ProductID      string
	DaysLeft       int
	ExpiresAt      string // RFC3339 or "2006-01-02 15:04 UTC"
}

// SendExpiryReminder fetches account info and sends an expiry reminder email.
func (a *NotificationActivities) SendExpiryReminder(ctx context.Context, in SendReminderInput) error {
	account, err := a.Accounts.GetByID(ctx, in.AccountID)
	if err != nil || account == nil {
		slog.Warn("activity/send-reminder: account lookup failed", "account_id", in.AccountID, "err", err)
		return fmt.Errorf("get account %d: %w", in.AccountID, err)
	}
	if account.Email == "" {
		slog.Info("activity/send-reminder: no email, skipping", "account_id", in.AccountID, "sub_id", in.SubscriptionID)
		return nil
	}

	subject := fmt.Sprintf("Your subscription expires in %d day(s)", in.DaysLeft)
	body := fmt.Sprintf(
		"Dear %s,\r\n\r\n"+
			"Your subscription (ID: %d) for product %q will expire in %d day(s) on %s.\r\n\r\n"+
			"Please renew your subscription to continue uninterrupted service.\r\n\r\n"+
			"Lurus Platform",
		account.DisplayName,
		in.SubscriptionID,
		in.ProductID,
		in.DaysLeft,
		in.ExpiresAt,
	)
	if err := a.Mailer.Send(ctx, account.Email, subject, body); err != nil {
		slog.Warn("activity/send-reminder: send failed", "account_id", in.AccountID, "sub_id", in.SubscriptionID, "email", account.Email, "err", err)
		return err
	}
	slog.Info("activity/send-reminder", "account_id", in.AccountID, "sub_id", in.SubscriptionID, "days_left", in.DaysLeft)
	return nil
}
