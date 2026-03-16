package sender

import (
	"context"
	"log/slog"
)

// FCMSender is a placeholder for Firebase Cloud Messaging push notifications.
// Activated in Sprint D when Flutter mobile app is ready.
type FCMSender struct{}

// NewFCMSender creates a placeholder FCM sender.
// In Phase D, this will be replaced with actual FCM integration.
func NewFCMSender(_ string) *FCMSender {
	return &FCMSender{}
}

// Send logs the message but does not actually deliver it.
// Real implementation will use firebase.google.com/go/v4/messaging.
func (f *FCMSender) Send(_ context.Context, msg Message) error {
	slog.Debug("fcm push placeholder",
		"to", msg.To,
		"subject", msg.Subject,
	)
	return nil
}

// Name returns the channel name.
func (f *FCMSender) Name() string { return "fcm" }
