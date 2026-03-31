package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestInternalAPI_ScopeEnforcement verifies that every internal API endpoint
// rejects requests with incorrect scopes (returns 403 Forbidden).
// The handler should never reach business logic when the scope is wrong.
func TestInternalAPI_ScopeEnforcement(t *testing.T) {
	// Create handler with nil services — scope check runs before any service call.
	h := NewInternalHandler(nil, nil, nil, nil, nil, nil, nil, "")

	type scopeTest struct {
		name       string
		method     string
		ginPath    string // Gin route pattern with :params
		reqPath    string // Actual request path
		wrongScope string
		body       any
		handler    gin.HandlerFunc
	}

	tests := []scopeTest{
		// account:read endpoints — test with wallet:debit (wrong scope)
		{"by_zitadel_sub", "GET", "/i/by-sub/:sub", "/i/by-sub/s1", "wallet:debit", nil, h.GetAccountByZitadelSub},
		{"by_id", "GET", "/i/by-id/:id", "/i/by-id/1", "wallet:debit", nil, h.GetAccountByID},
		{"overview", "GET", "/i/:id/overview", "/i/1/overview", "wallet:debit", nil, h.GetAccountOverview},
		{"by_email", "GET", "/i/by-email/:email", "/i/by-email/a@b.com", "wallet:debit", nil, h.GetAccountByEmail},
		{"by_phone", "GET", "/i/by-phone/:phone", "/i/by-phone/138", "wallet:debit", nil, h.GetAccountByPhone},
		{"by_oauth", "GET", "/i/by-oauth/:provider/:provider_id", "/i/by-oauth/wx/1", "wallet:debit", nil, h.GetAccountByOAuth},
		{"validate_session", "POST", "/i/validate-session", "/i/validate-session", "wallet:debit",
			map[string]string{"token": "t"}, h.ValidateSession},

		// account:write — test with account:read
		{"upsert", "POST", "/i/upsert", "/i/upsert", "account:read",
			map[string]string{"zitadel_sub": "s", "email": "e@e.com"}, h.UpsertAccount},

		// wallet:read — test with account:write
		{"usage_report", "POST", "/i/usage", "/i/usage", "account:write",
			map[string]any{"account_id": 1, "amount_cny": 1.0}, h.ReportUsage},
		{"billing_summary", "GET", "/i/:id/billing", "/i/1/billing", "account:write", nil, h.GetBillingSummary},
		{"wallet_balance", "GET", "/i/:id/balance", "/i/1/balance", "account:write", nil, h.GetWalletBalance},
		{"currency_info", "GET", "/i/currency-info", "/i/currency-info", "account:write", nil, h.GetCurrencyInfo},
		{"list_transactions", "POST", "/i/:id/txns", "/i/1/txns", "account:write", nil, h.InternalListWalletTransactions},

		// wallet:debit — test with account:read
		{"debit", "POST", "/i/:id/debit", "/i/1/debit", "account:read",
			map[string]any{"amount": 1.0, "type": "t"}, h.DebitWallet},
		{"pre_authorize", "POST", "/i/:id/pre-auth", "/i/1/pre-auth", "account:read",
			map[string]any{"amount": 1.0, "product_id": "p"}, h.PreAuthorize},
		{"settle", "POST", "/i/pa/:id/settle", "/i/pa/1/settle", "account:read",
			map[string]any{"actual_amount": 1.0}, h.SettlePreAuth},
		{"release", "POST", "/i/pa/:id/release", "/i/pa/1/release", "account:read", nil, h.ReleasePreAuth},
		{"exchange", "POST", "/i/:id/exchange", "/i/1/exchange", "account:read",
			map[string]any{"amount": 1.0, "lurus_user_id": 1, "idempotency_key": "k"}, h.ExchangeLucToLut},

		// entitlement — test with wallet:read
		{"entitlements", "GET", "/i/:id/ent/:product_id", "/i/1/ent/lucrum", "wallet:read", nil, h.GetEntitlements},
		{"subscription", "GET", "/i/:id/sub/:product_id", "/i/1/sub/lucrum", "wallet:read", nil, h.GetSubscription},

		// checkout — test with account:read
		{"create_checkout", "POST", "/i/checkout/create", "/i/checkout/create", "account:read",
			map[string]any{"account_id": 1, "amount_cny": 10.0, "payment_method": "stripe", "source_service": "t"}, h.CreateCheckout},
		{"checkout_status", "GET", "/i/checkout/:order_no/status", "/i/checkout/ORD1/status", "account:read", nil, h.GetCheckoutStatus},
		{"payment_methods", "GET", "/i/pm", "/i/pm", "account:read", nil, h.GetPaymentMethods},
		{"sub_checkout", "POST", "/i/sub-checkout", "/i/sub-checkout", "account:read",
			map[string]any{"account_id": 1, "product_id": "p", "plan_code": "c", "billing_cycle": "monthly", "payment_method": "wallet"}, h.InternalSubscriptionCheckout},
	}

	for _, tt := range tests {
		t.Run("scope_403_"+tt.name, func(t *testing.T) {
			r := testRouter()

			switch tt.method {
			case "GET":
				r.GET(tt.ginPath, withServiceScopes(tt.wrongScope), tt.handler)
			case "POST":
				r.POST(tt.ginPath, withServiceScopes(tt.wrongScope), tt.handler)
			}

			var req *http.Request
			if tt.body != nil {
				b, _ := json.Marshal(tt.body)
				req = httptest.NewRequest(tt.method, tt.reqPath, bytes.NewReader(b))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.reqPath, nil)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 Forbidden; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestInternalAPI_ScopeEnforcement_CorrectScope verifies that correct scopes
// pass the scope check (handler proceeds to business logic, not 403).
func TestInternalAPI_ScopeEnforcement_CorrectScope(t *testing.T) {
	h := NewInternalHandler(
		makeAccountServiceWith(newMockAccountStore()),
		makeSubService(),
		makeEntitlementService(),
		makeVIPService(),
		makeOverviewServiceH(),
		makeWalletService(),
		makeReferralService(),
		"",
	)

	tests := []struct {
		name      string
		method    string
		ginPath   string
		reqPath   string
		scope     string
		handler   gin.HandlerFunc
		wantNotForbidden bool
	}{
		{"account_read", "GET", "/i/by-sub/:sub", "/i/by-sub/nonexistent", "account:read", h.GetAccountByZitadelSub, true},
		{"wallet_read", "GET", "/i/:id/balance", "/i/1/balance", "wallet:read", h.GetWalletBalance, true},
		{"entitlement", "GET", "/i/:id/ent/:product_id", "/i/1/ent/lucrum", "entitlement", h.GetEntitlements, true},
		{"checkout", "GET", "/i/pm", "/i/pm", "checkout", h.GetPaymentMethods, true},
	}

	for _, tt := range tests {
		t.Run("scope_pass_"+tt.name, func(t *testing.T) {
			r := testRouter()
			r.GET(tt.ginPath, withServiceScopes(tt.scope), tt.handler)

			req := httptest.NewRequest(tt.method, tt.reqPath, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code == http.StatusForbidden {
				t.Errorf("status = 403, should NOT be forbidden with correct scope %s", tt.scope)
			}
		})
	}
}
