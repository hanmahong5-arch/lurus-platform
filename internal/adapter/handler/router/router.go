// Package router wires all HTTP routes for lurus-platform.
package router

import (
	"log/slog"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/ratelimit"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/readiness"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/slogctx"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tenant"
)

// Deps holds all handler dependencies injected at startup.
type Deps struct {
	Accounts        *handler.AccountHandler
	Subscriptions   *handler.SubscriptionHandler
	Wallets         *handler.WalletHandler
	Products        *handler.ProductHandler
	Internal        *handler.InternalHandler
	Webhooks        *handler.WebhookHandler
	Invoices        *handler.InvoiceHandler
	Refunds         *handler.RefundHandler
	AdminOps        *handler.AdminOpsHandler
	Reports         *handler.ReportHandler
	AdminConfig     *handler.AdminConfigHandler
	WechatAuth      *handler.WechatAuthHandler      // nil when WeChat login is not configured
	WechatOAuth     *handler.WechatOAuthHandler     // nil when WeChat OAuth2 adapter is not configured
	ZLogin          *handler.ZLoginHandler          // nil when custom OIDC login is not configured
	Registration    *handler.RegistrationHandler    // nil when registration is not configured
	Checkin         *handler.CheckinHandler         // daily check-in
	Organizations   *handler.OrganizationHandler    // organization management
	QRLogin         *handler.QRLoginHandler         // v1 QR login (login-only, legacy)
	QR              *handler.QRHandler              // v2 multi-action QR primitive (login → join_org/delegate pending)
	NewAPIProxy     *handler.NewAPIProxyHandler     // nil when newapi proxy is not configured
	ServiceKeyAdmin *handler.AdminServiceKeyHandler // nil when service key management not wired
	InternalKey     string                          // legacy INTERNAL_API_KEY (fallback during migration)
	ServiceKeys     *app.ServiceKeyStore            // scoped service key resolver (nil = legacy-only mode)
	JWT             *auth.JWTMiddleware
	RateLimit       *ratelimit.Limiter
	ExtraMiddleware []gin.HandlerFunc // metrics, tracing, etc. (applied before routes)

	// TrustedProxyCIDRs restricts which X-Forwarded-For headers Gin's
	// c.ClientIP() will honour. Empty slice = trust nothing (safe default
	// only when no reverse-proxy sits in front). Ingresses are usually
	// in cluster private ranges.
	TrustedProxyCIDRs []string

	// Readiness is the pluggable readiness probe set. nil = /readyz is
	// wired with an empty set (always 200), which matches the pre-PT7.4
	// behaviour of a naked /health probe.
	Readiness *readiness.Set
}

// Build constructs and returns the root Gin engine.
func Build(deps Deps) *gin.Engine {
	r := gin.New()

	// Configure which upstream proxies may set X-Forwarded-For. Without this
	// Gin trusts every client's self-declared X-Forwarded-For, which makes
	// ratelimit.PerIP trivially bypassable and pollutes audit logs with
	// attacker-chosen addresses. Empty CIDR list ⇒ trust nothing (direct
	// connections only).
	if len(deps.TrustedProxyCIDRs) == 0 {
		_ = r.SetTrustedProxies(nil)
	} else {
		_ = r.SetTrustedProxies(deps.TrustedProxyCIDRs)
	}

	r.Use(slogctx.Middleware()) // Assign request_id early for log correlation.
	r.Use(gin.Logger())
	r.Use(gin.Recovery())                                          // Catch panics, return 500 instead of crash.
	r.Use(handler.MaxBodySize(handler.DefaultMaxRequestBodyBytes)) // Reject >2 MB request bodies (413).
	r.Use(handler.RequestTimeout(handler.DefaultRequestTimeout))   // Cancel stuck requests after 30s (504).
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"https://admin.lurus.cn", "https://identity.lurus.cn", "https://auth.lurus.cn", "https://lucrum.lurus.cn", "https://www.lurus.cn"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Request-ID", "X-Idempotency-Key"},
		ExposeHeaders:    []string{"X-Request-ID", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           12 * 3600, // 12 hours preflight cache
	}))

	// Apply caller-provided middleware (Prometheus metrics, OTel tracing) before routes.
	for _, mw := range deps.ExtraMiddleware {
		r.Use(mw)
	}

	// Health check — unauthenticated liveness probe. Answers only "the
	// process is up and serving". Does NOT verify dependencies — that's
	// /readyz below.
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "lurus-platform"})
	})

	// Readiness probe — unauthenticated. Distinct from /health so that a
	// dependency outage pulls the pod out of the Service endpoints
	// (503 → NotReady) without also flapping liveness (which would
	// trigger a pod restart). See internal/pkg/readiness for the set.
	readinessSet := deps.Readiness
	if readinessSet == nil {
		readinessSet = readiness.NewSet() // empty set = always ready
	}
	r.GET("/readyz", readinessSet.HTTPHandler())

	// WeChat OAuth routes — no JWT auth (handles the browser redirect dance).
	if deps.WechatAuth != nil {
		r.GET("/api/v1/auth/wechat", deps.WechatAuth.Initiate)
		r.GET("/api/v1/auth/wechat/callback", deps.WechatAuth.Callback)
	}

	// Custom OIDC login UI routes — no JWT auth (called by the unauthenticated /zlogin page).
	if deps.ZLogin != nil {
		r.POST("/api/v1/auth/login", deps.ZLogin.DirectLogin)
		r.GET("/api/v1/auth/info", deps.ZLogin.GetAuthInfo)
		r.POST("/api/v1/auth/zlogin/password", deps.ZLogin.SubmitPassword)
		r.POST("/api/v1/auth/wechat/link-oidc", deps.ZLogin.LinkWechatAndComplete)
	}

	// Registration & password reset — unauthenticated
	if deps.Registration != nil {
		r.POST("/api/v1/auth/register", deps.Registration.Register)
		r.POST("/api/v1/auth/forgot-password", deps.Registration.ForgotPassword)
		r.POST("/api/v1/auth/reset-password", deps.Registration.ResetPassword)
		r.POST("/api/v1/auth/send-sms", deps.Registration.SendSMSCode)
		// Pre-submit validation (inline form feedback before the user submits).
		r.POST("/api/v1/auth/check-username", deps.Registration.CheckUsername)
		r.POST("/api/v1/auth/check-email", deps.Registration.CheckEmail)
	}

	// WeChat OAuth2 adapter — exposes a standard OAuth2 server wrapping WeChat's proprietary flow.
	// Zitadel registers this as a Generic OAuth IDP; the login-ui auto-shows a WeChat button.
	if deps.WechatOAuth != nil {
		r.GET("/oauth/wechat/authorize", deps.WechatOAuth.Authorize)
		r.GET("/oauth/wechat/callback", deps.WechatOAuth.Callback)
		r.POST("/oauth/wechat/token", deps.WechatOAuth.Token)
		r.GET("/oauth/wechat/userinfo", deps.WechatOAuth.UserInfo)
	}

	// QR login — public endpoints (create session, poll status).
	if deps.QRLogin != nil {
		qrPublic := r.Group("/api/v1/public/qr-login")
		if deps.RateLimit != nil {
			qrPublic.Use(deps.RateLimit.PerIP())
		}
		qrPublic.POST("/session", deps.QRLogin.CreateSession)
		qrPublic.GET("/:id/status", deps.QRLogin.PollStatus)
	}

	// QR v2 primitive — public endpoints (create/status). Confirm is auth'd below.
	if deps.QR != nil {
		qrV2Public := r.Group("/api/v2/qr")
		if deps.RateLimit != nil {
			qrV2Public.Use(deps.RateLimit.PerIP())
		}
		qrV2Public.POST("/session", deps.QR.CreateSession)
		qrV2Public.GET("/:id/status", deps.QR.PollStatus)
	}

	// Public QR code endpoint — unauthenticated, read-only.
	if deps.AdminConfig != nil {
		r.GET("/api/v1/public/qrcode/:type", deps.AdminConfig.GetPublicQRCode)
	}

	// Public user API — requires Zitadel JWT or lurus session token
	v1 := r.Group("/api/v1")
	v1.Use(deps.JWT.Auth())
	v1.Use(tenant.Middleware()) // Propagate account_id to ctx for RLS-aware repos.
	if deps.RateLimit != nil {
		v1.Use(deps.RateLimit.PerUser())
	}
	{
		// Account
		v1.GET("/account/me", deps.Accounts.GetMe)
		v1.PUT("/account/me", deps.Accounts.UpdateMe)
		v1.GET("/account/me/services", deps.Accounts.GetServices)
		v1.GET("/account/me/overview", deps.Accounts.GetMeOverview)
		v1.GET("/account/me/referral", deps.Accounts.GetMeReferral)

		// Products (read-only, public)
		v1.GET("/products", deps.Products.ListProducts)
		v1.GET("/products/:id/plans", deps.Products.ListPlans)

		// Subscriptions
		v1.GET("/subscriptions", deps.Subscriptions.ListSubscriptions)
		v1.GET("/subscriptions/:product_id", deps.Subscriptions.GetSubscription)
		v1.POST("/subscriptions/checkout", deps.Subscriptions.Checkout)
		v1.POST("/subscriptions/:product_id/cancel", deps.Subscriptions.CancelSubscription)

		// Wallet
		v1.GET("/wallet", deps.Wallets.GetWallet)
		v1.GET("/wallet/transactions", deps.Wallets.ListTransactions)
		v1.POST("/wallet/redeem", deps.Wallets.Redeem)

		// Topup & Orders
		v1.GET("/wallet/topup/info", deps.Wallets.TopupInfo)
		v1.POST("/wallet/topup", deps.Wallets.CreateTopup)
		v1.GET("/wallet/orders", deps.Wallets.ListOrders)
		v1.GET("/wallet/orders/:order_no", deps.Wallets.GetOrder)

		// Invoices
		v1.POST("/invoices", deps.Invoices.GenerateInvoice)
		v1.GET("/invoices", deps.Invoices.ListInvoices)
		v1.GET("/invoices/:invoice_no", deps.Invoices.GetInvoice)

		// Refunds
		v1.POST("/refunds", deps.Refunds.RequestRefund)
		v1.GET("/refunds", deps.Refunds.ListRefunds)
		v1.GET("/refunds/:refund_no", deps.Refunds.GetRefund)

		// Phone verification (requires auth)
		if deps.Registration != nil {
			v1.POST("/account/me/send-phone-code", deps.Registration.SendPhoneCode)
			v1.POST("/account/me/verify-phone", deps.Registration.VerifyPhone)
		}

		// QR login — confirm (requires authenticated APP user)
		if deps.QRLogin != nil {
			v1.POST("/qr-login/:id/confirm", deps.QRLogin.Confirm)
		}

		// Daily check-in
		v1.GET("/checkin/status", deps.Checkin.GetStatus)
		v1.POST("/checkin", deps.Checkin.DoCheckin)

		// Organizations
		v1.POST("/organizations", deps.Organizations.Create)
		v1.GET("/organizations", deps.Organizations.ListMine)
		v1.GET("/organizations/:id", deps.Organizations.Get)
		v1.POST("/organizations/:id/members", deps.Organizations.AddMember)
		v1.DELETE("/organizations/:id/members/:uid", deps.Organizations.RemoveMember)
		v1.GET("/organizations/:id/api-keys", deps.Organizations.ListAPIKeys)
		v1.POST("/organizations/:id/api-keys", deps.Organizations.CreateAPIKey)
		v1.DELETE("/organizations/:id/api-keys/:kid", deps.Organizations.RevokeAPIKey)
		v1.GET("/organizations/:id/wallet", deps.Organizations.GetWallet)
	}

	// QR v2 confirm — under /api/v2 with auth. Sibling group to v1 (different prefix).
	if deps.QR != nil {
		v2 := r.Group("/api/v2")
		v2.Use(deps.JWT.Auth())
		v2.Use(tenant.Middleware())
		if deps.RateLimit != nil {
			v2.Use(deps.RateLimit.PerUser())
		}
		v2.POST("/qr/:id/confirm", deps.QR.Confirm)
		// Authed create — caller identity is required for non-login actions
		// (join_org, delegate). The public POST /qr/session door stays
		// login-only; this sibling door handles the rest.
		v2.POST("/qr/session/authed", deps.QR.CreateSessionAuthed)
	}

	// Internal service-to-service API — scoped bearer token auth.
	// If ServiceKeys store is provided, uses per-service scoped keys.
	// Falls back to legacy INTERNAL_API_KEY for backward compatibility.
	keyStore := deps.ServiceKeys
	if keyStore == nil {
		// Legacy-only mode: create a store with just the fallback key.
		keyStore = app.NewServiceKeyStore(nil, deps.InternalKey)
	}
	internal := r.Group("/internal/v1")
	internal.Use(internalKeyAuth(keyStore))
	{
		internal.GET("/accounts/by-zitadel-sub/:sub", deps.Internal.GetAccountByZitadelSub)
		internal.GET("/accounts/by-id/:id", deps.Internal.GetAccountByID)
		internal.POST("/accounts/upsert", deps.Internal.UpsertAccount)
		internal.GET("/accounts/:id/entitlements/:product_id", deps.Internal.GetEntitlements)
		internal.GET("/accounts/:id/subscription/:product_id", deps.Internal.GetSubscription)
		internal.GET("/accounts/:id/overview", deps.Internal.GetAccountOverview)
		internal.POST("/usage/report", deps.Internal.ReportUsage)
		// Wallet debit for internal service calls (AI quota overage)
		internal.POST("/accounts/:id/wallet/debit", deps.Internal.DebitWallet)
		// Billing summary (balance + frozen + pre-auths + pending orders)
		internal.GET("/accounts/:id/billing-summary", deps.Internal.GetBillingSummary)
		// Cross-service checkout (create order, resolve payment URL)
		internal.POST("/checkout/create", deps.Internal.CreateCheckout)
		internal.GET("/checkout/:order_no/status", deps.Internal.GetCheckoutStatus)
		internal.GET("/payment-methods", deps.Internal.GetPaymentMethods)
		internal.GET("/payment/providers", deps.Internal.GetPaymentProviderStatus)
		// Pre-authorization (freeze/settle/release for streaming API calls)
		internal.POST("/accounts/:id/wallet/pre-authorize", deps.Internal.PreAuthorize)
		internal.POST("/wallet/pre-auth/:id/settle", deps.Internal.SettlePreAuth)
		internal.POST("/wallet/pre-auth/:id/release", deps.Internal.ReleasePreAuth)
		// Lookup by email or phone
		internal.GET("/accounts/by-email/:email", deps.Internal.GetAccountByEmail)
		internal.GET("/accounts/by-phone/:phone", deps.Internal.GetAccountByPhone)
		// Quick wallet balance lookup
		internal.GET("/accounts/:id/wallet/balance", deps.Internal.GetWalletBalance)
		// Session token validation
		internal.POST("/accounts/validate-session", deps.Internal.ValidateSession)
		// Lookup by third-party OAuth binding (e.g. wechat openid)
		internal.GET("/accounts/by-oauth/:provider/:provider_id", deps.Internal.GetAccountByOAuth)
		// Currency exchange (LUC → LUT)
		internal.POST("/accounts/:id/currency/exchange", deps.Internal.ExchangeLucToLut)
		internal.GET("/currency/info", deps.Internal.GetCurrencyInfo)
		// Subscription checkout (service-to-service, e.g. Lucrum/Creator on behalf of user)
		internal.POST("/subscriptions/checkout", deps.Internal.InternalSubscriptionCheckout)
		// Wallet transaction history (service-to-service, e.g. Creator for transaction display)
		internal.POST("/accounts/:id/wallet/transactions", deps.Internal.InternalListWalletTransactions)
		// Resolve org API key to organization (used by lurus-api and other services)
		internal.POST("/orgs/resolve-api-key", deps.Organizations.ResolveAPIKey)
		// User preferences sync (cross-device, e.g. Creator model usage stats)
		internal.POST("/preferences/sync", deps.Internal.SyncPreferences)
		internal.GET("/preferences/:account_id", deps.Internal.GetPreferences)
	}

	// Admin API — requires admin JWT role (Zitadel only, not lurus session tokens)
	admin := r.Group("/admin/v1")
	admin.Use(deps.JWT.AdminAuth())
	{
		admin.GET("/accounts", deps.Accounts.AdminListAccounts)
		admin.GET("/accounts/:id", deps.Accounts.AdminGetAccount)
		admin.POST("/accounts/:id/grant", deps.Accounts.AdminGrantEntitlement)
		admin.POST("/accounts/:id/wallet/adjust", deps.Wallets.AdminAdjustWallet)
		admin.POST("/accounts/:id/wallet/credit", deps.Internal.CreditWallet)

		admin.POST("/products", deps.Products.AdminCreateProduct)
		admin.PUT("/products/:id", deps.Products.AdminUpdateProduct)
		admin.POST("/products/:id/plans", deps.Products.AdminCreatePlan)
		admin.PUT("/plans/:id", deps.Products.AdminUpdatePlan)

		// Admin Invoices
		admin.GET("/invoices", deps.Invoices.AdminList)

		// Admin Refunds
		admin.POST("/refunds/:refund_no/approve", deps.Refunds.AdminApprove)
		admin.POST("/refunds/:refund_no/reject", deps.Refunds.AdminReject)

		// Admin Ops: batch redemption code generation
		admin.POST("/redemption-codes/batch", deps.AdminOps.BatchGenerateCodes)

		// Admin Reports: financial reconciliation
		admin.GET("/reports/financial", deps.Reports.FinancialReport)
		admin.GET("/reconciliation/issues", deps.Wallets.AdminListReconciliationIssues)
		admin.POST("/reconciliation/issues/:id/resolve", deps.Wallets.AdminResolveReconciliationIssue)

		// Admin Settings: runtime payment config + QR code management
		if deps.AdminConfig != nil {
			admin.GET("/settings", deps.AdminConfig.ListSettings)
			admin.PUT("/settings", deps.AdminConfig.UpdateSettings)
			admin.POST("/settings/qrcode", deps.AdminConfig.UploadQRCode)
		}

		// Admin Organizations
		admin.GET("/organizations", deps.Organizations.AdminList)
		admin.PATCH("/organizations/:id", deps.Organizations.AdminUpdateStatus)

		// Service API Key management
		if deps.ServiceKeyAdmin != nil {
			admin.POST("/service-keys", deps.ServiceKeyAdmin.CreateServiceKey)
			admin.GET("/service-keys", deps.ServiceKeyAdmin.ListServiceKeys)
			admin.DELETE("/service-keys/:id", deps.ServiceKeyAdmin.RevokeServiceKey)
		}
	}

	// NewAPI admin proxy — reverse-proxies /proxy/newapi/* to the LLM gateway.
	if deps.NewAPIProxy != nil {
		newapi := r.Group("/proxy/newapi")
		newapi.Use(deps.JWT.AdminAuth())
		newapi.Any("/*path", deps.NewAPIProxy.Handle)
	}

	// Payment provider webhooks — signature-verified per-provider
	webhooks := r.Group("/webhook")
	if deps.RateLimit != nil {
		webhooks.Use(deps.RateLimit.PerIP())
	}
	{
		webhooks.GET("/epay", deps.Webhooks.EpayNotify) // 易支付 uses GET callbacks
		webhooks.POST("/stripe", deps.Webhooks.StripeWebhook)
		webhooks.POST("/creem", deps.Webhooks.CreemWebhook)
		webhooks.POST("/alipay", deps.Webhooks.AlipayNotify)         // Alipay async notification
		webhooks.POST("/wechat", deps.Webhooks.WechatPayNotify)      // WeChat Pay v3 notification
		webhooks.POST("/worldfirst", deps.Webhooks.WorldFirstNotify) // WorldFirst (万里汇) notification
	}

	return r
}

// internalKeyAuth resolves the bearer token to a service identity with scoped permissions.
// If a ServiceKeyStore is provided, it checks against the database-backed key store.
// Falls back to the legacy shared INTERNAL_API_KEY for backward compatibility.
//
// After authentication, the following context values are set:
//   - "service_id"     (string) — the service name (e.g. "lurus-api")
//   - "service_scopes" ([]string) — the allowed scopes
//   - "service_legacy" (bool) — true if using the old shared key
func internalKeyAuth(keyStore *app.ServiceKeyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		bearer := c.GetHeader("Authorization")
		if len(bearer) <= 7 || bearer[:7] != "Bearer " {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized", "message": "Missing or malformed Authorization header",
			})
			return
		}
		rawToken := bearer[7:]

		result := keyStore.Resolve(rawToken)
		if result == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized", "message": "Invalid service API key",
			})
			return
		}

		// Set context for downstream handlers and audit logging.
		c.Set("service_id", result.ServiceName)
		c.Set("service_scopes", result.Scopes)
		c.Set("service_legacy", result.IsLegacy)

		// Audit log: who called what.
		slog.Info("internal_api_call",
			"service", result.ServiceName,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"legacy", result.IsLegacy,
			"request_id", c.GetString("request_id"),
		)

		c.Next()
	}
}
