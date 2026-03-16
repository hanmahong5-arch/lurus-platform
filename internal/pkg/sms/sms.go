// Package sms provides an abstraction layer for sending SMS messages
// via different cloud providers (Tencent Cloud, Aliyun).
package sms

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Sender is the interface for sending SMS messages.
type Sender interface {
	Send(ctx context.Context, phone, templateID string, params map[string]string) error
}

// NoopSender is a no-op implementation used when SMS is not configured.
type NoopSender struct{}

func (NoopSender) Send(_ context.Context, phone, templateID string, params map[string]string) error {
	slog.Warn("sms: noop sender, message not sent", "phone", phone, "template", templateID)
	return nil
}

// SMSConfig holds provider-agnostic SMS configuration.
type SMSConfig struct {
	Provider string // "tencent" or "aliyun" (empty = noop)

	// Tencent Cloud SMS
	TencentSecretID         string
	TencentSecretKey        string
	TencentAppID            string
	TencentSignName         string
	TencentTemplateIDVerify string
	TencentTemplateIDReset  string

	// Aliyun SMS
	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
	AliyunSignName        string
	AliyunTemplateVerify  string
	AliyunTemplateReset   string
}

// LoadFromEnv populates SMSConfig from environment variables.
func LoadFromEnv() SMSConfig {
	return SMSConfig{
		Provider:                os.Getenv("SMS_PROVIDER"),
		TencentSecretID:         os.Getenv("SMS_TENCENT_SECRET_ID"),
		TencentSecretKey:        os.Getenv("SMS_TENCENT_SECRET_KEY"),
		TencentAppID:            os.Getenv("SMS_TENCENT_APP_ID"),
		TencentSignName:         os.Getenv("SMS_TENCENT_SIGN_NAME"),
		TencentTemplateIDVerify: os.Getenv("SMS_TENCENT_TEMPLATE_ID_VERIFY"),
		TencentTemplateIDReset:  os.Getenv("SMS_TENCENT_TEMPLATE_ID_RESET"),
		AliyunAccessKeyID:       os.Getenv("SMS_ALIYUN_ACCESS_KEY_ID"),
		AliyunAccessKeySecret:   os.Getenv("SMS_ALIYUN_ACCESS_KEY_SECRET"),
		AliyunSignName:          os.Getenv("SMS_ALIYUN_SIGN_NAME"),
		AliyunTemplateVerify:    os.Getenv("SMS_ALIYUN_TEMPLATE_CODE_VERIFY"),
		AliyunTemplateReset:     os.Getenv("SMS_ALIYUN_TEMPLATE_CODE_RESET"),
	}
}

// NewFromConfig creates a Sender based on the provider configuration.
// Returns NoopSender if provider is empty or unrecognized.
func NewFromConfig(cfg SMSConfig) (Sender, error) {
	switch cfg.Provider {
	case "tencent":
		if cfg.TencentSecretID == "" || cfg.TencentSecretKey == "" || cfg.TencentAppID == "" {
			return nil, fmt.Errorf("sms: tencent provider requires SMS_TENCENT_SECRET_ID, SECRET_KEY, and APP_ID")
		}
		return NewTencentSender(cfg), nil
	case "aliyun":
		if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
			return nil, fmt.Errorf("sms: aliyun provider requires SMS_ALIYUN_ACCESS_KEY_ID and ACCESS_KEY_SECRET")
		}
		return NewAliyunSender(cfg), nil
	case "":
		slog.Info("sms: no provider configured, using noop sender")
		return NoopSender{}, nil
	default:
		return nil, fmt.Errorf("sms: unsupported provider %q (supported: tencent, aliyun)", cfg.Provider)
	}
}
