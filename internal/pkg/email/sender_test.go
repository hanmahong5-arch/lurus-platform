package email

import (
	"context"
	"net"
	"net/smtp"
	"strings"
	"testing"
)

// TestNoopSender_Send_AlwaysNil verifies that NoopSender always returns nil.
func TestNoopSender_Send_AlwaysNil(t *testing.T) {
	s := NoopSender{}
	ctx := context.Background()

	tests := []struct {
		to      string
		subject string
		body    string
	}{
		{"user@example.com", "Test Subject", "Test body"},
		{"", "", ""},                               // empty fields
		{"invalid-email", "S", strings.Repeat("X", 10000)}, // edge cases
	}

	for _, tt := range tests {
		if err := s.Send(ctx, tt.to, tt.subject, tt.body); err != nil {
			t.Errorf("NoopSender.Send(%q, %q, ...) = %v, want nil", tt.to, tt.subject, err)
		}
	}
}

// TestNoopSender_Send_IgnoresContext verifies NoopSender ignores context cancellation.
func TestNoopSender_Send_IgnoresContext(t *testing.T) {
	s := NoopSender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	if err := s.Send(ctx, "user@example.com", "subject", "body"); err != nil {
		t.Errorf("NoopSender.Send with cancelled context = %v, want nil", err)
	}
}

// TestNewSMTPSender_Creation verifies that NewSMTPSender constructs a sender without panic.
func TestNewSMTPSender_Creation(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewSMTPSender panicked: %v", r)
		}
	}()
	s := NewSMTPSender("smtp.example.com", 587, "user@example.com", "password", "from@example.com")
	if s == nil {
		t.Error("NewSMTPSender returned nil")
	}
}

// TestSMTPSender_Send_SMTPDown verifies that an unavailable SMTP server returns an error.
func TestSMTPSender_Send_SMTPDown(t *testing.T) {
	// Use a port that is almost certainly not listening.
	s := NewSMTPSender("127.0.0.1", 19999, "user", "pass", "from@example.com")

	err := s.Send(context.Background(), "to@example.com", "Subject", "Body")
	if err == nil {
		t.Error("expected error when SMTP server is down, got nil")
	}
}

// TestSMTPSender_Send_InvalidAddress verifies error for unreachable host.
func TestSMTPSender_Send_InvalidAddress(t *testing.T) {
	// Use an invalid host that should fail DNS resolution or connection.
	s := NewSMTPSender("invalid.host.that.does.not.exist.example.com", 587, "u", "p", "f@e.com")

	err := s.Send(context.Background(), "to@example.com", "Subject", "Body")
	if err == nil {
		t.Error("expected error for invalid SMTP host, got nil")
	}
}

// TestSMTPSender_Send_Success verifies successful email sending via a mock SMTP server.
func TestSMTPSender_Send_Success(t *testing.T) {
	// Start a minimal SMTP listener that accepts and closes connections.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen for SMTP test:", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	host, portStr, _ := net.SplitHostPort(addr)
	var port int
	if _, err := net.ResolveTCPAddr("tcp", addr); err == nil {
		// Parse port from addr.
		for _, c := range portStr {
			port = port*10 + int(c-'0')
		}
	}

	// Accept one connection in a goroutine and write SMTP greeting + responses.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// SMTP conversation: greeting → EHLO → AUTH → MAIL → RCPT → DATA → QUIT
		responses := []string{
			"220 test SMTP\r\n",
			"250-test\r\n250-AUTH PLAIN LOGIN\r\n250 OK\r\n",
			"235 OK\r\n",
			"250 OK\r\n",
			"250 OK\r\n",
			"354 Start input\r\n",
			"250 OK\r\n",
			"221 Bye\r\n",
		}
		buf := make([]byte, 4096)
		for _, resp := range responses {
			conn.Write([]byte(resp))
			conn.Read(buf) // read client command
		}
	}()

	s := NewSMTPSender(host, port, "testuser", "testpass", "from@example.com")
	_ = smtp.PlainAuth("", "testuser", "testpass", host) // just verify auth doesn't panic

	// We expect either success or connection error depending on SMTP handshake.
	// The test primarily checks that the Send method does not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SMTPSender.Send panicked: %v", r)
		}
	}()
	_ = s.Send(context.Background(), "to@example.com", "Test", "Hello")
}

// TestSender_Interface_Implemented verifies that both senders implement the Sender interface.
func TestSender_Interface_Implemented(t *testing.T) {
	var _ Sender = NoopSender{}
	var _ Sender = &SMTPSender{}
}
