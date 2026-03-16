package payment

import (
	"context"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// Epay tests cover constructor behavior and CreateCheckout (pure URL builder — no HTTP calls).

func TestNewEpayProvider_Disabled_EmptyPartnerID(t *testing.T) {
	p, err := NewEpayProvider("", "key", "https://epay.example.com", "https://notify.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when partner ID empty")
	}
}

func TestNewEpayProvider_Disabled_EmptyKey(t *testing.T) {
	p, err := NewEpayProvider("12345", "", "https://epay.example.com", "https://notify.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when key empty")
	}
}

func TestNewEpayProvider_Disabled_EmptyGateway(t *testing.T) {
	p, err := NewEpayProvider("12345", "key", "", "https://notify.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when gateway URL empty")
	}
}

func TestNewEpayProvider_Valid(t *testing.T) {
	p, err := NewEpayProvider("12345", "testkey", "https://epay.example.com", "https://notify.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Name() != "epay_alipay" {
		t.Errorf("Name() = %q, want epay_alipay", p.Name())
	}
}

// TestEpayProvider_CreateCheckout_Alipay verifies CreateCheckout builds a redirect URL for alipay.
func TestEpayProvider_CreateCheckout_Alipay(t *testing.T) {
	p, err := NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com/callback")
	if err != nil {
		t.Fatalf("NewEpayProvider: %v", err)
	}
	order := &entity.PaymentOrder{
		OrderNo:       "LO-20260309-001",
		AmountCNY:     10.00,
		PaymentMethod: "epay_alipay",
	}
	payURL, externalID, err := p.CreateCheckout(context.Background(), order, "https://return.example.com/return")
	if err != nil {
		t.Fatalf("CreateCheckout: %v", err)
	}
	if payURL == "" {
		t.Error("expected non-empty payURL")
	}
	if externalID != order.OrderNo {
		t.Errorf("externalID = %q, want %q", externalID, order.OrderNo)
	}
}

// TestEpayProvider_CreateCheckout_WxPay verifies CreateCheckout selects wxpay type correctly.
func TestEpayProvider_CreateCheckout_WxPay(t *testing.T) {
	p, err := NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com/callback")
	if err != nil {
		t.Fatalf("NewEpayProvider: %v", err)
	}
	order := &entity.PaymentOrder{
		OrderNo:       "LO-20260309-002",
		AmountCNY:     20.00,
		PaymentMethod: "epay_wxpay",
	}
	payURL, _, err := p.CreateCheckout(context.Background(), order, "https://return.example.com/return")
	if err != nil {
		t.Fatalf("CreateCheckout wxpay: %v", err)
	}
	if payURL == "" {
		t.Error("expected non-empty payURL for wxpay")
	}
}

// TestEpayProvider_VerifyCallback_InvalidSignature verifies that empty/wrong params return false.
func TestEpayProvider_VerifyCallback_InvalidSignature(t *testing.T) {
	p, _ := NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com/callback")

	// Empty params — signature verification should fail.
	_, ok := p.VerifyCallback(nil)
	if ok {
		t.Error("expected ok=false for empty params")
	}
}

// TestEpayProvider_VerifyCallback_WrongStatus verifies non-success trade status returns false.
func TestEpayProvider_VerifyCallback_WrongStatus(t *testing.T) {
	p, _ := NewEpayProvider("12345", "testkey12345678", "https://pay.example.com", "https://notify.example.com/callback")

	// Params with wrong trade status and no valid signature.
	params := map[string][]string{
		"trade_status": {"TRADE_CLOSED"},
		"out_trade_no": {"LO-001"},
	}
	_, ok := p.VerifyCallback(params)
	if ok {
		t.Error("expected ok=false for non-success trade status without valid signature")
	}
}
