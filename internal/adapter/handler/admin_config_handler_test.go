package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// testAdminSettingStore is an in-memory store implementing app.adminSettingStore.
// It is defined here to avoid requiring an exported interface in the app package.
type testAdminSettingStore struct {
	settings []entity.AdminSetting
}

func (s *testAdminSettingStore) GetAll(_ context.Context) ([]entity.AdminSetting, error) {
	return s.settings, nil
}

func (s *testAdminSettingStore) Set(_ context.Context, key, value, updatedBy string) error {
	for i := range s.settings {
		if s.settings[i].Key == key {
			s.settings[i].Value = value
			s.settings[i].UpdatedBy = updatedBy
			s.settings[i].UpdatedAt = time.Now()
			return nil
		}
	}
	s.settings = append(s.settings, entity.AdminSetting{
		Key: key, Value: value, UpdatedBy: updatedBy, UpdatedAt: time.Now(),
	})
	return nil
}

// makeAdminConfigSvc creates an AdminConfigService backed by testAdminSettingStore
// and pre-loads the cache so GetEffective/Get work without an extra DB round-trip.
func makeAdminConfigSvc(settings []entity.AdminSetting) *app.AdminConfigService {
	store := &testAdminSettingStore{settings: settings}
	svc := app.NewAdminConfigService(store)
	_ = svc.Load(context.Background())
	return svc
}

func TestAdminConfigHandler_ListSettings_MasksSecrets(t *testing.T) {
	settings := []entity.AdminSetting{
		{Key: "epay_key", Value: "secret-value", IsSecret: true},
		{Key: "epay_partner_id", Value: "12345", IsSecret: false},
	}
	h := NewAdminConfigHandler(makeAdminConfigSvc(settings))
	r := testRouter()
	r.GET("/admin/v1/settings", h.ListSettings)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Settings []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	byKey := make(map[string]string, len(resp.Settings))
	for _, s := range resp.Settings {
		byKey[s.Key] = s.Value
	}

	if byKey["epay_key"] != "••••••••" {
		t.Errorf("epay_key value = %q, want ••••••••", byKey["epay_key"])
	}
	if byKey["epay_partner_id"] != "12345" {
		t.Errorf("epay_partner_id value = %q, want 12345", byKey["epay_partner_id"])
	}
}

func TestAdminConfigHandler_UpdateSettings_OK(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.PUT("/admin/v1/settings", h.UpdateSettings)

	body := `{"settings":[{"key":"epay_partner_id","value":"12345"}]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Updated int `json:"updated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Updated != 1 {
		t.Errorf("updated = %d, want 1", resp.Updated)
	}
}

func TestAdminConfigHandler_UploadQRCode_ValidBase64(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.POST("/admin/v1/settings/qrcode", h.UploadQRCode)

	// base64.StdEncoding.EncodeToString([]byte("hello world")) = "aGVsbG8gd29ybGQ="
	validB64 := base64.StdEncoding.EncodeToString([]byte("hello world"))
	body := `{"type":"alipay","image_base64":"` + validB64 + `"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/settings/qrcode", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestAdminConfigHandler_UploadQRCode_InvalidBase64(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.POST("/admin/v1/settings/qrcode", h.UploadQRCode)

	body := `{"type":"wechat","image_base64":"not_base64!!!"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/settings/qrcode", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminConfigHandler_GetPublicQRCode_OK(t *testing.T) {
	// PNG magic bytes (0x89 0x50 ...) → content-type image/png
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00, 0x00, 0x00}
	pngBase64 := base64.StdEncoding.EncodeToString(pngBytes)

	settings := []entity.AdminSetting{
		{Key: "qr_static_alipay", Value: pngBase64},
	}
	h := NewAdminConfigHandler(makeAdminConfigSvc(settings))
	r := testRouter()
	r.GET("/api/v1/public/qrcode/:type", h.GetPublicQRCode)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/qrcode/alipay", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		t.Errorf("Content-Type = %q, want image/*", ct)
	}
	if len(w.Body.Bytes()) == 0 {
		t.Error("expected non-empty body for QR code response")
	}
}

func TestAdminConfigHandler_GetPublicQRCode_Empty(t *testing.T) {
	// No QR code stored for this type → 204
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.GET("/api/v1/public/qrcode/:type", h.GetPublicQRCode)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/qrcode/alipay", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestAdminConfigHandler_GetPublicQRCode_UnknownType(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.GET("/api/v1/public/qrcode/:type", h.GetPublicQRCode)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/qrcode/paypal", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestAdminConfigHandler_GetPublicQRCode_InvalidBase64Stored(t *testing.T) {
	settings := []entity.AdminSetting{
		{Key: "qr_static_wechat", Value: "!!!not-valid-base64!!!"},
	}
	h := NewAdminConfigHandler(makeAdminConfigSvc(settings))
	r := testRouter()
	r.GET("/api/v1/public/qrcode/:type", h.GetPublicQRCode)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/qrcode/wechat", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 for invalid base64", w.Code)
	}
}

func TestAdminConfigHandler_GetPublicQRCode_JPEGDetection(t *testing.T) {
	// JPEG magic bytes: FF D8
	jpegBytes := []byte{0xFF, 0xD8, 0x00, 0x00, 0x00}
	jpegBase64 := base64.StdEncoding.EncodeToString(jpegBytes)
	settings := []entity.AdminSetting{
		{Key: "qr_channel_promo", Value: jpegBase64},
	}
	h := NewAdminConfigHandler(makeAdminConfigSvc(settings))
	r := testRouter()
	r.GET("/api/v1/public/qrcode/:type", h.GetPublicQRCode)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/qrcode/channel", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
}

func TestAdminConfigHandler_UpdateSettings_InvalidJSON(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.PUT("/admin/v1/settings", h.UpdateSettings)

	req := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", strings.NewReader("{bad json}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminConfigHandler_UploadQRCode_MissingFields(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.POST("/admin/v1/settings/qrcode", h.UploadQRCode)

	// Missing image_base64 field.
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/settings/qrcode", strings.NewReader(`{"type":"alipay"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminConfigHandler_UploadQRCode_UnknownType(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.POST("/admin/v1/settings/qrcode", h.UploadQRCode)

	validB64 := base64.StdEncoding.EncodeToString([]byte("fake image"))
	body := `{"type":"paypal","image_base64":"` + validB64 + `"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/settings/qrcode", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAdminConfigHandler_UploadQRCode_ChannelType(t *testing.T) {
	h := NewAdminConfigHandler(makeAdminConfigSvc(nil))
	r := testRouter()
	r.POST("/admin/v1/settings/qrcode", h.UploadQRCode)

	validB64 := base64.StdEncoding.EncodeToString([]byte("channel promo image"))
	body := `{"type":"channel","image_base64":"` + validB64 + `"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/settings/qrcode", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
}
