// Package router wires all HTTP routes for lurus-platform.
package router

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/ratelimit"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/slogctx"
)

// Deps holds all handler dependencies injected at startup.
type Deps struct {
	Accounts      *handler.AccountHandler
	Subscriptions *handler.SubscriptionHandler
	Wallets       *handler.WalletHandler
	Products      *handler.ProductHandler
	Internal      *handler.InternalHandler
	Webhooks      *handler.WebhookHandler
	Invoices      *handler.InvoiceHandler
	Refunds       *handler.RefundHandler
	AdminOps      *handler.AdminOpsHandler
	Reports       *handler.ReportHandler
	AdminConfig   *handler.AdminConfigHandler
	WechatAuth    *handler.WechatAuthHandler        // nil when WeChat login is not configured
	WechatOAuth   *handler.WechatOAuthHandler       // nil when WeChat OAuth2 adapter is not configured
	ZLogin        *handler.ZLoginHandler            // nil when custom OIDC login is not configured
	Registration  *handler.RegistrationHandler      // nil when registration is not configured
	Checkin       *handler.CheckinHandler           // daily check-in
	Organizations *handler.OrganizationHandler      // organization management
	NewAPIProxy   *handler.NewAPIProxyHandler       // nil when newapi proxy is not configured
	InternalKey   string                            // secret for /internal/* bearer auth
	JWT           *auth.JWTMiddleware
	RateLimit     *ratelimit.Limiter
	ExtraMiddleware []gin.HandlerFunc               // metrics, tracing, etc. (applied before routes)
}

// Build constructs and returns the root Gin engine.
func Build(deps Deps) *gin.Engine {
	r := gin.New()
	r.Use(slogctx.Middleware()) // Assign request_id early for log correlation.
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(cors.Default())

	// Apply caller-provided middleware (Prometheus metrics, OTel tracing) before routes.
	for _, mw := range deps.ExtraMiddleware {
		r.Use(mw)
	}

	// Health check — unauthenticated
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "lurus-platform"})
	})

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
	}

	// WeChat OAuth2 adapter — exposes a standard OAuth2 server wrapping WeChat's proprietary flow.
	// Zitadel registers this as a Generic OAuth IDP; the login-ui auto-shows a WeChat button.
	if deps.WechatOAuth != nil {
		r.GET("/oauth/wechat/authorize", deps.WechatOAuth.Authorize)
		r.GET("/oauth/wechat/callback", deps.WechatOAuth.Callback)
		r.POST("/oauth/wechat/token", deps.WechatOAuth.Token)
		r.GET("/oauth/wechat/userinfo", deps.WechatOAuth.UserInfo)
	}

	// Public QR code endpoint — unauthenticated, read-only.
	if deps.AdminConfig != nil {
		r.GET("/api/v1/public/qrcode/:type", deps.AdminConfig.GetPublicQRCode)
	}

	// Public user API — requires Zitadel JWT or lurus session token
	v1 := r.Group("/api/v1")
	v1.Use(deps.JWT.Auth())
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

	// Internal service-to-service API — bearer token auth
	internal := r.Group("/internal/v1")
	internal.Use(internalKeyAuth(deps.InternalKey))
	{
		internal.GET("/accounts/by-zitadel-sub/:sub", deps.Internal.GetAccountByZitadelSub)
		internal.POST("/accounts/upsert", deps.Internal.UpsertAccount)
		internal.GET("/accounts/:id/entitlements/:product_id", deps.Internal.GetEntitlements)
		internal.GET("/accounts/:id/subscription/:product_id", deps.Internal.GetSubscription)
		internal.GET("/accounts/:id/overview", deps.Internal.GetAccountOverview)
		internal.POST("/usage/report", deps.Internal.ReportUsage)
		// Wallet debit/credit for internal service calls (AI quota overage, marketplace revenue)
		internal.POST("/accounts/:id/wallet/debit", deps.Internal.DebitWallet)
		internal.POST("/accounts/:id/wallet/credit", deps.Internal.CreditWallet)
		// Lookup by email or phone
		internal.GET("/accounts/by-email/:email", deps.Internal.GetAccountByEmail)
		internal.GET("/accounts/by-phone/:phone", deps.Internal.GetAccountByPhone)
		// Quick wallet balance lookup
		internal.GET("/accounts/:id/wallet/balance", deps.Internal.GetWalletBalance)
		// Session token validation
		internal.POST("/accounts/validate-session", deps.Internal.ValidateSession)
		// Lookup by third-party OAuth binding (e.g. wechat openid)
		internal.GET("/accounts/by-oauth/:provider/:provider_id", deps.Internal.GetAccountByOAuth)
		// Resolve org API key to organization (used by lurus-api and other services)
		internal.POST("/orgs/resolve-api-key", deps.Organizations.ResolveAPIKey)
	}

	// Admin API — requires admin JWT role (Zitadel only, not lurus session tokens)
	admin := r.Group("/admin/v1")
	admin.Use(deps.JWT.AdminAuth())
	{
		admin.GET("/accounts", deps.Accounts.AdminListAccounts)
		admin.GET("/accounts/:id", deps.Accounts.AdminGetAccount)
		admin.POST("/accounts/:id/grant", deps.Accounts.AdminGrantEntitlement)
		admin.POST("/accounts/:id/wallet/adjust", deps.Wallets.AdminAdjustWallet)

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

		// Admin Settings: runtime payment config + QR code management
		if deps.AdminConfig != nil {
			admin.GET("/settings", deps.AdminConfig.ListSettings)
			admin.PUT("/settings", deps.AdminConfig.UpdateSettings)
			admin.POST("/settings/qrcode", deps.AdminConfig.UploadQRCode)
		}

		// Admin Organizations
		admin.GET("/organizations", deps.Organizations.AdminList)
		admin.PATCH("/organizations/:id", deps.Organizations.AdminUpdateStatus)
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
	}

	return r
}

// internalKeyAuth validates the shared internal service API key.
// Uses constant-time comparison to prevent timing side-channel attacks.
func internalKeyAuth(key string) gin.HandlerFunc {
	expected := "Bearer " + key
	return func(c *gin.Context) {
		bearer := c.GetHeader("Authorization")
		// constant-time compare prevents timing attacks that could reveal key length or value
		if subtle.ConstantTimeCompare([]byte(bearer), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid internal key"})
			return
		}
		c.Next()
	}
}
