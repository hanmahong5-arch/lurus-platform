package sender

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
)

const (
	// ContentTypeHTML signals that the message body is HTML.
	ContentTypeHTML = "text/html"
)

// EmailSender delivers notifications via SMTP (Stalwart mail server).
type EmailSender struct {
	addr string
	from string
	auth smtp.Auth
}

// NewEmailSender creates an SMTP-based email sender.
// Returns nil if host is empty (email disabled).
func NewEmailSender(host string, port int, username, password, from string) *EmailSender {
	if host == "" {
		return nil
	}
	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	return &EmailSender{
		addr: fmt.Sprintf("%s:%d", host, port),
		from: from,
		auth: auth,
	}
}

// Send delivers an email message. Supports both plain text and HTML content
// based on the "content_type" metadata field.
func (s *EmailSender) Send(_ context.Context, msg Message) error {
	if msg.To == "" {
		return fmt.Errorf("email send: recipient address is empty")
	}

	contentType := "text/plain; charset=UTF-8"
	if ct, ok := msg.Metadata["content_type"]; ok && ct == ContentTypeHTML {
		contentType = "text/html; charset=UTF-8"
	}

	var raw strings.Builder
	raw.WriteString("From: " + s.from + "\r\n")
	raw.WriteString("To: " + msg.To + "\r\n")
	raw.WriteString("Subject: " + msg.Subject + "\r\n")
	raw.WriteString("MIME-Version: 1.0\r\n")
	raw.WriteString("Content-Type: " + contentType + "\r\n\r\n")
	raw.WriteString(msg.Body + "\r\n")

	if err := smtp.SendMail(s.addr, s.auth, s.from, []string{msg.To}, []byte(raw.String())); err != nil {
		return fmt.Errorf("smtp send to %s: %w", msg.To, err)
	}
	return nil
}

// Name returns the channel name.
func (s *EmailSender) Name() string { return "email" }

// NoopEmailSender discards messages when SMTP is not configured.
type NoopEmailSender struct{}

// Send is a no-op.
func (NoopEmailSender) Send(_ context.Context, _ Message) error { return nil }

// Name returns the channel name.
func (NoopEmailSender) Name() string { return "email_noop" }
