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
	identitynats "github.com/hanmahong5-arch/lurus-platform/internal/adapter/nats"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
	"github.com/hanmahong5-arch/lurus-platform/internal/module"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/cache"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/email"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/idempotency"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/lurusapi"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
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
	subRepo := repo.NewSubscriptionRepo(db)
	walletRepo := repo.NewWalletRepo(db)
	productRepo := repo.NewProductRepo(db)
	vipRepo := repo.NewVIPRepo(db)
	invoiceRepo := repo.NewInvoiceRepo(db)
	refundRepo := repo.NewRefundRepo(db)
	adminSettingsRepo := repo.NewAdminSettingsRepo(db)
	orgRepo := repo.NewOrganizationRepo(db)

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
	accountSvc := app.NewAccountService(accountRepo, walletRepo, vipRepo)
	// Note: accountSvc.SetOnAccountCreatedHook is wired after registry init (below).
	invoiceSvc := app.NewInvoiceService(invoiceRepo, walletRepo)
	referralSvc := app.NewReferralServiceWithCodes(accountRepo, walletRepo, walletRepo).WithStats(referralRepo).WithRewardEvents(referralRepo)
	orgSvc := app.NewOrganizationService(orgRepo)
	overviewSvc := app.NewOverviewService(accountRepo, vipSvc, walletRepo, subSvc, productRepo, ovCache)
	checkinSvc := app.NewCheckinService(checkinRepo, walletRepo)

	// --- Module Registry (pluggable hooks for notification, mail, etc.) ---
	registry := module.NewRegistry()
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
	publisher, err := identitynats.NewPublisher(nc)
	if err != nil {
		slog.Warn("nats publisher init failed, event publishing disabled", "err", err)
	}
	refundSvc := app.NewRefundService(refundRepo, walletRepo, publisher, nil).WithSubscriptionCanceller(subSvc)

	// --- NATS Consumer (non-fatal) ---
	consumer, err := identitynats.NewConsumer(nc, vipSvc)
	if err != nil {
		slog.Warn("nats consumer init failed, event consumption disabled", "err", err)
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
	zloginH := handler.NewZLoginHandler(accountSvc, accountRepo, cfg.ZitadelIssuer, cfg.ZitadelServiceAccountPAT, cfg.SessionSecret)
	registrationH := handler.NewRegistrationHandler(registrationSvc)
	checkinH := handler.NewCheckinHandler(checkinSvc)
	orgH := handler.NewOrganizationHandler(orgSvc)
	qrLoginH := handler.NewQRLoginHandler(rdb, cfg.SessionSecret)
	// The v2 QR handler fans out to OrganizationService for action=join_org
	// and (best-effort) to the NATS publisher for identity.org.member_joined.
	qrH := handler.NewQRHandlerWithKeyring(rdb, cfg.SessionSecret, cfg.QRSigningKeys).WithOrgService(orgSvc)
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

	// Readiness probe set — wired with the live infra clients so /readyz
	// actively verifies each dependency per request. NATSChecker tolerates
	// a nil conn (when init above failed), so a partially-up pod still
	// reports Postgres/Redis correctly.
	readinessSet := readiness.NewSet(
		readiness.RedisChecker(rdb),
		readiness.PostgresChecker(sqlDB),
		readiness.NATSChecker(nc),
	)

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
		QRLogin:           qrLoginH,
		QR:                qrH,
		NewAPIProxy:       newAPIProxyH,
		InternalKey:       cfg.InternalAPIKey,
		JWT:               jwtMiddleware,
		RateLimit:         rateLimiter,
		TrustedProxyCIDRs: parseCSVList(cfg.TrustedProxiesCIDRs),
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

	// Graceful shutdown trigger
	g.Go(func() error {
		<-gctx.Done()
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
