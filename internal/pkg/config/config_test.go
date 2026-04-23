package config

import (
	"os"
	"sync"
	"testing"
	"time"
)

// setEnv sets env vars for the duration of a test and restores them on cleanup.
func setEnv(t *testing.T, kvs map[string]string) {
	t.Helper()
	old := make(map[string]string, len(kvs))
	for k, v := range kvs {
		old[k] = os.Getenv(k)
		os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k, v := range old {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	})
}

// requiredEnvs returns the minimal set of required env vars.
// SESSION_SECRET must decode to ≥ 32 bytes (see Validate); we use a 32-byte
// base64 test value here.
func requiredEnvs() map[string]string {
	return map[string]string{
		"DATABASE_DSN":     "postgres://test:test@localhost/test",
		"ZITADEL_ISSUER":  "https://auth.example.com",
		"ZITADEL_JWKS_URL": "https://auth.example.com/oauth/v2/keys",
		"INTERNAL_API_KEY": "test-internal-key",
		"SESSION_SECRET":   "dGVzdC1zZXNzaW9uLXNlY3JldC1rZXktMzItYnl0ZXMh", // "test-session-secret-key-32-bytes!" base64
	}
}

// TestConfig_Load_DefaultValues verifies that optional fields use their defaults.
func TestConfig_Load_DefaultValues(t *testing.T) {
	setEnv(t, requiredEnvs())
	// Clear any optional overrides that may be set in the environment.
	optionals := []string{
		"PORT", "GRPC_PORT", "ENV", "REDIS_ADDR", "REDIS_DB",
		"NATS_ADDR", "ZITADEL_AUDIENCE", "ZITADEL_ADMIN_ROLE",
		"RATE_LIMIT_IP_PER_MINUTE", "RATE_LIMIT_USER_PER_MINUTE",
		"GRACE_PERIOD_DAYS", "SHUTDOWN_TIMEOUT", "CACHE_ENTITLEMENT_TTL",
		"EMAIL_SMTP_PORT", "OTEL_SERVICE_NAME",
	}
	for _, k := range optionals {
		old := os.Getenv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if old != "" {
				os.Setenv(k, old)
			}
		})
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"Port", cfg.Port, 18104},
		{"GRPCPort", cfg.GRPCPort, 18105},
		{"Env", cfg.Env, "production"},
		{"RedisAddr", cfg.RedisAddr, "redis.messaging.svc:6379"},
		{"RedisDB", cfg.RedisDB, 3},
		{"NATSAddr", cfg.NATSAddr, "nats://nats.messaging.svc:4222"},
		{"ZitadelAdminRole", cfg.ZitadelAdminRole, "admin"},
		{"RateLimitIPPerMinute", cfg.RateLimitIPPerMinute, 120},
		{"RateLimitUserPerMinute", cfg.RateLimitUserPerMinute, 300},
		{"GracePeriodDays", cfg.GracePeriodDays, 3},
		{"ShutdownTimeout", cfg.ShutdownTimeout, 30 * time.Second},
		{"CacheEntitlementTTL", cfg.CacheEntitlementTTL, 5 * time.Minute},
		{"EmailSMTPPort", cfg.EmailSMTPPort, 587},
		{"OtelServiceName", cfg.OtelServiceName, "lurus-platform"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestConfig_Load_EnvOverride verifies that environment variables override defaults.
func TestConfig_Load_EnvOverride(t *testing.T) {
	envs := requiredEnvs()
	envs["PORT"] = "9999"
	envs["REDIS_DB"] = "5"
	envs["GRACE_PERIOD_DAYS"] = "7"
	envs["SHUTDOWN_TIMEOUT"] = "60s"
	envs["ZITADEL_ADMIN_ROLE"] = "superadmin"
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
	if cfg.RedisDB != 5 {
		t.Errorf("RedisDB = %d, want 5", cfg.RedisDB)
	}
	if cfg.GracePeriodDays != 7 {
		t.Errorf("GracePeriodDays = %d, want 7", cfg.GracePeriodDays)
	}
	if cfg.ShutdownTimeout != 60*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 60s", cfg.ShutdownTimeout)
	}
	if cfg.ZitadelAdminRole != "superadmin" {
		t.Errorf("ZitadelAdminRole = %q, want %q", cfg.ZitadelAdminRole, "superadmin")
	}
}

// TestConfig_Load_InvalidInt_UsesDefault verifies that a non-integer value falls back to the default.
func TestConfig_Load_InvalidInt_UsesDefault(t *testing.T) {
	envs := requiredEnvs()
	envs["PORT"] = "not-a-number"
	envs["REDIS_DB"] = "abc"
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != 18104 {
		t.Errorf("invalid PORT should use default 18104, got %d", cfg.Port)
	}
	if cfg.RedisDB != 3 {
		t.Errorf("invalid REDIS_DB should use default 3, got %d", cfg.RedisDB)
	}
}

// TestConfig_Load_InvalidDuration_UsesDefault verifies duration fallback.
func TestConfig_Load_InvalidDuration_UsesDefault(t *testing.T) {
	envs := requiredEnvs()
	envs["SHUTDOWN_TIMEOUT"] = "not-a-duration"
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("invalid SHUTDOWN_TIMEOUT should use default 30s, got %v", cfg.ShutdownTimeout)
	}
}

// TestConfig_Load_RequiredField_Panics verifies that missing required env var causes panic.
func TestConfig_Load_RequiredField_Panics(t *testing.T) {
	// Clear required fields.
	os.Unsetenv("DATABASE_DSN")
	os.Unsetenv("ZITADEL_ISSUER")
	os.Unsetenv("ZITADEL_JWKS_URL")
	os.Unsetenv("INTERNAL_API_KEY")
	t.Cleanup(func() {
		// Restore nothing — each test is responsible for its own env setup.
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing required env var, but did not panic")
		}
	}()
	_, _ = Load() // should panic
}

// TestConfig_Load_RequiredFields_Values verifies that required fields are set correctly.
func TestConfig_Load_RequiredFields_Values(t *testing.T) {
	envs := requiredEnvs()
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseDSN != envs["DATABASE_DSN"] {
		t.Errorf("DatabaseDSN = %q, want %q", cfg.DatabaseDSN, envs["DATABASE_DSN"])
	}
	if cfg.ZitadelIssuer != envs["ZITADEL_ISSUER"] {
		t.Errorf("ZitadelIssuer = %q, want %q", cfg.ZitadelIssuer, envs["ZITADEL_ISSUER"])
	}
	if cfg.ZitadelJWKSURL != envs["ZITADEL_JWKS_URL"] {
		t.Errorf("ZitadelJWKSURL = %q, want %q", cfg.ZitadelJWKSURL, envs["ZITADEL_JWKS_URL"])
	}
	if cfg.InternalAPIKey != envs["INTERNAL_API_KEY"] {
		t.Errorf("InternalAPIKey = %q, want %q", cfg.InternalAPIKey, envs["INTERNAL_API_KEY"])
	}
}

// TestConfig_Load_Concurrent_Safe verifies that concurrent Load calls do not race.
func TestConfig_Load_Concurrent_Safe(t *testing.T) {
	setEnv(t, requiredEnvs())

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			cfg, err := Load()
			if err != nil {
				errs[idx] = err
				return
			}
			if cfg == nil {
				errs[idx] = errNilConfig
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error: %v", i, err)
		}
	}
}

var errNilConfig = errorString("Load() returned nil config")

type errorString string

func (e errorString) Error() string { return string(e) }

// TestConfig_OptionalPaymentFields verifies payment-related optional fields default to empty.
func TestConfig_OptionalPaymentFields(t *testing.T) {
	envs := requiredEnvs()
	// Explicitly unset payment fields.
	unset := []string{
		"STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET",
		"EPAY_PARTNER_ID", "EPAY_KEY",
		"CREEM_API_KEY", "CREEM_WEBHOOK_SECRET",
	}
	for _, k := range unset {
		old := os.Getenv(k)
		os.Unsetenv(k)
		k := k
		t.Cleanup(func() {
			if old != "" {
				os.Setenv(k, old)
			}
		})
	}
	setEnv(t, envs)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StripeSecretKey != "" {
		t.Errorf("StripeSecretKey should be empty by default, got %q", cfg.StripeSecretKey)
	}
	if cfg.EpayPartnerID != "" {
		t.Errorf("EpayPartnerID should be empty by default")
	}
}
