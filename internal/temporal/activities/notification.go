package activities

import (
	"context"
	"fmt"

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
		return fmt.Errorf("get account %d: %w", in.AccountID, err)
	}
	if account.Email == "" {
		return nil // no email on file, skip silently
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
	return a.Mailer.Send(ctx, account.Email, subject, body)
}
