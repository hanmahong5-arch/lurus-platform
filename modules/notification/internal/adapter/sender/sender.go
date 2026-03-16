// Package sender defines the Sender interface and multi-channel dispatch.
package sender

import "context"

// Message is the payload passed to a channel sender.
type Message struct {
	To       string // email address, device token, or account ID (depending on channel)
	Subject  string
	Body     string
	Priority string
	Metadata map[string]string
}

// Sender abstracts a delivery channel so the notification service is not
// coupled to any specific transport.
type Sender interface {
	// Send delivers a message through this channel. Returns nil on success.
	Send(ctx context.Context, msg Message) error
	// Name returns the channel name for logging.
	Name() string
}
