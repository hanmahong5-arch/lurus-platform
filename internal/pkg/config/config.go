// Package config loads and validates all service configuration from environment variables.
package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all service configuration.
type Config struct {
	// Server
	Port     int
	GRPCPort int
	Env      string

	// Database
	DatabaseDSN string

	// Redis
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// NATS
	NATSAddr string

	// Auth — Zitadel JWT validation
	ZitadelIssuer    string // ZITADEL_ISSUER (e.g. https://auth.lurus.cn)
	ZitadelAudience  string // ZITADEL_AUDIENCE (project ID)
	ZitadelJWKSURL   string // ZITADEL_JWKS_URL (e.g. https://auth.lurus.cn/oauth/v2/keys)
	ZitadelAdminRole string // ZITADEL_ADMIN_ROLE (default: admin)

	// Auth (internal service key for /internal/* routes)
	InternalAPIKey string

	// Rate limiting
	RateLimitIPPerMinute   int // RATE_LIMIT_IP_PER_MINUTE (default: 120)
	RateLimitUserPerMinute int // RATE_LIMIT_USER_PER_MINUTE (default: 300)

	// Subscription automation
	GracePeriodDays int // GRACE_PERIOD_DAYS (default: 3)

	// Payment providers
	StripeSecretKey     string
	StripeWebhookSecret string
	StripeReturnURL     string
	StripeUSDRate       float64 // STRIPE_USD_RATE (CNY→USD rate; default 7.1; update regularly or use live FX)
	EpayPartnerID       string
	EpayKey             string
	EpayGatewayURL      string
	EpayNotifyURL       string
	EpayReturnURL       string
	CreemAPIKey         string
	CreemWebhookSecret  string
	CreemReturnURL      string

	// Alipay (direct integration via go-pay)
	AlipayAppID         string // ALIPAY_APP_ID
	AlipayPrivateKey    string // ALIPAY_PRIVATE_KEY (RSA2 PKCS1)
	AlipayAppPublicCert string // ALIPAY_APP_PUBLIC_CERT (file path or base64 content)
	AlipayPublicCert    string // ALIPAY_PUBLIC_CERT (file path or base64 content)
	AlipayRootCert      string // ALIPAY_ROOT_CERT (file path or base64 content)
	AlipayNotifyURL     string // ALIPAY_NOTIFY_URL
	AlipayReturnURL     string // ALIPAY_RETURN_URL
	AlipayIsProd        bool   // ALIPAY_IS_PROD (default: true)

	// WeChat Pay v3 (direct integration via go-pay)
	WechatPayMchID      string // WECHAT_PAY_MCH_ID
	WechatPaySerialNo   string // WECHAT_PAY_SERIAL_NO (merchant cert serial)
	WechatPayAPIv3Key   string // WECHAT_PAY_API_V3_KEY
	WechatPayPrivateKey string // WECHAT_PAY_PRIVATE_KEY (merchant RSA private key, PEM)
	WechatPayAppID      string // WECHAT_PAY_APP_ID
	WechatPayNotifyURL  string // WECHAT_PAY_NOTIFY_URL

	// WorldFirst (万里汇, built on Alipay+ infrastructure)
	WorldFirstClientID   string // WORLDFIRST_CLIENT_ID
	WorldFirstPrivateKey string // WORLDFIRST_PRIVATE_KEY (merchant RSA private key, PEM)
	WorldFirstPublicKey  string // WORLDFIRST_PUBLIC_KEY (WorldFirst's RSA public key, PEM)
	WorldFirstGateway    string // WORLDFIRST_GATEWAY (default: https://open-sea-global.alipay.com)
	WorldFirstNotifyURL  string // WORLDFIRST_NOTIFY_URL
	WorldFirstKeyVersion string // WORLDFIRST_KEY_VERSION (default: "1")

	// Email (SMTP)
	EmailSMTPHost string // EMAIL_SMTP_HOST (empty = noop sender)
	EmailSMTPPort int    // EMAIL_SMTP_PORT (default: 587)
	EmailSMTPUser string // EMAIL_SMTP_USER
	EmailSMTPPass string // EMAIL_SMTP_PASS
	EmailFrom     string // EMAIL_FROM

	// WeChat OAuth proxy (used by WeChat direct login)
	WechatServerAddress string // WECHAT_SERVER_ADDRESS (base URL of wechat proxy)
	WechatServerToken   string // WECHAT_SERVER_TOKEN (bearer token for proxy)

	// Custom Zitadel login UI (ZLogin)
	// Requires a Zitadel service account or PAT with session creation rights.
	ZitadelServiceAccountPAT string // ZITADEL_SERVICE_ACCOUNT_PAT

	// WeChat OAuth2 adapter — allows Zitadel to use WeChat as a Generic OAuth IDP.
	// Set to the same value as Zitadel IDP config → Client Secret.
	WechatOAuthClientSecret string // WECHAT_OAUTH_CLIENT_SECRET

	// Session token (lurus-issued HS256 JWT for WeChat login)
	SessionSecret string // SESSION_SECRET (min 32 bytes recommended)

	// QR signing keyring (comma-separated `kid:hex32[,kid:hex32...]`) — enables
	// zero-downtime rotation of QR payload HMAC keys. Empty = fall back to
	// SessionSecret as a single implicit key (id "default"). Verification
	// accepts any active key in the ring; signing always uses the highest kid.
	QRSigningKeys string // QR_SIGNING_KEYS (optional)

	// CIDR list of trusted proxies for X-Forwarded-For parsing. Without this,
	// any client can spoof their IP by sending X-Forwarded-For, bypassing
	// per-IP rate limits and polluting audit logs. Defaults to typical
	// K8s/Docker/loopback ranges; override if your ingress runs elsewhere.
	TrustedProxiesCIDRs string // TRUSTED_PROXIES

	// Timeouts
	ShutdownTimeout     time.Duration
	CacheEntitlementTTL time.Duration

	// NewAPI admin proxy (identity admin panel → newapi backend)
	NewAPIInternalURL      string // NEWAPI_INTERNAL_URL (e.g. http://lurus-newapi.lurus-system.svc:3000)
	NewAPIAdminAccessToken string // NEWAPI_ADMIN_ACCESS_TOKEN
	NewAPIAdminUserID      string // NEWAPI_ADMIN_USER_ID

	// SMS
	SMSProvider string // SMS_PROVIDER ("tencent" or "aliyun"; empty = noop)

	// Lurus API (currency exchange)
	LurusAPIInternalURL string // LURUS_API_INTERNAL_URL (e.g. http://lurus-api.lurus-system.svc:8850)
	LurusAPIInternalKey string // LURUS_API_INTERNAL_KEY (bearer key for /internal/* on lurus-api)

	// OpenTelemetry tracing
	OtelEndpoint    string // OTEL_EXPORTER_OTLP_ENDPOINT (empty = noop)
	OtelServiceName string // OTEL_SERVICE_NAME (default: lurus-platform)

	// Temporal workflow engine
	TemporalHostPort  string // TEMPORAL_HOST_PORT (empty = disabled)
	TemporalNamespace string // TEMPORAL_NAMESPACE (default: default)
}

// Load reads config from environment variables and validates required fields.
// Fails fast on startup if any required field is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                     parseInt("PORT", 18104),
		GRPCPort:                 parseInt("GRPC_PORT", 18105),
		Env:                      getEnv("ENV", "production"),
		DatabaseDSN:              requireEnv("DATABASE_DSN"),
		RedisAddr:                getEnv("REDIS_ADDR", "redis.messaging.svc:6379"),
		RedisPassword:            getEnv("REDIS_PASSWORD", ""),
		RedisDB:                  parseInt("REDIS_DB", 3),
		NATSAddr:                 getEnv("NATS_ADDR", "nats://nats.messaging.svc:4222"),
		ZitadelIssuer:            requireEnv("ZITADEL_ISSUER"),
		ZitadelAudience:          getEnv("ZITADEL_AUDIENCE", ""),
		ZitadelJWKSURL:           requireEnv("ZITADEL_JWKS_URL"),
		ZitadelAdminRole:         getEnv("ZITADEL_ADMIN_ROLE", "admin"),
		InternalAPIKey:           requireEnv("INTERNAL_API_KEY"),
		RateLimitIPPerMinute:     parseInt("RATE_LIMIT_IP_PER_MINUTE", 120),
		RateLimitUserPerMinute:   parseInt("RATE_LIMIT_USER_PER_MINUTE", 300),
		GracePeriodDays:          parseInt("GRACE_PERIOD_DAYS", 3),
		StripeSecretKey:          getEnv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:      getEnv("STRIPE_WEBHOOK_SECRET", ""),
		StripeReturnURL:          getEnv("STRIPE_RETURN_URL", ""),
		StripeUSDRate:            parseFloat("STRIPE_USD_RATE", 7.1),
		EpayPartnerID:            getEnv("EPAY_PARTNER_ID", ""),
		EpayKey:                  getEnv("EPAY_KEY", ""),
		EpayGatewayURL:           getEnv("EPAY_GATEWAY_URL", ""),
		EpayNotifyURL:            getEnv("EPAY_NOTIFY_URL", ""),
		EpayReturnURL:            getEnv("EPAY_RETURN_URL", ""),
		CreemAPIKey:              getEnv("CREEM_API_KEY", ""),
		CreemWebhookSecret:       getEnv("CREEM_WEBHOOK_SECRET", ""),
		CreemReturnURL:           getEnv("CREEM_RETURN_URL", ""),
		AlipayAppID:              getEnv("ALIPAY_APP_ID", ""),
		AlipayPrivateKey:         getEnv("ALIPAY_PRIVATE_KEY", ""),
		AlipayAppPublicCert:      getEnv("ALIPAY_APP_PUBLIC_CERT", ""),
		AlipayPublicCert:         getEnv("ALIPAY_PUBLIC_CERT", ""),
		AlipayRootCert:           getEnv("ALIPAY_ROOT_CERT", ""),
		AlipayNotifyURL:          getEnv("ALIPAY_NOTIFY_URL", ""),
		AlipayReturnURL:          getEnv("ALIPAY_RETURN_URL", ""),
		AlipayIsProd:             getEnv("ALIPAY_IS_PROD", "true") == "true",
		WechatPayMchID:           getEnv("WECHAT_PAY_MCH_ID", ""),
		WechatPaySerialNo:        getEnv("WECHAT_PAY_SERIAL_NO", ""),
		WechatPayAPIv3Key:        getEnv("WECHAT_PAY_API_V3_KEY", ""),
		WechatPayPrivateKey:      getEnv("WECHAT_PAY_PRIVATE_KEY", ""),
		WechatPayAppID:           getEnv("WECHAT_PAY_APP_ID", ""),
		WechatPayNotifyURL:       getEnv("WECHAT_PAY_NOTIFY_URL", ""),
		WorldFirstClientID:       getEnv("WORLDFIRST_CLIENT_ID", ""),
		WorldFirstPrivateKey:     getEnv("WORLDFIRST_PRIVATE_KEY", ""),
		WorldFirstPublicKey:      getEnv("WORLDFIRST_PUBLIC_KEY", ""),
		WorldFirstGateway:        getEnv("WORLDFIRST_GATEWAY", "https://open-sea-global.alipay.com"),
		WorldFirstNotifyURL:      getEnv("WORLDFIRST_NOTIFY_URL", ""),
		WorldFirstKeyVersion:     getEnv("WORLDFIRST_KEY_VERSION", "1"),
		WechatServerAddress:      getEnv("WECHAT_SERVER_ADDRESS", ""),
		WechatServerToken:        getEnv("WECHAT_SERVER_TOKEN", ""),
		SessionSecret:            getEnv("SESSION_SECRET", ""),
		QRSigningKeys:            getEnv("QR_SIGNING_KEYS", ""),
		TrustedProxiesCIDRs:      getEnv("TRUSTED_PROXIES", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1/32,100.64.0.0/10"),
		ZitadelServiceAccountPAT: getEnv("ZITADEL_SERVICE_ACCOUNT_PAT", ""),
		WechatOAuthClientSecret:  getEnv("WECHAT_OAUTH_CLIENT_SECRET", ""),
		EmailSMTPHost:            getEnv("EMAIL_SMTP_HOST", ""),
		EmailSMTPPort:            parseInt("EMAIL_SMTP_PORT", 587),
		EmailSMTPUser:            getEnv("EMAIL_SMTP_USER", ""),
		EmailSMTPPass:            getEnv("EMAIL_SMTP_PASS", ""),
		EmailFrom:                getEnv("EMAIL_FROM", ""),
		// ShutdownTimeout must exceed qrMaxPollWait (30s) by a healthy margin so
		// in-flight QR long-poll connections can drain naturally on SIGTERM rather
		// than being torn down mid-poll and returning a spurious 5xx to the client.
		ShutdownTimeout:        parseDuration("SHUTDOWN_TIMEOUT", 45*time.Second),
		CacheEntitlementTTL:    parseDuration("CACHE_ENTITLEMENT_TTL", 5*time.Minute),
		NewAPIInternalURL:      getEnv("NEWAPI_INTERNAL_URL", ""),
		NewAPIAdminAccessToken: getEnv("NEWAPI_ADMIN_ACCESS_TOKEN", ""),
		NewAPIAdminUserID:      getEnv("NEWAPI_ADMIN_USER_ID", ""),
		LurusAPIInternalURL:    getEnv("LURUS_API_INTERNAL_URL", "http://lurus-api.lurus-system.svc:8850"),
		LurusAPIInternalKey:    getEnv("LURUS_API_INTERNAL_KEY", ""),
		SMSProvider:            getEnv("SMS_PROVIDER", ""),
		OtelEndpoint:           getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OtelServiceName:        getEnv("OTEL_SERVICE_NAME", "lurus-platform"),
		TemporalHostPort:       getEnv("TEMPORAL_HOST_PORT", ""),
		TemporalNamespace:      getEnv("TEMPORAL_NAMESPACE", "default"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// minSessionSecretBytes is the minimum length we accept for SESSION_SECRET
// after base64 decoding. 32 bytes gives 256-bit HMAC keys — anything less is
// a security risk and almost certainly a misconfiguration.
const minSessionSecretBytes = 32

// Validate enforces invariants that requireEnv alone can't express:
//
//   - SESSION_SECRET must decode to ≥ 32 bytes (HMAC-SHA256 key strength).
//   - Optional feature-flag configs (PAT, SMTP, WeChat, ...) are logged as
//     disabled so the deployment's reduced capability is visible in logs.
//
// Returns an error for invariants that MUST block startup. Non-fatal
// degradations print WARN logs but allow startup to proceed.
func (c *Config) Validate() error {
	// SESSION_SECRET: reject empty; treat input as base64 first, then raw.
	raw := strings.TrimSpace(c.SessionSecret)
	if raw == "" {
		return fmt.Errorf("SESSION_SECRET is required (min %d bytes, base64 or raw)", minSessionSecretBytes)
	}
	n := decodedLen(raw)
	if n < minSessionSecretBytes {
		return fmt.Errorf("SESSION_SECRET too short: decoded %d bytes, need ≥ %d (regenerate: `openssl rand -base64 32`)", n, minSessionSecretBytes)
	}

	// Log degraded-feature summary so operators know what's turned off.
	if c.ZitadelServiceAccountPAT == "" {
		log.Println("config: ZITADEL_SERVICE_ACCOUNT_PAT not set — custom login (/api/v1/auth/login) will respond 503")
	}
	if c.StripeSecretKey == "" {
		log.Println("config: STRIPE_SECRET_KEY not set — Stripe checkout disabled")
	}
	if c.AlipayAppID == "" {
		log.Println("config: ALIPAY_APP_ID not set — Alipay direct disabled")
	}
	if c.WechatPayMchID == "" {
		log.Println("config: WECHAT_PAY_MCH_ID not set — WeChat Pay direct disabled")
	}
	if c.TemporalHostPort == "" {
		log.Println("config: TEMPORAL_HOST_PORT not set — async workflows run on the direct path (no durability)")
	}
	if c.SMSProvider == "" {
		log.Println("config: SMS_PROVIDER not set — SMS verification codes disabled")
	}
	return nil
}

// decodedLen returns the byte length of s after tolerant base64 decoding.
// Accepts both standard and URL-safe alphabets with optional padding.
// Falls back to raw length if s is not valid base64.
func decodedLen(s string) int {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return len(b)
		}
	}
	return len(s)
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseInt(key string, defaultVal int) int {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

func parseFloat(key string, defaultVal float64) float64 {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

func parseDuration(key string, defaultVal time.Duration) time.Duration {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}
