// Package config loads and validates environment variables at startup.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all service configuration, loaded from env vars.
type Config struct {
	Port            int
	Env             string
	DatabaseDSN     string
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	NATSAddr        string
	SMTPHost        string
	SMTPPort        int
	SMTPUser        string
	SMTPPass        string
	EmailFrom       string
	InternalAPIKey  string
	ShutdownTimeout time.Duration
	FCMCredentials  string // path to FCM service account JSON (empty = disabled)
	AlertAdminEmail string // email recipient for system alerts from Alertmanager
	WebhookSecret   string // shared secret for webhook endpoints (empty = no auth)
}

// Load reads configuration from environment variables and returns an error
// if any required variable is missing.
func Load() (*Config, error) {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_DSN is required")
	}
	internalKey := os.Getenv("INTERNAL_API_KEY")
	if internalKey == "" {
		return nil, fmt.Errorf("INTERNAL_API_KEY is required")
	}

	cfg := &Config{
		Port:            envInt("PORT", 18900),
		Env:             envStr("ENV", "production"),
		DatabaseDSN:     dsn,
		RedisAddr:       envStr("REDIS_ADDR", "redis.messaging.svc:6379"),
		RedisPassword:   envStr("REDIS_PASSWORD", ""),
		RedisDB:         envInt("REDIS_DB", 4),
		NATSAddr:        envStr("NATS_ADDR", "nats://nats.messaging.svc:4222"),
		SMTPHost:        envStr("SMTP_HOST", ""),
		SMTPPort:        envInt("SMTP_PORT", 587),
		SMTPUser:        envStr("SMTP_USER", ""),
		SMTPPass:        envStr("SMTP_PASS", ""),
		EmailFrom:       envStr("EMAIL_FROM", "noreply@lurus.cn"),
		InternalAPIKey:  internalKey,
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 30*time.Second),
		FCMCredentials:  envStr("FCM_CREDENTIALS_PATH", ""),
		AlertAdminEmail: envStr("ALERT_ADMIN_EMAIL", ""),
		WebhookSecret:   envStr("WEBHOOK_SECRET", ""),
	}
	return cfg, nil
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
