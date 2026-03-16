// Package email provides an abstraction for email delivery.
package email

import (
	"context"
	"fmt"
	"net/smtp"
)

// Sender abstracts email delivery so callers are not coupled to SMTP.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// NoopSender discards all messages. Used when SMTP is not configured so the
// service starts normally without email credentials in development or staging.
type NoopSender struct{}

// Send is a no-op that always returns nil.
func (NoopSender) Send(_ context.Context, _, _, _ string) error { return nil }

// SMTPSender delivers email via SMTP with PLAIN authentication.
type SMTPSender struct {
	addr string // host:port
	from string
	auth smtp.Auth
}

// NewSMTPSender creates an SMTPSender. All parameters are required.
func NewSMTPSender(host string, port int, username, password, from string) *SMTPSender {
	auth := smtp.PlainAuth("", username, password, host)
	return &SMTPSender{
		addr: fmt.Sprintf("%s:%d", host, port),
		from: from,
		auth: auth,
	}
}

// Send delivers a plain-text email. The ctx parameter is accepted for interface
// conformance; SMTP delivery does not support context cancellation natively.
func (s *SMTPSender) Send(_ context.Context, to, subject, body string) error {
	msg := []byte(
		"From: " + s.from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n\r\n" +
			body + "\r\n",
	)
	return smtp.SendMail(s.addr, s.auth, s.from, []string{to}, msg)
}
