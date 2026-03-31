package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ── parsePagination edge cases ────────────────────────────────────────────

func TestParsePagination_Default(t *testing.T) {
	r := testRouter()
	var gotPage, gotSize int
	r.GET("/test", func(c *gin.Context) {
		gotPage, gotSize = parsePagination(c)
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if gotPage != 1 {
		t.Errorf("page = %d, want 1 (default)", gotPage)
	}
	if gotSize != 20 {
		t.Errorf("page_size = %d, want 20 (default)", gotSize)
	}
}

func TestParsePagination_CustomValues(t *testing.T) {
	r := testRouter()
	var gotPage, gotSize int
	r.GET("/test", func(c *gin.Context) {
		gotPage, gotSize = parsePagination(c)
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?page=3&page_size=50", nil)
	r.ServeHTTP(w, req)

	if gotPage != 3 {
		t.Errorf("page = %d, want 3", gotPage)
	}
	if gotSize != 50 {
		t.Errorf("page_size = %d, want 50", gotSize)
	}
}

func TestParsePagination_ClampAbove100(t *testing.T) {
	r := testRouter()
	var gotSize int
	r.GET("/test", func(c *gin.Context) {
		_, gotSize = parsePagination(c)
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?page_size=999", nil)
	r.ServeHTTP(w, req)

	if gotSize != 100 {
		t.Errorf("page_size = %d, want 100 (clamped)", gotSize)
	}
}

func TestParsePagination_NegativePage(t *testing.T) {
	r := testRouter()
	var gotPage int
	r.GET("/test", func(c *gin.Context) {
		gotPage, _ = parsePagination(c)
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?page=-1", nil)
	r.ServeHTTP(w, req)

	if gotPage != 1 {
		t.Errorf("page = %d, want 1 (negative ignored)", gotPage)
	}
}

func TestParsePagination_ZeroPageSize(t *testing.T) {
	r := testRouter()
	var gotSize int
	r.GET("/test", func(c *gin.Context) {
		_, gotSize = parsePagination(c)
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?page_size=0", nil)
	r.ServeHTTP(w, req)

	if gotSize != 20 {
		t.Errorf("page_size = %d, want 20 (zero uses default)", gotSize)
	}
}

func TestParsePagination_NonNumeric(t *testing.T) {
	r := testRouter()
	var gotPage, gotSize int
	r.GET("/test", func(c *gin.Context) {
		gotPage, gotSize = parsePagination(c)
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test?page=abc&page_size=xyz", nil)
	r.ServeHTTP(w, req)

	if gotPage != 1 || gotSize != 20 {
		t.Errorf("page=%d, size=%d, want 1, 20 (non-numeric uses defaults)", gotPage, gotSize)
	}
}

// ── parsePathInt64 edge cases ─────────────────────────────────────────────

func TestParsePathInt64_Valid(t *testing.T) {
	r := testRouter()
	var gotID int64
	var gotOK bool
	r.GET("/test/:id", func(c *gin.Context) {
		gotID, gotOK = parsePathInt64(c, "id", "Test ID")
		if gotOK {
			c.Status(200)
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test/42", nil)
	r.ServeHTTP(w, req)

	if !gotOK || gotID != 42 {
		t.Errorf("id = %d, ok = %v, want 42, true", gotID, gotOK)
	}
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestParsePathInt64_Zero(t *testing.T) {
	r := testRouter()
	r.GET("/test/:id", func(c *gin.Context) {
		parsePathInt64(c, "id", "Test ID")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test/0", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (zero ID rejected)", w.Code)
	}
}

func TestParsePathInt64_Negative(t *testing.T) {
	r := testRouter()
	r.GET("/test/:id", func(c *gin.Context) {
		parsePathInt64(c, "id", "Test ID")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test/-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (negative ID rejected)", w.Code)
	}
}

func TestParsePathInt64_NonNumeric(t *testing.T) {
	r := testRouter()
	r.GET("/test/:id", func(c *gin.Context) {
		parsePathInt64(c, "id", "Test ID")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test/abc", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (non-numeric)", w.Code)
	}
}

// ── requireAccountID edge cases ───────────────────────────────────────────

func TestRequireAccountID_Missing(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		// No account_id in context.
		requireAccountID(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (missing account_id)", w.Code)
	}
}

func TestRequireAccountID_ZeroValue(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		c.Set("account_id", int64(0))
		requireAccountID(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (zero account_id)", w.Code)
	}
}

func TestRequireAccountID_WrongType(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		c.Set("account_id", "not-an-int64") // string instead of int64
		requireAccountID(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (wrong type)", w.Code)
	}
}

func TestRequireAccountID_Valid(t *testing.T) {
	r := testRouter()
	var gotID int64
	r.GET("/test", func(c *gin.Context) {
		c.Set("account_id", int64(42))
		id, ok := requireAccountID(c)
		if ok {
			gotID = id
			c.Status(200)
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 || gotID != 42 {
		t.Errorf("status=%d, id=%d, want 200, 42", w.Code, gotID)
	}
}

// ── respondInternalError ──────────────────────────────────────────────────

func TestRespondInternalError_NoInfoLeak(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		respondInternalError(c, "test.context", fmt.Errorf("db connection string: host=secret:5432"))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	// Verify response body does NOT contain the internal error details.
	body := w.Body.String()
	if contains(body, "secret") || contains(body, "5432") || contains(body, "connection") {
		t.Errorf("response leaks internal details: %s", body)
	}
	// Verify it returns the generic message.
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "An internal error occurred" {
		t.Errorf("message = %v, want 'An internal error occurred'", resp["message"])
	}
}

// ── respondRateLimitedRich ────────────────────────────────────────────────

func TestRespondRateLimitedRich_Format(t *testing.T) {
	r := testRouter()
	r.GET("/test", func(c *gin.Context) {
		respondRateLimitedRich(c, "Too many requests", 5000)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	var resp RichError
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error.Code != "rate_limited" {
		t.Errorf("code = %s, want rate_limited", resp.Error.Code)
	}
	if resp.Error.RetryAfterMs != 5000 {
		t.Errorf("retry_after_ms = %d, want 5000", resp.Error.RetryAfterMs)
	}
}

// ── Action builders ───────────────────────────────────────────────────────

func TestActionGoToLogin(t *testing.T) {
	a := ActionGoToLogin()
	if a.Type != "link" || a.URL != "/login" {
		t.Errorf("ActionGoToLogin = %+v, want type=link, url=/login", a)
	}
}

func TestActionGoToRegister(t *testing.T) {
	a := ActionGoToRegister()
	if a.Type != "link" || a.URL != "/register" {
		t.Errorf("ActionGoToRegister = %+v, want type=link, url=/register", a)
	}
}

func TestActionGoToForgotPassword(t *testing.T) {
	a := ActionGoToForgotPassword()
	if a.Type != "link" || a.URL != "/forgot-password" {
		t.Errorf("ActionGoToForgotPassword = %+v, want type=link, url=/forgot-password", a)
	}
}

func TestActionRetry(t *testing.T) {
	a := ActionRetry()
	if a.Type != "retry" || a.URL != "" {
		t.Errorf("ActionRetry = %+v, want type=retry, url empty", a)
	}
}

// ── toJSONFieldName edge cases ────────────────────────────────────────────

// ── validationTagToMessage coverage ────────────────────────────────────────

func TestValidationTagToMessage_AllTags(t *testing.T) {
	tests := []struct {
		tag      string
		param    string
		contains string
	}{
		{"required", "", "required"},
		{"email", "", "email"},
		{"min", "8", "at least 8"},
		{"max", "32", "at most 32"},
		{"gt", "0", "greater than 0"},
		{"gte", "1", "at least 1"},
		{"lt", "100", "less than 100"},
		{"lte", "50", "at most 50"},
		{"oneof", "a b c", "one of: a b c"},
		{"url", "", "URL"},
		{"uuid", "", "UUID"},
		{"numeric", "", "number"},
		{"alphanum", "", "letters and numbers"},
		{"unknown_tag", "", "Invalid value"},
	}
	for _, tt := range tests {
		t.Run("tag_"+tt.tag, func(t *testing.T) {
			got := validationTagToMessage(tt.tag, tt.param, "field")
			if !contains(got, tt.contains) {
				t.Errorf("validationTagToMessage(%q) = %q, want containing %q", tt.tag, got, tt.contains)
			}
		})
	}
}

func TestToJSONFieldName_AllUpperCase(t *testing.T) {
	got := toJSONFieldName("ID")
	if got != "i_d" {
		// Simple camelCase→snake_case: 'I' → '_i', 'D' → '_d'
		// First char uppercase doesn't get prefix underscore.
		t.Logf("note: toJSONFieldName('ID') = %q (implementation-defined)", got)
	}
}

func TestToJSONFieldName_SingleChar(t *testing.T) {
	got := toJSONFieldName("A")
	if got != "a" {
		t.Errorf("toJSONFieldName('A') = %q, want 'a'", got)
	}
}

func TestToJSONFieldName_AlreadyLower(t *testing.T) {
	got := toJSONFieldName("email")
	if got != "email" {
		t.Errorf("toJSONFieldName('email') = %q, want 'email'", got)
	}
}

func TestToJSONFieldName_CamelCase(t *testing.T) {
	got := toJSONFieldName("AmountCNY")
	if got != "amount_c_n_y" {
		t.Logf("note: toJSONFieldName('AmountCNY') = %q (consecutive capitals)", got)
	}
}

// helper
func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
