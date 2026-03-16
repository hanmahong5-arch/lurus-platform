package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminOpsHandler_BatchGenerateCodes_Validation(t *testing.T) {
	h := NewAdminOpsHandler(makeReferralService())
	r := testRouter()
	r.POST("/admin/v1/redemption-codes/batch", h.BatchGenerateCodes)

	tests := []struct {
		name   string
		body   map[string]interface{}
		status int
		errMsg string
	}{
		{
			"valid",
			map[string]interface{}{"count": 5, "product_id": "lurus_api", "plan_code": "pro", "duration_days": 30},
			http.StatusOK,
			"",
		},
		{
			"count_zero",
			map[string]interface{}{"count": 0, "product_id": "p", "plan_code": "c", "duration_days": 1},
			http.StatusBadRequest,
			"between 1 and 1000",
		},
		{
			"count_over_max",
			map[string]interface{}{"count": 1001, "product_id": "p", "plan_code": "c", "duration_days": 1},
			http.StatusBadRequest,
			"between 1 and 1000",
		},
		{
			"missing_product_id",
			map[string]interface{}{"count": 1, "plan_code": "c", "duration_days": 1},
			http.StatusBadRequest,
			"product_id",
		},
		{
			"missing_plan_code",
			map[string]interface{}{"count": 1, "product_id": "p", "duration_days": 1},
			http.StatusBadRequest,
			"plan_code",
		},
		{
			"zero_duration",
			map[string]interface{}{"count": 1, "product_id": "p", "plan_code": "c", "duration_days": 0},
			http.StatusBadRequest,
			"positive",
		},
		{
			"negative_duration",
			map[string]interface{}{"count": 1, "product_id": "p", "plan_code": "c", "duration_days": -1},
			http.StatusBadRequest,
			"positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/v1/redemption-codes/batch", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
			if tt.errMsg != "" {
				var resp map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &resp)
				errStr, _ := resp["error"].(string)
				if !containsStr(errStr, tt.errMsg) {
					t.Errorf("error = %q, want containing %q", errStr, tt.errMsg)
				}
			}
		})
	}
}

func TestAdminOpsHandler_BatchGenerateCodes_CSVExport(t *testing.T) {
	h := NewAdminOpsHandler(makeReferralService())
	r := testRouter()
	r.POST("/admin/v1/redemption-codes/batch", h.BatchGenerateCodes)

	body, _ := json.Marshal(map[string]interface{}{
		"count": 3, "product_id": "lurus_api", "plan_code": "pro", "duration_days": 30,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/redemption-codes/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/csv")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if disp := w.Header().Get("Content-Disposition"); disp == "" {
		t.Error("missing Content-Disposition header")
	}
}
