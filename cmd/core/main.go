package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	natsgo "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	otelgin "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	identitygrpc "github.com/hanmahong5-arch/lurus-platform/internal/adapter/grpc"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler/router"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/kovaprov"
	identitynats "github.com/hanmahong5-arch/lurus-platform/internal/adapter/nats"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	appsms "github.com/hanmahong5-arch/lurus-platform/internal/app/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/module"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/app_registry"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/identity_admin"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/newapi_sync"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/buildinfo"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/cache"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/lurusapi"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/newapi"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/outbox"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/ratelimit"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/readiness"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/slogctx"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/sms"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
	lurustemporal "github.com/hanmahong5-arch/lurus-platform/internal/temporal"
	"github.com/hanmahong5-arch/lurus-platform/internal/temporal/activities"
	lurusweb "github.com/hanmahong5-arch/lurus-platform/web"
)

func main() {
	_ = godotenv.Load()

	// Config validates required env vars — panics if missing (fast-fail on startup)
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// Use JSON log handler in production with context enrichment (trace_id,
	// span_id, account_id, request_id) for log-trace correlation in Loki/Grafana.
	if cfg.Env == "production" {
		jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
		slog.SetDefault(slog.New(slogctx.New(jsonHandler)))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	// Log the build provenance as the very first line so an operator can
	// correlate a crash-looping pod, a trace, or a support ticket with an
	// exact ghcr.io/.../lurus-platform-core:main-<sha7> image.
	bi := buildinfo.Get()
	slog.Info("lurus-platform build",
		"sha", bi.SHA,
		"built_at", bi.BuiltAt,
		"env", cfg.Env,
	)

	// --- OpenTelemetry tracing ---
	tracingShutdown, err := tracing.Init(ctx, cfg.OtelServiceName, cfg.OtelEndpoint)
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(shutCtx)
	}()

	// --- Database ---
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	defer sqlDB.Close()

	// --- Redis ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer rdb.Close()

	// --- NATS ---
	nc, err := natsgo.Connect(cfg.NATSAddr,
		natsgo.RetryOnFailedConnect(true),
		natsgo.MaxReconnects(10),
		natsgo.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	// --- Repositories ---
	accountRepo := repo.NewAccountRepo(db)
	accountPurgeRepo := repo.NewAccountPurgeRepo(db)
	accountDeleteRequestRepo := repo.NewAccountDeleteRequestRepo(db)
	subRepo := repo.NewSubscriptionRepo(db)
	walletRepo := repo.NewWalletRepo(db)
	productRepo := repo.NewProductRepo(db)
	vipRepo := repo.NewVIPRepo(db)
	invoiceRepo := repo.NewInvoiceRepo(db)
	refundRepo := repo.NewRefundRepo(db)
	adminSettingsRepo := repo.NewAdminSettingsRepo(db)
	orgRepo := repo.NewOrganizationRepo(db)
	orgServiceRepo := repo.NewOrgServiceRepo(db)
	hookFailureRepo := repo.NewHookFailureRepo(db)

	// --- Admin Config Service (DB-first payment config override) ---
	adminConfigSvc := app.NewAdminConfigService(adminSettingsRepo)
	if err := adminConfigSvc.Load(ctx); err != nil {
		// Non-fatal: log and continue with env var defaults.
		slog.Warn("admin config load failed, using env defaults", "err", err)
	} else {
		// Overlay non-empty DB values onto cfg before payment provider init.
		adminConfigSvc.ApplyToConfig(cfg)
	}

	// --- Cache ---
	entCache := cache.NewEntitlementCache(rdb, cfg.CacheEntitlementTTL)
	ovCache := cache.NewOverviewCache(rdb, 2*time.Minute)

	// --- Repositories (extended) ---
	referralRepo := repo.NewReferralRepo(db)

	// --- Repositories (checkin) ---
	checkinRepo := repo.NewCheckinRepo(db)

	// --- App Services ---
	vipSvc := app.NewVIPService(vipRepo, walletRepo)
	walletSvc := app.NewWalletService(walletRepo, vipSvc)
	productSvc := app.NewProductService(productRepo)
	entSvc := app.NewEntitlementService(subRepo, productRepo, entCache)
	subSvc := app.NewSubscriptionService(subRepo, productRepo, entSvc, cfg.GracePeriodDays)
	accountSvc := app.NewAccountService(accountRepo, walletRepo, vipRepo).
		WithPurgeStore(accountPurgeRepo)
	// Note: accountSvc.SetOnAccountCreatedHook is wired after registry init (below).
	invoiceSvc := app.NewInvoiceService(invoiceRepo, walletRepo)
	referralSvc := app.NewReferralServiceWithCodes(accountRepo, walletRepo, walletRepo).WithStats(referralRepo).WithRewardEvents(referralRepo)
	orgSvc := app.NewOrganizationService(orgRepo)
	overviewSvc := app.NewOverviewService(accountRepo, vipSvc, walletRepo, subSvc, productRepo, ovCache)
	checkinSvc := app.NewCheckinService(checkinRepo, walletRepo)

	// --- Module Registry (pluggable hooks for notification, mail, etc.) ---
	// P1-9: hook DLQ + retry. Failures land in module.hook_failures and
	// surface in /admin/v1/onboarding-failures with one-click replay.
	registry := module.NewRegistry().
		WithDLQ(hookFailureRepo).
		WithAccountFetcher(accountSvc.GetByID).
		WithMetrics(hookMetricsSink{})
	if cfg.InternalAPIKey != "" {
		notifMod := module.NewNotificationModule(module.NotificationConfig{
			Enabled:    true,
			ServiceURL: "http://lurus-notification.lurus-platform.svc.cluster.local:18900",
			APIKey:     cfg.InternalAPIKey,
		})
		notifMod.Register(registry)
	}

	// Mail module: auto-provision username@lurus.cn mailboxes in Stalwart.
	if stalwartURL := os.Getenv("STALWART_ADMIN_URL"); stalwartURL != "" {
		mailMod := module.NewMailModule(module.MailConfig{
			Enabled:          true,
			StalwartAdminURL: stalwartURL,
			StalwartUser:     getEnvDefault("STALWART_ADMIN_USER", "admin"),
			StalwartPassword: os.Getenv("STALWART_ADMIN_PASSWORD"),
			DefaultQuotaMB:   1024,
			MailDomain:       getEnvDefault("MAIL_DOMAIN", "lurus.cn"),
		})
		mailMod.Register(registry)
	}

	// NewAPI sync module: build NewAPI admin client and register the
	// account-created hook so every new platform account auto-provisions
	// a matching NewAPI user (1:1 mapping by username "lurus_<id>").
	// All three NewAPI env vars must be set; otherwise the module is
	// silently disabled (dev / standalone deployments).
	// See docs/ADR-newapi-billing-sync.md (C.2 step 4c+4d).
	//
	// The reference is captured in newapiSyncMod so the NATS topup
	// consumer (constructed below) can wire its OnTopupCompleted hook.
	// newapiClient is also captured for the readiness probe (soft) so a
	// NewAPI outage shows up at /readyz without flipping ready=false.
	var (
		newapiSyncMod *newapi_sync.Module
		newapiClient  *newapi.Client
	)
	if cfg.NewAPIInternalURL != "" && cfg.NewAPIAdminAccessToken != "" && cfg.NewAPIAdminUserID != "" {
		nc, err := newapi.New(cfg.NewAPIInternalURL, cfg.NewAPIAdminAccessToken, cfg.NewAPIAdminUserID)
		if err != nil {
			slog.Warn("newapi_sync: client init failed, sync disabled", "err", err)
		} else {
			newapiClient = nc
			// Money-path deduper: fail-closed so a Redis outage NAKs
			// JetStream redeliveries instead of letting them double-credit.
			// See docs/平台硬化清单.md P0-1+P0-2 for the rationale.
			topupDedup := idempotency.New(rdb, idempotency.DefaultWebhookTTL).
				WithFailClosed().
				WithKeyPrefix("newapi_sync:topup:seen:")
			newapiSyncMod = newapi_sync.New(newapiClient, accountRepo).WithDeduper(topupDedup)
			if newapiSyncMod != nil {
				newapiSyncMod.Register(registry)
			}
		}
	}

	// Wire check-in hook to fire module events (notifications on streak milestones).
	checkinSvc.SetOnCheckinHook(func(ctx context.Context, accountID int64, streak int) {
		registry.FireCheckin(ctx, accountID, streak)
	})

	// Wire account-created hook for OIDC first-login path (UpsertByZitadelSub → new account).
	accountSvc.SetOnAccountCreatedHook(func(ctx context.Context, account *entity.Account) {
		registry.FireAccountCreated(ctx, account)
	})

	slog.Info("module registry initialized", "hooks", registry.HookCount())

	// --- NATS Publisher (non-fatal: degrade gracefully if NATS unavailable) ---
	// Wrap the raw publisher with the DLQ-monitored outbox so transient NATS
	// outages cause events to be parked in a Redis DLQ list rather than
	// silently dropped. Downstream consumers (qr_handler, refund_service,
	// temporal activities) see an interface-compatible Publisher and need no
	// code change. When NATS init fails we still install the outbox with a
	// nil upstream so all events go straight to the DLQ until NATS recovers.
	natsPub, err := identitynats.NewPublisher(nc)
	if err != nil {
		slog.Warn("nats publisher init failed, events will be parked in DLQ", "err", err)
	}
	var publisher *outbox.DLQPublisher
	if natsPub != nil {
		publisher = outbox.New(natsPub, rdb, outbox.Config{})
	} else {
		publisher = outbox.New(nil, rdb, outbox.Config{})
	}
	refundSvc := app.NewRefundService(refundRepo, walletRepo, publisher, nil).WithSubscriptionCanceller(subSvc)

	// --- NATS Consumer (non-fatal) ---
	consumer, err := identitynats.NewConsumer(nc, vipSvc)
	if err != nil {
		slog.Warn("nats consumer init failed, event consumption disabled", "err", err)
	}
	// Plug newapi_sync into the topup-completed subscription so wallet
	// credit events propagate to NewAPI quota (C.2 step 4d). When the
	// module is nil (env unset) the subscription is simply not opened.
	if consumer != nil && newapiSyncMod != nil {
		consumer = consumer.WithTopupHandler(newapiSyncMod.OnTopupCompleted)
	}

	// --- Payment Providers ---
	epayProvider, err := payment.NewEpayProvider(cfg.EpayPartnerID, cfg.EpayKey, cfg.EpayGatewayURL, cfg.EpayNotifyURL)
	if err != nil {
		return fmt.Errorf("init epay provider: %w", err)
	}
	stripeProvider := payment.NewStripeProvider(cfg.StripeSecretKey, cfg.StripeWebhookSecret, cfg.StripeUSDRate)
	creemProvider, err := payment.NewCreemProvider(cfg.CreemAPIKey, cfg.CreemWebhookSecret)
	if err != nil {
		return fmt.Errorf("init creem provider: %w", err)
	}

	// Alipay direct integration (go-pay).
	alipayProvider, err := payment.NewAlipayProvider(payment.AlipayConfig{
		AppID:                   cfg.AlipayAppID,
		PrivateKey:              cfg.AlipayPrivateKey,
		IsProd:                  cfg.AlipayIsProd,
		NotifyURL:               cfg.AlipayNotifyURL,
		ReturnURL:               cfg.AlipayReturnURL,
		AppPublicCertContent:    readCertContent(cfg.AlipayAppPublicCert),
		AlipayPublicCertContent: readCertContent(cfg.AlipayPublicCert),
		AlipayRootCertContent:   readCertContent(cfg.AlipayRootCert),
	}, payment.TradeTypePC)
	if err != nil {
		return fmt.Errorf("init alipay provider: %w", err)
	}
	if alipayProvider != nil {
		slog.Info("alipay provider enabled (direct integration)")
	}

	// WeChat Pay v3 direct integration (go-pay).
	wechatPayProvider, err := payment.NewWechatPayProvider(payment.WechatPayConfig{
		MchID:      cfg.WechatPayMchID,
		SerialNo:   cfg.WechatPaySerialNo,
		APIv3Key:   cfg.WechatPayAPIv3Key,
		PrivateKey: cfg.WechatPayPrivateKey,
		AppID:      cfg.WechatPayAppID,
		NotifyURL:  cfg.WechatPayNotifyURL,
		IsProd:     true,
	}, payment.TradeTypeNative)
	if err != nil {
		return fmt.Errorf("init wechat pay provider: %w", err)
	}
	if wechatPayProvider != nil {
		slog.Info("wechat pay provider enabled (direct integration)")
	}

	// WorldFirst (万里汇) integration — Alipay+ Cashier Payment.
	worldFirstProvider, err := payment.NewWorldFirstProvider(payment.WorldFirstConfig{
		ClientID:      cfg.WorldFirstClientID,
		PrivateKeyPEM: cfg.WorldFirstPrivateKey,
		PublicKeyPEM:  cfg.WorldFirstPublicKey,
		Gateway:       cfg.WorldFirstGateway,
		NotifyURL:     cfg.WorldFirstNotifyURL,
		KeyVersion:    cfg.WorldFirstKeyVersion,
	})
	if err != nil {
		return fmt.Errorf("init worldfirst provider: %w", err)
	}
	if worldFirstProvider != nil {
		slog.Info("worldfirst provider enabled", "gateway", cfg.WorldFirstGateway)
	}

	// --- Auth Middleware (Zitadel JWKS JWT + lurus session token) ---
	jwtValidator := auth.NewValidator(auth.ValidatorConfig{
		Issuer:     cfg.ZitadelIssuer,
		Audience:   cfg.ZitadelAudience,
		JWKSURL:    cfg.ZitadelJWKSURL,
		JWKSTTL:    time.Hour,
		AdminRoles: []string{cfg.ZitadelAdminRole},
	})
	// AccountLookup: resolve Zitadel claims → lurus account_id (Redis cache → DB upsert on miss).
	accountLookup := buildAccountLookup(rdb, accountSvc)
	// SessionSecret enables HS256 lurus session tokens (WeChat login). Empty = disabled.
	jwtMiddleware := auth.NewJWTMiddleware(jwtValidator, accountLookup, cfg.SessionSecret)

	// --- Rate Limiter ---
	rateLimiter := ratelimit.New(rdb, ratelimit.DefaultConfig(
		cfg.RateLimitIPPerMinute,
		cfg.RateLimitUserPerMinute,
	))

	// --- Webhook Idempotency Deduper ---
	webhookDeduper := idempotency.New(rdb, 24*time.Hour)

	// --- Email Sender (needed by registration service, must be initialized before handlers) ---
	var emailSender email.Sender
	if cfg.EmailSMTPHost != "" {
		emailSender = email.NewSMTPSender(cfg.EmailSMTPHost, cfg.EmailSMTPPort, cfg.EmailSMTPUser, cfg.EmailSMTPPass, cfg.EmailFrom)
	} else {
		emailSender = email.NoopSender{}
	}

	// --- SMS Sender ---
	smsCfg := sms.LoadFromEnv()
	smsSender, err := sms.NewFromConfig(smsCfg)
	if err != nil {
		return fmt.Errorf("init sms sender: %w", err)
	}

	// --- Zitadel Client + Registration Service ---
	zitadelClient := zitadel.NewClient(cfg.ZitadelIssuer, cfg.ZitadelServiceAccountPAT)
	registrationSvc := app.NewRegistrationService(accountRepo, walletRepo, vipRepo, referralSvc, zitadelClient, cfg.SessionSecret, emailSender, smsSender, rdb, smsCfg)
	if registrationSvc != nil {
		registrationSvc.SetOnAccountCreatedHook(func(ctx context.Context, account *entity.Account) {
			registry.FireAccountCreated(ctx, account)
		})
		registrationSvc.SetOnReferralSignupHook(func(ctx context.Context, referrerAccountID int64, referredName string) {
			registry.FireReferralSignup(ctx, referrerAccountID, referredName)
		})
	}

	// --- Payment Provider Registry ---
	paymentRegistry := payment.NewRegistry()
	if epayProvider != nil {
		paymentRegistry.Register("epay", epayProvider)
	}
	if stripeProvider != nil {
		paymentRegistry.Register("stripe", stripeProvider,
			payment.MethodInfo{ID: "stripe", Name: "信用卡 (Stripe)", Provider: "stripe", Type: "redirect"})
	}
	if creemProvider != nil {
		paymentRegistry.Register("creem", creemProvider,
			payment.MethodInfo{ID: "creem", Name: "Creem", Provider: "creem", Type: "redirect"})
	}
	if worldFirstProvider != nil {
		paymentRegistry.Register("worldfirst", worldFirstProvider,
			payment.MethodInfo{ID: "worldfirst", Name: "万里汇 (WorldFirst)", Provider: "worldfirst", Type: "redirect"})
	}
	// Direct Alipay preferred over Epay gateway.
	if alipayProvider != nil {
		paymentRegistry.Register("alipay", alipayProvider,
			payment.MethodInfo{ID: "alipay", Name: "支付宝", Provider: "alipay", Type: "redirect"},
			payment.MethodInfo{ID: "alipay_qr", Name: "支付宝 (扫码)", Provider: "alipay", Type: "qr"},
			payment.MethodInfo{ID: "alipay_wap", Name: "支付宝 (手机)", Provider: "alipay", Type: "redirect"},
		)
	} else if epayProvider != nil {
		paymentRegistry.Register("epay", epayProvider,
			payment.MethodInfo{ID: "epay_alipay", Name: "支付宝", Provider: "epay", Type: "qr"})
	}
	// Direct WeChat Pay preferred over Epay gateway.
	if wechatPayProvider != nil {
		paymentRegistry.Register("wechat", wechatPayProvider,
			payment.MethodInfo{ID: "wechat_native", Name: "微信支付 (扫码)", Provider: "wechat", Type: "qr"},
			payment.MethodInfo{ID: "wechat_h5", Name: "微信支付 (H5)", Provider: "wechat", Type: "redirect"},
		)
	} else if epayProvider != nil {
		paymentRegistry.Register("epay", epayProvider,
			payment.MethodInfo{ID: "epay_wechat", Name: "微信支付", Provider: "epay", Type: "qr"})
	}
	// Fallback routes: when direct provider circuit is open, try epay gateway.
	if epayProvider != nil {
		if alipayProvider != nil {
			paymentRegistry.SetFallback("alipay", "epay", "epay_alipay")
			paymentRegistry.SetFallback("alipay_qr", "epay", "epay_alipay")
			paymentRegistry.SetFallback("alipay_wap", "epay", "epay_alipay")
		}
		if wechatPayProvider != nil {
			paymentRegistry.SetFallback("wechat_native", "epay", "epay_wxpay")
			paymentRegistry.SetFallback("wechat_h5", "epay", "epay_wxpay")
		}
	}
	slog.Info("payment registry initialized", "methods", len(paymentRegistry.ListMethods()))

	// --- HTTP Handlers ---
	accountH := handler.NewAccountHandler(accountSvc, vipSvc, subSvc, overviewSvc, referralSvc)
	subH := handler.NewSubscriptionHandler(subSvc, productSvc, walletSvc, paymentRegistry)
	walletH := handler.NewWalletHandler(walletSvc, paymentRegistry)
	productH := handler.NewProductHandler(productSvc)
	lurusAPIClient := lurusapi.NewClient(cfg.LurusAPIInternalURL, cfg.LurusAPIInternalKey)
	preferenceRepo := repo.NewPreferenceRepo(db)
	internalH := handler.NewInternalHandler(accountSvc, subSvc, entSvc, vipSvc, overviewSvc, walletSvc, referralSvc, cfg.SessionSecret).
		WithPayments(paymentRegistry).
		WithProductService(productSvc).
		WithLurusAPI(lurusAPIClient).
		WithPreferenceRepo(preferenceRepo)
	webhookH := handler.NewWebhookHandler(walletSvc, subSvc, paymentRegistry, webhookDeduper)
	invoiceH := handler.NewInvoiceHandler(invoiceSvc)
	refundH := handler.NewRefundHandler(refundSvc)
	adminOpsH := handler.NewAdminOpsHandler(referralSvc)
	reportH := handler.NewReportHandler(db)
	adminConfigH := handler.NewAdminConfigHandler(adminConfigSvc)
	wechatAuthH := handler.NewWechatAuthHandler(accountSvc, cfg.WechatServerAddress, cfg.WechatServerToken, cfg.SessionSecret)
	wechatOAuthH := handler.NewWechatOAuthHandler(cfg.WechatServerAddress, cfg.WechatServerToken, cfg.WechatOAuthClientSecret, rdb)
	// Cookie parent domain — read from env so dev (localhost) and prod
	// (.lurus.cn) diverge without code changes. Empty = host-only cookie
	// (the safe default; only the issuing host can read it).
	cookieDomain := getEnvDefault("LURUS_COOKIE_DOMAIN", "")
	zloginH := handler.NewZLoginHandler(accountSvc, accountRepo, cfg.ZitadelIssuer, cfg.ZitadelServiceAccountPAT, cfg.SessionSecret).
		WithCookieDomain(cookieDomain)
	registrationH := handler.NewRegistrationHandler(registrationSvc).WithCookieDomain(cookieDomain)
	// Server-side session-token revoke list (P1-5): logout puts the
	// token's hash on the list with TTL = remaining JWT validity, so a
	// stolen Bearer can't be replayed for the rest of its 30-day TTL.
	// Reuses the existing Redis client; no new infra to provision.
	sessionRevoker := auth.NewSessionRevoker(rdb)
	whoamiH := handler.NewWhoamiHandler(accountSvc, cfg.SessionSecret).
		WithRevoker(sessionRevoker)
	// LLM-token handler is wired with the same newapi_sync.Module that
	// powers 4c+4d. nil-safe: when env vars are unset the module is nil
	// and the endpoint returns a clear 503 (not a silent SPA fallback).
	llmTokenH := handler.NewLLMTokenHandler(cfg.SessionSecret, newapiSyncMod)
	checkinH := handler.NewCheckinHandler(checkinSvc)
	orgH := handler.NewOrganizationHandler(orgSvc)

	// --- Kova provisioning bridge (F2 revenue path) ---
	// KOVA_PROVISION_BASE_URL empty → mock-mode client (synthetic admin
	// keys, no R6 round-trip). Set it to e.g. "http://100.122.83.20:9999"
	// once the R6 sidecar is shipped (kova repo follow-up); also set
	// KOVA_PROVISION_API_KEY to the bearer the sidecar expects.
	kovaProvClient := kovaprov.New(
		os.Getenv("KOVA_PROVISION_BASE_URL"),
		os.Getenv("KOVA_PROVISION_API_KEY"),
	)
	kovaProvSvc := app.NewKovaProvisioningService(orgRepo, orgServiceRepo, orgServiceRepo, kovaProvClient)
	kovaProvH := handler.NewKovaProvisioningHandler(kovaProvSvc)
	if kovaProvClient.IsMock() {
		slog.Warn("kova provisioning running in MOCK mode — set KOVA_PROVISION_BASE_URL for live R6 wiring")
	}
	qrLoginH := handler.NewQRLoginHandler(rdb, cfg.SessionSecret)
	// The v2 QR handler fans out to OrganizationService for action=join_org
	// and (best-effort) to the NATS publisher for identity.org.member_joined.
	qrH := handler.NewQRHandlerWithKeyring(rdb, cfg.SessionSecret, cfg.QRSigningKeys).
		WithOrgService(orgSvc).
		WithMaxInflightPolls(cfg.QRMaxInflightPolls)
	if publisher != nil {
		// Avoid the typed-nil-pointer-in-interface trap — only wire when
		// NATS init actually succeeded. Without this check, h.publisher==nil
		// evaluates to false inside the handler and Publish would panic.
		qrH = qrH.WithPublisher(publisher)
	}

	// --- NewAPI Admin Proxy (optional) ---
	var newAPIProxyH *handler.NewAPIProxyHandler
	if cfg.NewAPIInternalURL != "" {
		var proxyErr error
		newAPIProxyH, proxyErr = handler.NewNewAPIProxyHandler(
			cfg.NewAPIInternalURL, cfg.NewAPIAdminAccessToken, cfg.NewAPIAdminUserID)
		if proxyErr != nil {
			return fmt.Errorf("init newapi proxy: %w", proxyErr)
		}
		slog.Info("newapi admin proxy enabled", "target", cfg.NewAPIInternalURL)
	}

	// --- Memorus AI Memory Proxy (optional) ---
	// When MEMORUS_INTERNAL_URL + MEMORUS_API_KEY are both set, exposes
	// /api/v1/memorus/* under user JWT auth. Clients send Lutu JWT;
	// we inject memorus' shared X-API-Key server-side so it never
	// ships in the APP binary.
	var memorusProxyH *handler.MemorusProxyHandler
	if cfg.MemorusInternalURL != "" && cfg.MemorusAPIKey != "" {
		var memorusErr error
		memorusProxyH, memorusErr = handler.NewMemorusProxyHandler(
			cfg.MemorusInternalURL, cfg.MemorusAPIKey)
		if memorusErr != nil {
			return fmt.Errorf("init memorus proxy: %w", memorusErr)
		}
		slog.Info("memorus proxy enabled", "target", cfg.MemorusInternalURL)
	}

	// Readiness probe set — wired with the live infra clients so /readyz
	// actively verifies critical dependencies per request.
	//
	// NATS is intentionally NOT in the set: outbox falls back to a Redis DLQ
	// when NATS is unreachable (see internal/pkg/outbox) and all HTTP
	// handlers that don't publish events stay fully functional, so a NATS
	// outage should NOT remove pods from the Service endpoints. Only
	// hard dependencies (Redis + Postgres — every request hits one or both)
	// belong here.
	readinessSet := readiness.NewSet(
		readiness.RedisChecker(rdb),
		readiness.PostgresChecker(sqlDB),
	).
		// NewAPI is wired SOFT — a NewAPI outage degrades newapi_sync
		// (account creation hooks fail, topup mirroring lags) but the
		// platform's core surfaces (login / wallet / whoami) still work,
		// so flipping /readyz to 503 would do strictly more harm than
		// good. Operators see the soft failure in the response body's
		// `degraded` array and via the lurus_platform_newapi_sync_ops_total
		// `error` counter. See docs/平台硬化清单.md P0-5.
		WithSoftChecker(newapi.NewReadinessChecker(newapiClient))

	// App registry reconciler: built up-front so the AppsAdmin handler
	// can share the instance for manual /rotate-secret calls. The
	// goroutine that runs the periodic loop is spawned later in the
	// errgroup section. nil-safe: when apps.yaml is absent or we're not
	// in a K8s pod the reconciler is left nil and the rotate endpoint
	// returns 503.
	appRegRecon := buildAppRegistryReconciler(rdb, zitadelClient)

	// Wire the QR-delegate destructive flow (Phase 3 / Track 1). All
	// three deps (QRHandler + K8sClient + Tombstones) are required for
	// DeleteRequest to leave its 501 gate; missing any one keeps the
	// endpoint disabled rather than half-wired. Tombstones plug into the
	// reconciler too, so the recreation-suppression survives pod restarts.
	appsAdminH := handler.NewAppsAdminHandler(getEnvDefault("APPS_YAML_PATH", app_registry.ConfigPath), zitadelClient, appRegRecon)
	if appRegRecon != nil {
		// Best-effort K8s client (returns ErrNotInCluster off-cluster) +
		// Redis-backed tombstone store. Both no-op cleanly when nil so
		// the WithDeleteFlow call below is safe to make either way.
		if k8sClient, err := app_registry.NewK8sClient(); err == nil {
			tombstones := app_registry.NewTombstones(rdb)
			appsAdminH = appsAdminH.WithDeleteFlow(qrH, k8sClient, tombstones)
			appRegRecon = appRegRecon.WithTombstones(tombstones)
		}
	}

	// GDPR-grade account purge (Phase 4 / Sprint 1A) — wire the
	// delete_account QR-delegate executor and admin endpoint. The
	// executor is registered as a second QRHandler.WithDelegateExecutor
	// alongside AppsAdmin's delete_oidc_app — multi-executor dispatch
	// resolves by op name. zitadelClient is allowed to be nil; the
	// cascade degrades that step with a warn-level audit instead of
	// failing the whole purge.
	accountDeleteExec := handler.NewAccountDeleteExecutor(accountSvc, subSvc, walletSvc, zitadelClient)
	qrH = qrH.WithDelegateExecutor(accountDeleteExec)
	accountAdminH := handler.NewAccountAdminHandler(accountSvc).WithDeleteFlow(qrH)
	if publisher != nil {
		// Best-effort identity.account.delete_requested emission on
		// every admin-initiated destructive intent. Same nil-safety
		// shape as QRHandler.WithPublisher above.
		accountAdminH = accountAdminH.WithPublisher(publisher)
	}

	// User-self delete-request flow (PIPL §47 / GDPR Art.17). Sibling
	// to the admin QR-delegate flow above; the user submits intent +
	// reason, the row sits in a 30-day cooling-off window, and the
	// AccountPurgeWorker (Sprint 1B follow-up, opt-in via
	// CRON_PURGE_ENABLED) dispatches the same cascade reusing
	// accountDeleteExec. Subscription guard returns 409 if the user
	// still has an active or grace-period subscription.
	accountDeleteReqSvc := app.NewAccountDeleteRequestService(accountDeleteRequestRepo, accountSvc).
		WithSubscriptionGuard(subSvc)
	accountSelfDeleteH := handler.NewAccountSelfDeleteHandler(accountDeleteReqSvc)
	if publisher != nil {
		accountSelfDeleteH = accountSelfDeleteH.WithPublisher(publisher)
	}

	// Refund QR-approve flow (Phase 4 / Sprint 3A) — large refunds
	// route through a boss biometric scan instead of direct admin
	// approval. The executor satisfies the same QRDelegateExecutor
	// + ops.DelegateOp pair as the other delegate ops, so adding
	// this op required no changes to qr_handler dispatch nor to
	// the catalogue endpoint — proves the abstraction.
	refundApproveExec := handler.NewRefundApproveExecutor(refundSvc)
	qrH = qrH.WithDelegateExecutor(refundApproveExec)
	refundH = refundH.WithQRApprove(qrH)

	// Privileged-op catalogue (Phase 4 / Sprint 2). One registry per
	// process — populated here at boot, served read-only by
	// OpsCatalogHandler at /admin/v1/ops, consumed by the Lutu APP
	// confirm screen and the future audit dashboard. Registration
	// is intentionally MustRegister: a duplicate Type() or unknown
	// RiskLevel is a deployer mistake, not a runtime condition.
	opsRegistry := ops.NewRegistry()
	opsRegistry.MustRegister(accountDeleteExec)
	opsRegistry.MustRegister(appsAdminH)
	opsRegistry.MustRegister(refundApproveExec)
	// rotate_secret is a direct admin action (not QR-delegate) but
	// belongs in the catalogue so audit dashboards and operator UIs
	// see the full surface of privileged ops the platform exposes.
	// Registered as ops.Info — no executor wired here because the
	// existing AppsAdminHandler.RotateSecret handles dispatch via
	// admin JWT, not the ops registry.
	opsRegistry.MustRegister(ops.Info{
		OpType:        "rotate_secret",
		OpDescription: "Rotate an OIDC client_secret (direct admin action, no APP confirmation required)",
		OpRisk:        ops.RiskWarn,
		OpDestructive: false,
	})

	// Lurus API key abstraction over Zitadel (Service User + PAT).
	// The whole point of identity_admin module is to keep operators
	// out of Zitadel console; the Web Admin "应用密钥" tab consumes
	// the /admin/v1/api-keys endpoints. Wired only when Zitadel is
	// configured (without it Service.Create has nothing to call).
	var apiKeysAdminH *handler.APIKeysAdminHandler
	if zitadelClient != nil {
		apiKeyRepo := repo.NewAPIKeyRepo(db)
		apiKeySvc := identity_admin.NewService(apiKeyRepo, zitadelClient, slog.Default())
		apiKeysAdminH = handler.NewAPIKeysAdminHandler(apiKeySvc)
		// AppsAdminHandler already registered as "delete_oidc_app" via
		// its DelegateOp methods; the api-key handler exposes ops
		// metadata for "create_api_key" alone (handler.Type returns
		// that) and we register rotate/revoke as ops.Info siblings so
		// the catalogue lists all three to admin UIs.
		opsRegistry.MustRegister(apiKeysAdminH)
		opsRegistry.MustRegister(ops.Info{
			OpType:        "rotate_api_key",
			OpDescription: "Rotate the PAT behind a Lurus API key (revoke old, mint new) — direct admin action",
			OpRisk:        ops.RiskWarn,
			OpDestructive: false,
		})
		opsRegistry.MustRegister(ops.Info{
			OpType:        "revoke_api_key",
			OpDescription: "Revoke a Lurus API key (delete the underlying Zitadel Service User) — direct admin action",
			OpRisk:        ops.RiskDestructive,
			OpDestructive: true,
		})
	}

	opsCatalogH := handler.NewOpsCatalogHandler(opsRegistry)
	onboardingFailH := handler.NewOnboardingFailureHandler(hookFailureRepo, registry)

	// --- SMS Relay Handler (Zitadel webhook → Aliyun SMS) ---
	// Active only when SMS_PROVIDER is configured. When noop, the endpoint is
	// still wired but messages are logged and discarded (useful for staging).
	smsRelayUC := appsms.NewSMSRelayUsecase(
		smsSender,
		smsCfg.AliyunSignName,
		smsCfg.AliyunTemplateVerify,
		smsCfg.AliyunTemplateReset,
		3, // maxRetries
	)
	smsRelayH := handler.NewSMSRelayHandler(smsRelayUC)

	engine := router.Build(router.Deps{
		Accounts:          accountH,
		Subscriptions:     subH,
		Wallets:           walletH,
		Products:          productH,
		Internal:          internalH,
		Webhooks:          webhookH,
		Invoices:          invoiceH,
		Refunds:           refundH,
		AdminOps:          adminOpsH,
		Reports:           reportH,
		AdminConfig:       adminConfigH,
		WechatAuth:        wechatAuthH,
		WechatOAuth:       wechatOAuthH,
		ZLogin:            zloginH,
		Registration:      registrationH,
		Checkin:           checkinH,
		Organizations:     orgH,
		KovaProvisioning:  kovaProvH,
		QRLogin:           qrLoginH,
		QR:                qrH,
		AppsAdmin:         appsAdminH,
		AccountAdmin:      accountAdminH,
		AccountSelfDelete: accountSelfDeleteH,
		OpsCatalog:        opsCatalogH,
		OnboardingFailure: onboardingFailH,
		APIKeysAdmin:      apiKeysAdminH,
		Whoami:            whoamiH,
		LLMToken:          llmTokenH,
		CookieDomain:      cookieDomain,
		NewAPIProxy:       newAPIProxyH,
		MemorusProxy:      memorusProxyH,
		SMSRelay:          smsRelayH,
		InternalKey:       cfg.InternalAPIKey,
		JWT:               jwtMiddleware,
		RateLimit:         rateLimiter,
		TrustedProxyCIDRs: parseCSVList(cfg.TrustedProxiesCIDRs),
		CORSOrigins:       parseCSVList(cfg.CORSAllowedOrigins),
		SessionSecret:     cfg.SessionSecret,
		SessionRevoker:    sessionRevoker,
		Readiness:         readinessSet,
		ExtraMiddleware: []gin.HandlerFunc{
			metrics.HTTPMiddleware(),
			otelgin.Middleware(cfg.OtelServiceName),
		},
	})

	// Prometheus /metrics endpoint (unauthenticated, scraped internally by Prometheus).
	engine.GET("/metrics", gin.WrapH(metrics.Handler()))

	// --- SPA static files (web/dist embedded) ---
	webFS, err := fs.Sub(lurusweb.Dist, "dist")
	if err != nil {
		return fmt.Errorf("embed web/dist: %w", err)
	}
	engine.NoRoute(handler.NoRouteHandler(webFS))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Temporal Worker (required — all subscription workflows run via Temporal) ---
	temporalClient, err := lurustemporal.NewClient(cfg.TemporalHostPort, cfg.TemporalNamespace)
	if err != nil {
		return fmt.Errorf("temporal client: %w", err)
	}
	var temporalWorker *lurustemporal.Worker
	if temporalClient != nil {
		defer temporalClient.Close()
		temporalWorker = lurustemporal.NewWorker(temporalClient, lurustemporal.WorkerDeps{
			SubActivities:    &activities.SubscriptionActivities{Subs: subSvc},
			WalletActivities: &activities.WalletActivities{Wallets: walletSvc},
			EventActivities:  &activities.EventActivities{Publisher: publisher},
			QueryActivities:  &activities.QueryActivities{Plans: productRepo},
			NotificationActivities: &activities.NotificationActivities{
				Mailer:   emailSender,
				Accounts: accountSvc,
			},
		})
		webhookH.WithTemporalClient(temporalClient)
	}

	g, gctx := errgroup.WithContext(ctx)

	// HTTP server
	g.Go(func() error {
		slog.Info("lurus-platform starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	// gRPC server (dual-protocol: HTTP + gRPC)
	grpcSrv := identitygrpc.NewServer(identitygrpc.Deps{
		Accounts:     accountSvc,
		Entitlements: entSvc,
		Overview:     overviewSvc,
		VIP:          vipSvc,
		Wallet:       walletSvc,
		Referral:     referralSvc,
		InternalKey:  cfg.InternalAPIKey,
	})
	g.Go(func() error {
		return grpcSrv.ListenAndServe(gctx, cfg.GRPCPort)
	})

	// NATS consumer (skip if init failed)
	if consumer != nil {
		g.Go(func() error {
			return consumer.Run(gctx)
		})
	}

	// Temporal worker (all subscription workflows: renewal, lifecycle, payment completion)
	if temporalWorker != nil {
		g.Go(func() error {
			return temporalWorker.Run(gctx)
		})
	}

	// Reconciliation worker: periodic cleanup of stale payment orders and pre-auths.
	// Runs every 5 minutes as a lightweight complement to the hourly Temporal ExpiryScanner.
	reconciler := app.NewReconciliationWorker(walletSvc, paymentRegistry)
	reconciler.SetOnAlertHook(func(ctx context.Context, issue *entity.ReconciliationIssue) {
		registry.FireReconciliationIssue(ctx, issue)
	})
	g.Go(func() error {
		reconciler.Start(gctx)
		return nil
	})

	// App registry reconciler: converges apps.yaml → Zitadel OIDC apps +
	// K8s Secrets. The instance was built earlier (so AppsAdmin can share
	// it for manual /rotate-secret); here we just spawn the periodic loop.
	if appRegRecon != nil {
		g.Go(func() error {
			appRegRecon.Run(gctx)
			return nil
		})
	}

	// newapi_sync reconcile cron — backfill orphan accounts that missed
	// the OnAccountCreated hook (NewAPI down at signup, hook crashed
	// mid-flight). Ticks every 5 minutes by default; idempotent retry
	// via OnAccountCreated's find-then-create. See P1-4 in the
	// hardening list and internal/module/newapi_sync/reconcile.go.
	// Disabled (loop blocks on ctx) when newapiSyncMod is nil — same
	// nil-safety as the rest of the integration.
	if newapiSyncMod != nil {
		g.Go(func() error {
			return newapiSyncMod.RunReconcileLoop(gctx, newapi_sync.DefaultReconcileInterval, newapi_sync.DefaultReconcileBatch)
		})
	}

	// Account purge cron worker (Sprint 1B follow-up). Drains
	// expired pending rows from identity.account_delete_requests by
	// invoking the existing AccountDeleteExecutor cascade under a
	// "approved by self / cron" attribution. Opt-in via
	// CRON_PURGE_ENABLED — when false the worker logs once and
	// returns immediately so this code path lands without changing
	// behavior. callerID=0 is the synthetic "automation" caller; the
	// audit row records the requesting user via RequestedBy.
	purgeWorker := app.NewAccountPurgeWorker(
		accountDeleteRequestRepo,
		cronPurgeCascadeAdapter{exec: accountDeleteExec},
		app.AccountPurgeWorkerConfig{
			Interval: cfg.CronPurgeInterval,
			Batch:    cfg.CronPurgeBatch,
			Enabled:  cfg.CronPurgeEnabled,
		},
	)
	g.Go(func() error { return purgeWorker.Run(gctx) })

	// Hook DLQ depth sampler — refreshes the hook_dlq_pending gauge
	// every 30s so alerts fire on fresh data even when no admin is
	// browsing /admin/v1/onboarding-failures (P1-9 polish).
	dlqDepthWorker := app.NewHookDLQDepthWorker(
		hookFailureRepo,
		hookMetricsSink{},
		30*time.Second,
	)
	g.Go(func() error { return dlqDepthWorker.Run(gctx) })

	// Graceful shutdown trigger. The grace window must exceed the 30s QR
	// long-poll cap (see qrMaxPollWait) so in-flight long polls can return
	// naturally instead of being severed mid-flight. gRPC shutdown piggybacks
	// on gctx cancellation inside identitygrpc.Server.ListenAndServe.
	g.Go(func() error {
		<-gctx.Done()
		slog.Info("http: draining long-poll connections before shutdown",
			"timeout", cfg.ShutdownTimeout,
			"long_poll_cap", "30s",
		)
		shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutCtx)
	})

	return g.Wait()
}

// buildAccountLookup creates an AccountLookup function that caches Zitadel sub → account_id
// in Redis (TTL 10min) to avoid a DB round-trip on every authenticated request.
// On first login (DB miss), the account is auto-created via UpsertByZitadelSub so that
// a valid Zitadel JWT never produces a 401 due to a missing local record.
func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseCSVList splits a comma-separated list, trimming whitespace and dropping
// empty entries. Returns nil when the input is empty so callers can detect
// "no value configured" separately from "explicit empty list".
func parseCSVList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// readCertContent reads a certificate file from the given path.
// Returns nil if the path is empty (cert not configured).
// The value can be a file path or raw PEM content.
func readCertContent(pathOrContent string) []byte {
	if pathOrContent == "" {
		return nil
	}
	// If it looks like PEM content (starts with -----), use directly.
	if len(pathOrContent) > 10 && pathOrContent[:5] == "-----" {
		return []byte(pathOrContent)
	}
	// Otherwise treat as file path.
	data, err := os.ReadFile(pathOrContent)
	if err != nil {
		slog.Warn("failed to read cert file, treating as raw content", "path", pathOrContent, "err", err)
		return []byte(pathOrContent)
	}
	return data
}

func buildAccountLookup(rdb *redis.Client, accountSvc *app.AccountService) auth.AccountLookup {
	const subCacheTTL = 10 * time.Minute

	return func(ctx context.Context, claims *auth.Claims) (int64, error) {
		key := "sub:id:" + claims.Sub

		// Fast path: Redis cache.
		val, err := rdb.Get(ctx, key).Int64()
		if err == nil {
			return val, nil
		}

		// Slow path: DB lookup → auto-upsert on miss so first-time logins succeed.
		account, err := accountSvc.UpsertByZitadelSub(ctx, claims.Sub, claims.Email, claims.Name, "")
		if err != nil {
			return 0, fmt.Errorf("account upsert: %w", err)
		}

		// Cache the resolved account_id.
		_ = rdb.Set(ctx, key, account.ID, subCacheTTL).Err()

		return account.ID, nil
	}
}

// buildAppRegistryReconciler constructs the app_registry reconciler from
// apps.yaml + the in-cluster ServiceAccount + Redis. Returns nil — and
// logs a single explanatory line — for any of the well-known "not
// configured" reasons (file missing, not in a K8s pod, Zitadel PAT
// unset). Any nil return short-circuits both the periodic loop *and*
// the manual /rotate-secret endpoint, since the latter cannot do its
// job without the same dependencies.
func buildAppRegistryReconciler(rdb *redis.Client, zitadelClient *zitadel.Client) *app_registry.Reconciler {
	configPath := getEnvDefault("APPS_YAML_PATH", app_registry.ConfigPath)
	spec, err := app_registry.LoadSpec(configPath)
	if err != nil {
		if os.IsNotExist(errors.Unwrap(err)) {
			slog.Info("app_registry: apps.yaml not present, reconciler disabled", "path", configPath)
			return nil
		}
		slog.Warn("app_registry: load spec failed, reconciler disabled", "err", err)
		return nil
	}
	k8sClient, err := app_registry.NewK8sClient()
	if err != nil {
		if errors.Is(err, app_registry.ErrNotInCluster) {
			slog.Info("app_registry: not in a K8s pod, reconciler disabled")
		} else {
			slog.Warn("app_registry: k8s client init failed, reconciler disabled", "err", err)
		}
		return nil
	}
	rotation := app_registry.NewRotationState(rdb)
	recon, err := app_registry.NewReconciler(spec, zitadelClient, k8sClient, rotation, app_registry.Options{})
	if err != nil {
		slog.Warn("app_registry: construct failed, reconciler disabled", "err", err)
		return nil
	}
	return recon
}

// hookMetricsSink adapts metrics package functions to the
// module.HookMetricsSink interface (P1-9). Stateless — methods just
// forward to package-level metric vars.
type hookMetricsSink struct{}

// RecordHookOutcome forwards to the hook_outcomes_total counter.
func (hookMetricsSink) RecordHookOutcome(event, hook, result string) {
	metrics.RecordHookOutcome(event, hook, result)
}

// SetDLQDepth forwards to the hook_dlq_pending gauge.
func (hookMetricsSink) SetDLQDepth(depth int64) {
	metrics.SetHookDLQDepth(depth)
}

// cronPurgeCascadeAdapter bridges app.AccountPurgeWorker (which knows
// nothing about handler types) to handler.AccountDeleteExecutor. The
// worker calls PurgeAccount(ctx, accountID); the adapter wraps that
// into the QRDelegateParams shape the executor expects, with
// callerID=0 marking the call as automation-driven (the executor's
// audit row attributes the action to the row's RequestedBy, not this
// synthetic caller). Kept as a type rather than a closure so the nil-
// guard inside the worker can detect a missing dependency at boot.
type cronPurgeCascadeAdapter struct {
	exec *handler.AccountDeleteExecutor
}

// PurgeAccount invokes the cascade for the supplied account.
// callerID=0 is intentional — the row's own RequestedBy is the
// audit-attribution field for the user-self flow; the cron is a
// transport, not an approver.
func (a cronPurgeCascadeAdapter) PurgeAccount(ctx context.Context, accountID int64) error {
	return a.exec.ExecuteDelegate(ctx, handler.QRDelegateParams{
		Op:        handler.QRDelegateOpDeleteAccount(),
		AccountID: accountID,
	}, 0)
}
