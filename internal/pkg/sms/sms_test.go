package sms

import (
	"context"
	"testing"
)

func TestNewFromConfig_NoopWhenEmpty(t *testing.T) {
	sender, err := NewFromConfig(SMSConfig{Provider: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sender.(NoopSender); !ok {
		t.Errorf("expected NoopSender, got %T", sender)
	}
}

func TestNewFromConfig_UnsupportedProvider(t *testing.T) {
	_, err := NewFromConfig(SMSConfig{Provider: "twilio"})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestNewFromConfig_TencentMissingCredentials(t *testing.T) {
	_, err := NewFromConfig(SMSConfig{Provider: "tencent"})
	if err == nil {
		t.Fatal("expected error when tencent credentials missing")
	}
}

func TestNewFromConfig_TencentWithCredentials(t *testing.T) {
	sender, err := NewFromConfig(SMSConfig{
		Provider:         "tencent",
		TencentSecretID:  "id",
		TencentSecretKey: "key",
		TencentAppID:     "app",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
}

func TestNewFromConfig_AliyunMissingCredentials(t *testing.T) {
	_, err := NewFromConfig(SMSConfig{Provider: "aliyun"})
	if err == nil {
		t.Fatal("expected error when aliyun credentials missing")
	}
}

func TestNewFromConfig_AliyunWithCredentials(t *testing.T) {
	sender, err := NewFromConfig(SMSConfig{
		Provider:              "aliyun",
		AliyunAccessKeyID:     "id",
		AliyunAccessKeySecret: "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
}

func TestNoopSender_Send(t *testing.T) {
	s := NoopSender{}
	err := s.Send(context.Background(), "13800138000", "TPL001", map[string]string{"code": "1234"})
	if err != nil {
		t.Fatalf("NoopSender.Send should not error: %v", err)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("SMS_PROVIDER", "tencent")
	t.Setenv("SMS_TENCENT_SECRET_ID", "test-id")
	t.Setenv("SMS_TENCENT_APP_ID", "test-app")

	cfg := LoadFromEnv()
	if cfg.Provider != "tencent" {
		t.Errorf("Provider = %q, want 'tencent'", cfg.Provider)
	}
	if cfg.TencentSecretID != "test-id" {
		t.Errorf("TencentSecretID = %q, want 'test-id'", cfg.TencentSecretID)
	}
	if cfg.TencentAppID != "test-app" {
		t.Errorf("TencentAppID = %q, want 'test-app'", cfg.TencentAppID)
	}
}

func TestLoadFromEnv_Defaults(t *testing.T) {
	// No env vars set — all fields should be empty.
	cfg := LoadFromEnv()
	if cfg.Provider != "" {
		t.Errorf("Provider = %q, want empty", cfg.Provider)
	}
}
