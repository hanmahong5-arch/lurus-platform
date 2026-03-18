package sender

import (
	"context"
	"fmt"
	"log/slog"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// FCMSender delivers push notifications via Firebase Cloud Messaging.
type FCMSender struct {
	client *messaging.Client
}

// NewFCMSender creates an FCM sender from a service account JSON credentials path.
// Returns a noop placeholder if credentialsPath is empty.
func NewFCMSender(credentialsPath string) *FCMSender {
	if credentialsPath == "" {
		return &FCMSender{}
	}

	ctx := context.Background()
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialsPath))
	if err != nil {
		slog.Warn("fcm: firebase app init failed, using noop",
			"path", credentialsPath, "err", err)
		return &FCMSender{}
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		slog.Warn("fcm: messaging client init failed, using noop", "err", err)
		return &FCMSender{}
	}

	slog.Info("fcm push sender enabled")
	return &FCMSender{client: client}
}

// Send delivers a push notification to the device token specified in msg.To.
// If the FCM client is not initialized, this is a noop.
func (f *FCMSender) Send(ctx context.Context, msg Message) error {
	if f.client == nil {
		slog.Debug("fcm push noop", "to", msg.To, "subject", msg.Subject)
		return nil
	}

	if msg.To == "" {
		return fmt.Errorf("fcm send: device token is empty")
	}

	fcmMsg := &messaging.Message{
		Token: msg.To,
		Notification: &messaging.Notification{
			Title: msg.Subject,
			Body:  msg.Body,
		},
		Data: msg.Metadata,
	}

	// Set priority for urgent notifications.
	if msg.Priority == "urgent" || msg.Priority == "high" {
		fcmMsg.Android = &messaging.AndroidConfig{
			Priority: "high",
		}
	}

	response, err := f.client.Send(ctx, fcmMsg)
	if err != nil {
		// Check for unregistered token — caller should deactivate this token.
		if messaging.IsUnregistered(err) {
			return fmt.Errorf("fcm send: token unregistered: %w", err)
		}
		return fmt.Errorf("fcm send: %w", err)
	}

	slog.Debug("fcm push sent", "response", response, "to", msg.To)
	return nil
}

// Name returns the channel name.
func (f *FCMSender) Name() string { return "fcm" }

// IsActive returns true if the FCM client is initialized.
func (f *FCMSender) IsActive() bool { return f.client != nil }
