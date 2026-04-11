package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/payment"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

func makeSubHandler() *SubscriptionHandler {
	return NewSubscriptionHandler(
		makeSubService(),
		makeProductService(),
		makeWalletService(),
		payment.NewRegistry(),
	)
}

// ---------- ListSubscriptions ----------

func TestSubHandler_ListSubscriptions_OK(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.GET("/api/v1/subscriptions", withAccountID(1), h.ListSubscriptions)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["subscriptions"]; !ok {
		t.Error("response missing 'subscriptions' key")
	}
}

func TestSubHandler_ListSubscriptions_Empty(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.GET("/api/v1/subscriptions", withAccountID(999), h.ListSubscriptions)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}

// ---------- GetSubscription ----------

func TestSubHandler_GetSubscription_NotFound(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.GET("/api/v1/subscriptions/:product_id", withAccountID(1), h.GetSubscription)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/llm-api", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

// ---------- Checkout ----------

func TestSubHandler_Checkout_MissingBody(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", w.Code)
	}
}

func TestSubHandler_Checkout_PlanNotFound_Wallet(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]any{
		"product_id":     "llm-api",
		"plan_id":        999,
		"payment_method": "wallet",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

func TestSubHandler_Checkout_PlanNotFound_External(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]any{
		"product_id":     "llm-api",
		"plan_id":        999,
		"payment_method": "stripe",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
}

func makeSubHandlerWithPlan(plan *entity.ProductPlan) *SubscriptionHandler {
	ps := newMockPlanStore()
	ps.plans[plan.ID] = plan
	subSvc := app.NewSubscriptionService(newMockSubStore(), ps, makeEntitlementService(), 3)
	return NewSubscriptionHandler(subSvc, app.NewProductService(ps), makeWalletService(), payment.NewRegistry())
}

func TestSubHandler_Checkout_WalletPayment_FreePlan(t *testing.T) {
	h := makeSubHandlerWithPlan(&entity.ProductPlan{ID: 1, ProductID: "llm-api", PriceCNY: 0})
	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]any{
		"product_id":     "llm-api",
		"plan_id":        1,
		"payment_method": "wallet",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["subscription"]; !ok {
		t.Error("response missing 'subscription' key")
	}
}

func TestSubHandler_Checkout_WalletPayment_InsufficientBalance(t *testing.T) {
	h := makeSubHandlerWithPlan(&entity.ProductPlan{ID: 1, ProductID: "llm-api", PriceCNY: 99.0})
	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]any{
		"product_id":     "llm-api",
		"plan_id":        1,
		"payment_method": "wallet",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("status=%d, want 402; body=%s", w.Code, w.Body.String())
	}
}

// ---------- CancelSubscription ----------

func TestSubHandler_CancelSubscription_NoActive(t *testing.T) {
	h := makeSubHandler()
	r := testRouter()
	r.POST("/api/v1/subscriptions/:product_id/cancel", withAccountID(1), h.CancelSubscription)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/llm-api/cancel", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Cancel on non-existent subscription → error from service
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// ---------- resolveCheckout ----------

// TestSubHandler_Checkout_ExternalPayment_ProviderNil verifies 400 when the payment provider is nil.
func TestSubHandler_Checkout_ExternalPayment_ProviderNil(t *testing.T) {
	ps := newMockPlanStore()
	planID := int64(999)
	ps.plans[planID] = &entity.ProductPlan{
		ID:        planID,
		ProductID: "lucrum",
		PriceCNY:  99.0,
	}
	h := NewSubscriptionHandler(
		makeSubService(),
		app.NewProductService(ps),
		makeWalletService(),
		payment.NewRegistry(),
	)

	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]interface{}{
		"product_id":     "lucrum",
		"plan_id":        planID,
		"payment_method": "stripe",
		"return_url":     "https://example.com/return",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// stripe provider is nil → resolveCheckout returns error → 400
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (stripe provider not configured)", w.Code)
	}
}

// TestSubHandler_Checkout_EpayAlipay_ProviderNil verifies 400 for epay_alipay with nil provider.
func TestSubHandler_Checkout_EpayAlipay_ProviderNil(t *testing.T) {
	ps := newMockPlanStore()
	planID := int64(998)
	ps.plans[planID] = &entity.ProductPlan{
		ID:        planID,
		ProductID: "lucrum",
		PriceCNY:  49.0,
	}
	h := NewSubscriptionHandler(
		makeSubService(),
		app.NewProductService(ps),
		makeWalletService(),
		payment.NewRegistry(),
	)

	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]interface{}{
		"product_id":     "lucrum",
		"plan_id":        planID,
		"payment_method": "epay_alipay",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (epay provider not configured)", w.Code)
	}
}

// TestSubHandler_Checkout_UnknownMethod_ProviderNil verifies 400 for unknown payment method.
func TestSubHandler_Checkout_UnknownMethod_ProviderNil(t *testing.T) {
	ps := newMockPlanStore()
	planID := int64(997)
	ps.plans[planID] = &entity.ProductPlan{
		ID:        planID,
		ProductID: "lucrum",
		PriceCNY:  29.0,
	}
	h := NewSubscriptionHandler(
		makeSubService(),
		app.NewProductService(ps),
		makeWalletService(),
		payment.NewRegistry(),
	)

	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]interface{}{
		"product_id":     "lucrum",
		"plan_id":        planID,
		"payment_method": "unknown_provider",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (unknown payment method)", w.Code)
	}
}

// TestSubHandler_Checkout_Creem_ProviderNil verifies that creem payment with nil provider returns 400.
func TestSubHandler_Checkout_Creem_ProviderNil(t *testing.T) {
	ps := newMockPlanStore()
	planID := int64(996)
	ps.plans[planID] = &entity.ProductPlan{ID: planID, ProductID: "lucrum", PriceCNY: 15.0}
	h := NewSubscriptionHandler(makeSubService(), app.NewProductService(ps), makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.POST("/api/v1/subscriptions/checkout", withAccountID(1), h.Checkout)

	body, _ := json.Marshal(map[string]any{
		"product_id":     "lucrum",
		"plan_id":        planID,
		"payment_method": "creem",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions/checkout", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (creem provider not configured)", w.Code)
	}
}

// TestSubHandler_ListSubscriptions_Error verifies 500 when the store fails.
func TestSubHandler_ListSubscriptions_Error(t *testing.T) {
	subSvc := app.NewSubscriptionService(&errSubStoreH{*newMockSubStore()}, newMockPlanStore(), makeEntitlementService(), 3)
	h := NewSubscriptionHandler(subSvc, makeProductService(), makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/subscriptions", withAccountID(1), h.ListSubscriptions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
}

func TestSubHandler_GetSubscription_Error(t *testing.T) {
	subSvc := app.NewSubscriptionService(&errGetActiveSubStore{*newMockSubStore()}, newMockPlanStore(), makeEntitlementService(), 3)
	h := NewSubscriptionHandler(subSvc, makeProductService(), makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/subscriptions/:product_id", withAccountID(1), h.GetSubscription)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/llm-api", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestSubHandler_GetSubscription_Found(t *testing.T) {
	store := newMockSubStore()
	store.active["1:llm-api"] = &entity.Subscription{
		ID: 1, AccountID: 1, ProductID: "llm-api", Status: "active",
	}
	subSvc := app.NewSubscriptionService(store, newMockPlanStore(), makeEntitlementService(), 3)
	h := NewSubscriptionHandler(subSvc, makeProductService(), makeWalletService(), payment.NewRegistry())
	r := testRouter()
	r.GET("/api/v1/subscriptions/:product_id", withAccountID(1), h.GetSubscription)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/llm-api", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}
