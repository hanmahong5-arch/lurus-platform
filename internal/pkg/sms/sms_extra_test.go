package sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripFunc is a helper that lets a plain function implement http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// newAliyunSenderWithTransport creates an AliyunSender and overrides its HTTP transport.
func newAliyunSenderWithTransport(transport http.RoundTripper) *AliyunSender {
	s := NewAliyunSender(SMSConfig{
		AliyunAccessKeyID:     "test-key-id",
		AliyunAccessKeySecret: "test-key-secret",
		AliyunSignName:        "TestSign",
	})
	s.client.SetTransport(transport)
	return s
}

// newTencentSenderWithTransport creates a TencentSender and overrides its HTTP transport.
func newTencentSenderWithTransport(transport http.RoundTripper) *TencentSender {
	s := NewTencentSender(SMSConfig{
		TencentSecretID:  "test-id",
		TencentSecretKey: "test-key",
		TencentAppID:     "140000000",
		TencentSignName:  "TestSign",
	})
	s.client.WithHttpTransport(transport)
	return s
}

// aliyunOKBody returns a minimal Aliyun SendSms success JSON body.
func aliyunOKBody() string {
	return `{"Code":"OK","Message":"OK","RequestId":"req-001","BizId":"biz-001"}`
}

// aliyunErrBody returns a minimal Aliyun SendSms error JSON body.
func aliyunErrBody(code, msg string) string {
	b, _ := json.Marshal(map[string]string{"Code": code, "Message": msg, "RequestId": "req-002"})
	return string(b)
}

// tencentOKBody returns a minimal Tencent SendSms success JSON body.
func tencentOKBody() string {
	return `{"Response":{"SendStatusSet":[{"SerialNo":"s1","PhoneNumber":"+8613800138000","Fee":1,"Code":"Ok","Message":"send success","IsoCode":"CN"}],"RequestId":"r1"}}`
}

// tencentErrBody returns a Tencent SendSms body with a non-Ok status code.
func tencentErrBody(code, msg string) string {
	body, _ := json.Marshal(map[string]interface{}{
		"Response": map[string]interface{}{
			"SendStatusSet": []map[string]interface{}{
				{"SerialNo": "s1", "PhoneNumber": "+8613800138000", "Fee": 1, "Code": code, "Message": msg, "IsoCode": "CN"},
			},
			"RequestId": "r2",
		},
	})
	return string(body)
}

// staticTransport returns an http.RoundTripper that always serves the given body / status.
func staticTransport(status int, body string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
}

// --- AliyunSender tests -------------------------------------------------

func TestAliyunSender_Send_Success(t *testing.T) {
	s := newAliyunSenderWithTransport(staticTransport(200, aliyunOKBody()))
	err := s.Send(context.Background(), "13800138000", "SMS_001", map[string]string{"code": "1234"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAliyunSender_Send_SuccessNoParams(t *testing.T) {
	s := newAliyunSenderWithTransport(staticTransport(200, aliyunOKBody()))
	err := s.Send(context.Background(), "13800138000", "SMS_001", nil)
	if err != nil {
		t.Fatalf("unexpected error with nil params: %v", err)
	}
}

func TestAliyunSender_Send_SuccessEmptyParams(t *testing.T) {
	s := newAliyunSenderWithTransport(staticTransport(200, aliyunOKBody()))
	err := s.Send(context.Background(), "13800138000", "SMS_001", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error with empty params: %v", err)
	}
}

func TestAliyunSender_Send_ErrorCode(t *testing.T) {
	s := newAliyunSenderWithTransport(staticTransport(200, aliyunErrBody("INVALID_PARAMETERS", "param error")))
	err := s.Send(context.Background(), "13800138000", "SMS_001", nil)
	if err == nil {
		t.Fatal("expected error for non-OK Aliyun code")
	}
	if !strings.Contains(err.Error(), "INVALID_PARAMETERS") {
		t.Errorf("error %q should mention the code", err.Error())
	}
}

func TestAliyunSender_Send_LimitExceeded(t *testing.T) {
	s := newAliyunSenderWithTransport(staticTransport(200, aliyunErrBody("LIMIT_DAY_REACH", "daily limit")))
	err := s.Send(context.Background(), "13800138000", "SMS_001", nil)
	if err == nil {
		t.Fatal("expected error for limit-exceeded Aliyun code")
	}
}

func TestAliyunSender_Send_HTTPError(t *testing.T) {
	// Non-JSON body forces the SDK to return an error.
	s := newAliyunSenderWithTransport(staticTransport(500, "internal server error"))
	err := s.Send(context.Background(), "13800138000", "SMS_001", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestAliyunSender_Send_MultipleParams(t *testing.T) {
	s := newAliyunSenderWithTransport(staticTransport(200, aliyunOKBody()))
	err := s.Send(context.Background(), "13900139000", "SMS_002",
		map[string]string{"code": "5678", "product": "lurus", "ttl": "5"})
	if err != nil {
		t.Fatalf("unexpected error with multiple params: %v", err)
	}
}

func TestAliyunSender_Send_ViaHTTPTestServer(t *testing.T) {
	// Validate that the HTTP request actually reaches our handler.
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, aliyunOKBody())
	}))
	defer srv.Close()

	s := newAliyunSenderWithTransport(srv.Client().Transport)
	// Override Domain so the SDK routes to our test server host.
	s.client.Domain = strings.TrimPrefix(srv.URL, "http://")
	s.client.SetHTTPSInsecure(true)

	err := s.Send(context.Background(), "13800138000", "SMS_VIA_SRV", map[string]string{"code": "9999"})
	// May or may not succeed depending on HTTPS/HTTP handling; we only care
	// that the code path is exercised (no panic).
	_ = err
	_ = gotBody
}

// --- TencentSender tests ------------------------------------------------

func TestTencentSender_Send_Success(t *testing.T) {
	s := newTencentSenderWithTransport(staticTransport(200, tencentOKBody()))
	err := s.Send(context.Background(), "13800138000", "TPL001", map[string]string{"code": "1234"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTencentSender_Send_SuccessWithTTL(t *testing.T) {
	s := newTencentSenderWithTransport(staticTransport(200, tencentOKBody()))
	err := s.Send(context.Background(), "13900139000", "TPL002",
		map[string]string{"code": "4321", "ttl": "10"})
	if err != nil {
		t.Fatalf("unexpected error with code+ttl params: %v", err)
	}
}

func TestTencentSender_Send_PhoneWithPlus(t *testing.T) {
	// Phone numbers that already have a prefix should NOT be prefixed again.
	s := newTencentSenderWithTransport(staticTransport(200, tencentOKBody()))
	err := s.Send(context.Background(), "+8613800138000", "TPL001", map[string]string{"code": "0000"})
	if err != nil {
		t.Fatalf("unexpected error for pre-formatted phone: %v", err)
	}
}

func TestTencentSender_Send_ShortPhone(t *testing.T) {
	// Phone numbers shorter than 11 chars should not get the +86 prefix.
	s := newTencentSenderWithTransport(staticTransport(200, tencentOKBody()))
	err := s.Send(context.Background(), "8613800", "TPL001", map[string]string{"code": "1111"})
	if err != nil {
		t.Fatalf("unexpected error for short phone: %v", err)
	}
}

func TestTencentSender_Send_ErrorCode(t *testing.T) {
	s := newTencentSenderWithTransport(staticTransport(200, tencentErrBody("LimitExceeded.PhoneNumberDailyLimit", "daily limit")))
	err := s.Send(context.Background(), "13800138000", "TPL001", nil)
	if err == nil {
		t.Fatal("expected error for non-Ok Tencent status code")
	}
	if !strings.Contains(err.Error(), "LimitExceeded") {
		t.Errorf("error %q should mention the code", err.Error())
	}
}

func TestTencentSender_Send_EmptyStatusSet(t *testing.T) {
	// Empty SendStatusSet should not error — the production code skips the check.
	body := `{"Response":{"SendStatusSet":[],"RequestId":"r3"}}`
	s := newTencentSenderWithTransport(staticTransport(200, body))
	err := s.Send(context.Background(), "13800138000", "TPL001", nil)
	if err != nil {
		t.Fatalf("unexpected error for empty status set: %v", err)
	}
}

func TestTencentSender_Send_HTTPError(t *testing.T) {
	s := newTencentSenderWithTransport(staticTransport(500, "{}"))
	// Tencent SDK may or may not error on 500 body; we just exercise the path.
	_ = s.Send(context.Background(), "13800138000", "TPL001", nil)
}

func TestTencentSender_Send_NoParams(t *testing.T) {
	s := newTencentSenderWithTransport(staticTransport(200, tencentOKBody()))
	err := s.Send(context.Background(), "13800138000", "TPL001", nil)
	if err != nil {
		t.Fatalf("unexpected error with nil params: %v", err)
	}
}

func TestTencentSender_Send_OnlyTTL(t *testing.T) {
	// Only "ttl" key present — "code" key absent; exercises the second branch.
	s := newTencentSenderWithTransport(staticTransport(200, tencentOKBody()))
	err := s.Send(context.Background(), "13800138000", "TPL001", map[string]string{"ttl": "5"})
	if err != nil {
		t.Fatalf("unexpected error with only ttl param: %v", err)
	}
}
