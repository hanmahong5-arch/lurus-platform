package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ── handleBindError field-level extraction ───────────────────────────────────

func TestHandleBindError_RequiredField(t *testing.T) {
	r := testRouter()
	r.POST("/test", func(c *gin.Context) {
		var req struct {
			Name  string `json:"name" binding:"required"`
			Email string `json:"email" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			handleBindError(c, err)
			return
		}
		c.String(200, "ok")
	})

	// Send empty body — both fields missing.
	body := `{}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp RichError
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, w.Body.String())
	}

	if resp.Error.Code != "validation_error" {
		t.Errorf("code = %q, want 'validation_error'", resp.Error.Code)
	}
	if resp.Error.Fields == nil {
		t.Fatal("expected fields map")
	}
	if resp.Error.Fields["name"] == "" {
		t.Error("expected error on 'name' field")
	}
	if resp.Error.Fields["email"] == "" {
		t.Error("expected error on 'email' field")
	}
}

func TestHandleBindError_GreaterThanZero(t *testing.T) {
	r := testRouter()
	r.POST("/test", func(c *gin.Context) {
		var req struct {
			Amount float64 `json:"amount" binding:"required,gt=0"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			handleBindError(c, err)
			return
		}
		c.String(200, "ok")
	})

	body := `{"amount": -5}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var resp RichError
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Error.Fields == nil || resp.Error.Fields["amount"] == "" {
		t.Errorf("expected error on 'amount' field; fields = %v", resp.Error.Fields)
	}
	if resp.Error.Fields != nil && !strings.Contains(resp.Error.Fields["amount"], "greater than") {
		t.Errorf("amount error = %q, want containing 'greater than'", resp.Error.Fields["amount"])
	}
}

func TestHandleBindError_InvalidJSON(t *testing.T) {
	r := testRouter()
	r.POST("/test", func(c *gin.Context) {
		var req struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			handleBindError(c, err)
			return
		}
		c.String(200, "ok")
	})

	body := `{invalid json`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	// Should NOT contain validator field details — just generic message.
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, "fields") {
		t.Error("JSON parse error should not include fields map")
	}
}

func TestHandleBindError_BodyTooLarge(t *testing.T) {
	r := testRouter()
	r.Use(MaxBodySize(50)) // 50 bytes limit
	r.POST("/test", func(c *gin.Context) {
		var req struct {
			Data string `json:"data" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			handleBindError(c, err)
			return
		}
		c.String(200, "ok")
	})

	body := `{"data":"` + strings.Repeat("x", 100) + `"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 413 or 400", w.Code)
	}
}

// ── toJSONFieldName ─────────────────────────────────────────────────────────

func TestToJSONFieldName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Name", "name"},
		{"AmountCNY", "amount_c_n_y"},
		{"PaymentMethod", "payment_method"},
		{"ID", "i_d"},
		{"email", "email"},
	}
	for _, tc := range tests {
		got := toJSONFieldName(tc.input)
		if got != tc.want {
			t.Errorf("toJSONFieldName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── validationTagToMessage ──────────────────────────────────────────────────

func TestValidationTagToMessage(t *testing.T) {
	tests := []struct {
		tag, param, field string
		wantContains      string
	}{
		{"required", "", "name", "required"},
		{"email", "", "email", "valid email"},
		{"min", "8", "password", "at least 8"},
		{"gt", "0", "amount", "greater than 0"},
		{"oneof", "stripe epay", "method", "one of"},
		{"unknown_tag", "", "x", "Invalid"},
	}
	for _, tc := range tests {
		got := validationTagToMessage(tc.tag, tc.param, tc.field)
		if !strings.Contains(strings.ToLower(got), strings.ToLower(tc.wantContains)) {
			t.Errorf("validationTagToMessage(%q, %q, %q) = %q, want containing %q",
				tc.tag, tc.param, tc.field, got, tc.wantContains)
		}
	}
}

// ── respondValidationError format ───────────────────────────────────────────

func TestRespondValidationError_Format(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		respondValidationError(c, "Fix these", map[string]string{
			"email": "Invalid format",
			"name":  "Required",
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	var resp RichError
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Error.Code != "validation_error" {
		t.Errorf("code = %q", resp.Error.Code)
	}
	if resp.Error.Message != "Fix these" {
		t.Errorf("message = %q", resp.Error.Message)
	}
	if len(resp.Error.Fields) != 2 {
		t.Errorf("fields count = %d, want 2", len(resp.Error.Fields))
	}
}

// ── respondConflictWithAction format ────────────────────────────────────────

func TestRespondConflictWithAction_Format(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		respondConflictWithAction(c, "Already exists",
			ActionGoToLogin(), ActionGoToForgotPassword())
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}

	var resp RichError
	json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.Error.Actions) != 2 {
		t.Fatalf("actions count = %d, want 2", len(resp.Error.Actions))
	}
	if resp.Error.Actions[0].URL != "/login" {
		t.Errorf("action[0].url = %q, want '/login'", resp.Error.Actions[0].URL)
	}
	if resp.Error.Actions[1].URL != "/forgot-password" {
		t.Errorf("action[1].url = %q, want '/forgot-password'", resp.Error.Actions[1].URL)
	}
}
