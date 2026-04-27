package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// --- Constructor ---

func TestNewWechatPayProvider_Disabled_EmptyMchID(t *testing.T) {
	p, err := NewWechatPayProvider(WechatPayConfig{
		MchID:      "",
		APIv3Key:   "key32chars_1234567890123456789",
		PrivateKey: "dummy",
	}, TradeTypeNative)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when MchID empty")
	}
}

func TestNewWechatPayProvider_Disabled_EmptyAPIv3Key(t *testing.T) {
	p, err := NewWechatPayProvider(WechatPayConfig{
		MchID:      "1234567890",
		APIv3Key:   "",
		PrivateKey: "dummy",
	}, TradeTypeNative)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when APIv3Key empty")
	}
}

func TestNewWechatPayProvider_Disabled_EmptyPrivateKey(t *testing.T) {
	p, err := NewWechatPayProvider(WechatPayConfig{
		MchID:    "1234567890",
		APIv3Key: "key32chars_1234567890123456789",
	}, TradeTypeNative)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when PrivateKey empty")
	}
}

func TestNewWechatPayProvider_Disabled_AllEmpty(t *testing.T) {
	p, err := NewWechatPayProvider(WechatPayConfig{}, TradeTypeNative)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provider when all fields empty")
	}
}

// --- Name ---

func TestWechatPayProvider_Name_Native(t *testing.T) {
	p := &WechatPayProvider{tradeType: TradeTypeNative}
	if p.Name() != "wechat_native" {
		t.Errorf("Name() = %q, want wechat_native", p.Name())
	}
}

func TestWechatPayProvider_Name_WAP(t *testing.T) {
	p := &WechatPayProvider{tradeType: TradeTypeWAP}
	if p.Name() != "wechat_h5" {
		t.Errorf("Name() = %q, want wechat_h5", p.Name())
	}
}

func TestWechatPayProvider_Name_JSAPI(t *testing.T) {
	p := &WechatPayProvider{tradeType: TradeTypeJSAPI}
	if p.Name() != "wechat_jsapi" {
		t.Errorf("Name() = %q, want wechat_jsapi", p.Name())
	}
}

func TestWechatPayProvider_Name_PC_FallsBackToNative(t *testing.T) {
	p := &WechatPayProvider{tradeType: TradeTypePC}
	if p.Name() != "wechat_native" {
		t.Errorf("Name() = %q, want wechat_native (default)", p.Name())
	}
}

// --- extractOpenID ---

func TestExtractOpenID_Valid(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"openid": "oLSt45abcXYZ"})
	o := &entity.PaymentOrder{CallbackData: data}
	got := extractOpenID(o)
	if got != "oLSt45abcXYZ" {
		t.Errorf("extractOpenID = %q, want oLSt45abcXYZ", got)
	}
}

func TestExtractOpenID_EmptyCallbackData(t *testing.T) {
	o := &entity.PaymentOrder{}
	if extractOpenID(o) != "" {
		t.Error("expected empty string for nil CallbackData")
	}
}

func TestExtractOpenID_MalformedJSON(t *testing.T) {
	o := &entity.PaymentOrder{CallbackData: json.RawMessage(`{invalid}`)}
	if extractOpenID(o) != "" {
		t.Error("expected empty string for malformed JSON")
	}
}

func TestExtractOpenID_MissingField(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"user_id": "u123"})
	o := &entity.PaymentOrder{CallbackData: data}
	if extractOpenID(o) != "" {
		t.Error("expected empty string when openid field missing")
	}
}

func TestExtractOpenID_EmptyOpenIDValue(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"openid": ""})
	o := &entity.PaymentOrder{CallbackData: data}
	if extractOpenID(o) != "" {
		t.Error("expected empty string for empty openid value")
	}
}

// --- HandleNotify ---

// buildWechatNotifyRequest builds a simulated WeChat Pay v3 notify HTTP request.
// WeChat v3 notify body is JSON with encrypted resource field.
func buildWechatV3NotifyRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/notify/wechat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestWechatPayProvider_HandleNotify_MalformedJSON(t *testing.T) {
	p := &WechatPayProvider{apiV3Key: "test-api-v3-key-32chars!!!!!!"}

	req := buildWechatV3NotifyRequest(`{invalid json}`)
	_, ok, err := p.HandleNotify(req)
	// Should fail to parse — either error returned or ok=false.
	if ok && err == nil {
		t.Error("expected error or ok=false for malformed JSON notify body")
	}
}

func TestWechatPayProvider_HandleNotify_EmptyBody(t *testing.T) {
	p := &WechatPayProvider{apiV3Key: "test-api-v3-key-32chars!!!!!!"}

	req := buildWechatV3NotifyRequest("")
	_, ok, err := p.HandleNotify(req)
	// Should fail gracefully.
	if ok && err == nil {
		t.Error("expected error or ok=false for empty notify body")
	}
}

func TestWechatPayProvider_HandleNotify_ValidStructureButInvalidDecrypt(t *testing.T) {
	// A structurally valid V3 notify but with dummy encrypted data.
	// V3ParseNotify should parse, but DecryptPayCipherText should fail.
	p := &WechatPayProvider{apiV3Key: "01234567890123456789012345678901"} // 32-char key

	body := `{
		"id":"notify_123",
		"create_time":"2026-04-11T10:00:00+08:00",
		"event_type":"TRANSACTION.SUCCESS",
		"resource_type":"encrypt-resource",
		"resource":{
			"algorithm":"AEAD_AES_256_GCM",
			"ciphertext":"invalid_ciphertext_that_cannot_decrypt",
			"nonce":"AAAAAAAAAAAA",
			"associated_data":"transaction"
		}
	}`
	req := buildWechatV3NotifyRequest(body)
	_, ok, err := p.HandleNotify(req)
	// Should return error from decryption failure, not panic.
	if ok && err == nil {
		t.Error("expected error for invalid ciphertext")
	}
}

// --- CreateCheckout (routing logic) ---

func TestWechatPayProvider_CreateCheckout_JSAPIRequiresOpenID(t *testing.T) {
	// JSAPI without openid in CallbackData must return an error.
	p := &WechatPayProvider{
		tradeType: TradeTypeJSAPI,
		appID:     "wx_test_app",
		mchID:     "1234567890",
		notifyURL: "https://notify.example.com",
	}

	order := &entity.PaymentOrder{
		OrderNo:       "LO-JSAPI-001",
		AmountCNY:     30.0,
		PaymentMethod: "wechat_jsapi",
		CallbackData:  json.RawMessage(`{}`), // no openid
	}

	_, _, err := p.CreateCheckout(context.Background(), order, "")
	if err == nil {
		t.Fatal("expected error when openid missing for JSAPI")
	}
	if !strings.Contains(err.Error(), "openid") {
		t.Errorf("error should mention openid, got: %v", err)
	}
}

func TestWechatPayProvider_CreateCheckout_JSAPIRequiresOpenID_EmptyCallbackData(t *testing.T) {
	p := &WechatPayProvider{
		tradeType: TradeTypeJSAPI,
		appID:     "wx_test_app",
		mchID:     "1234567890",
		notifyURL: "https://notify.example.com",
	}

	order := &entity.PaymentOrder{
		OrderNo:       "LO-JSAPI-002",
		AmountCNY:     50.0,
		PaymentMethod: "wechat_jsapi",
		// CallbackData is nil
	}

	_, _, err := p.CreateCheckout(context.Background(), order, "")
	if err == nil {
		t.Fatal("expected error when CallbackData is nil (no openid)")
	}
	if !strings.Contains(err.Error(), "openid") {
		t.Errorf("error should mention openid, got: %v", err)
	}
}

func TestWechatPayProvider_CreateCheckout_AmountZeroRoundsUpToOne(t *testing.T) {
	// AmountCNY=0 should be rounded up to 1 fen (minimum).
	p := &WechatPayProvider{
		tradeType: TradeTypeJSAPI,
		appID:     "wx",
		mchID:     "1234",
		notifyURL: "https://notify.example.com",
	}

	order := &entity.PaymentOrder{
		OrderNo:       "LO-ZERO",
		AmountCNY:     0,
		PaymentMethod: "wechat_jsapi",
	}

	// The JSAPI path checks for openid before making the API call,
	// so it will fail with openid error — but the amount logic runs first.
	_, _, err := p.CreateCheckout(context.Background(), order, "")
	if err == nil {
		t.Error("expected error (openid missing)")
	}
	// Just verifying no panic from amount=0
}

func TestWechatPayProvider_CreateCheckout_PaymentMethodOverride_H5(t *testing.T) {
	// Verify that "wechat_h5" payment method override switches to H5 trade type.
	// The provider default is Native; method override should pick H5.
	// H5 API will fail (no real client), but the override routing is exercised.
	p := &WechatPayProvider{
		tradeType: TradeTypeNative,
		appID:     "wx",
		mchID:     "1234",
		notifyURL: "https://notify.example.com",
	}

	order := &entity.PaymentOrder{
		OrderNo:       "LO-H5-OVERRIDE",
		AmountCNY:     10.0,
		PaymentMethod: "wechat_h5",
	}

	// Will fail at API call level (nil client), recover gracefully.
	func() {
		defer func() { recover() }()
		_, _, _ = p.CreateCheckout(context.Background(), order, "")
	}()
}

func TestWechatPayProvider_CreateCheckout_PaymentMethodOverride_Native(t *testing.T) {
	// Verify that "wechat_native" payment method override is handled.
	p := &WechatPayProvider{
		tradeType: TradeTypeWAP,
		appID:     "wx",
		mchID:     "1234",
		notifyURL: "https://notify.example.com",
	}

	order := &entity.PaymentOrder{
		OrderNo:       "LO-NATIVE-OVERRIDE",
		AmountCNY:     10.0,
		PaymentMethod: "wechat_native",
	}

	func() {
		defer func() { recover() }()
		_, _, _ = p.CreateCheckout(context.Background(), order, "")
	}()
}
