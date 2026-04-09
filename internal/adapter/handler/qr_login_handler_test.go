package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/auth"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const testSessionSecret = "test-secret-must-be-at-least-32-bytes!!"

func setupQRLoginTest(t *testing.T) (*handler.QRLoginHandler, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	h := handler.NewQRLoginHandler(rdb, testSessionSecret)
	return h, mr
}

func TestQRLogin_CreateSession(t *testing.T) {
	h, _ := setupQRLoginTest(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)

	h.CreateSession(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["session_id"]; !ok {
		t.Fatal("response missing session_id")
	}
	if _, ok := resp["qr_url"]; !ok {
		t.Fatal("response missing qr_url")
	}
	expiresIn, ok := resp["expires_in"].(float64)
	if !ok || expiresIn != 300 {
		t.Fatalf("expected expires_in=300, got %v", resp["expires_in"])
	}
}

func TestQRLogin_PollStatus_Pending(t *testing.T) {
	h, _ := setupQRLoginTest(t)

	// Create session first.
	gin.SetMode(gin.TestMode)
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)

	var createResp map[string]any
	json.Unmarshal(w1.Body.Bytes(), &createResp)
	sessionID := createResp["session_id"].(string)

	// Poll with short timeout.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/public/qr-login/%s/status?timeout=1", sessionID), nil)
	c2.Params = gin.Params{{Key: "id", Value: sessionID}}

	h.PollStatus(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var pollResp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &pollResp)
	if pollResp["status"] != "pending" {
		t.Fatalf("expected pending, got %v", pollResp["status"])
	}
}

func TestQRLogin_PollStatus_NotFound(t *testing.T) {
	h, _ := setupQRLoginTest(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/nonexistent/status?timeout=1", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}

	h.PollStatus(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_FullFlow_CreateConfirmConsume(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Step 1: Create session.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)

	var createResp map[string]any
	json.Unmarshal(w1.Body.Bytes(), &createResp)
	sessionID := createResp["session_id"].(string)

	// Step 2: Confirm (simulate authenticated user with account_id=42).
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/qr-login/%s/confirm", sessionID), nil)
	c2.Params = gin.Params{{Key: "id", Value: sessionID}}
	c2.Set("account_id", int64(42))

	h.Confirm(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var confirmResp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &confirmResp)
	if confirmResp["confirmed"] != true {
		t.Fatal("expected confirmed=true")
	}

	// Step 3: Poll — should get token.
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/public/qr-login/%s/status?timeout=1", sessionID), nil)
	c3.Params = gin.Params{{Key: "id", Value: sessionID}}

	h.PollStatus(c3)

	if w3.Code != http.StatusOK {
		t.Fatalf("poll: expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	var pollResp map[string]any
	json.Unmarshal(w3.Body.Bytes(), &pollResp)
	if pollResp["status"] != "confirmed" {
		t.Fatalf("expected confirmed, got %v", pollResp["status"])
	}
	tokenStr, ok := pollResp["token"].(string)
	if !ok || tokenStr == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate the issued token.
	accountID, err := auth.ValidateSessionToken(tokenStr, testSessionSecret)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if accountID != 42 {
		t.Fatalf("expected account_id=42, got %d", accountID)
	}

	// Step 4: Second poll — should get "consumed" (410 Gone).
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/public/qr-login/%s/status?timeout=1", sessionID), nil)
	c4.Params = gin.Params{{Key: "id", Value: sessionID}}

	h.PollStatus(c4)

	if w4.Code != http.StatusGone {
		t.Fatalf("second poll: expected 410, got %d: %s", w4.Code, w4.Body.String())
	}
}

func TestQRLogin_Confirm_ExpiredSession(t *testing.T) {
	h, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Create session then fast-forward TTL.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)

	var createResp map[string]any
	json.Unmarshal(w1.Body.Bytes(), &createResp)
	sessionID := createResp["session_id"].(string)

	// Expire the key.
	mr.FastForward(6 * time.Minute)

	// Try to confirm.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/qr-login/%s/confirm", sessionID), nil)
	c2.Params = gin.Params{{Key: "id", Value: sessionID}}
	c2.Set("account_id", int64(1))

	h.Confirm(c2)

	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestQRLogin_Confirm_DoubleConfirm(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Create session.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)

	var createResp map[string]any
	json.Unmarshal(w1.Body.Bytes(), &createResp)
	sessionID := createResp["session_id"].(string)

	// First confirm.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/qr-login/%s/confirm", sessionID), nil)
	c2.Params = gin.Params{{Key: "id", Value: sessionID}}
	c2.Set("account_id", int64(42))
	h.Confirm(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("first confirm: expected 200, got %d", w2.Code)
	}

	// Second confirm — should fail (409 Conflict).
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/qr-login/%s/confirm", sessionID), nil)
	c3.Params = gin.Params{{Key: "id", Value: sessionID}}
	c3.Set("account_id", int64(99))
	h.Confirm(c3)

	if w3.Code != http.StatusConflict {
		t.Fatalf("second confirm: expected 409, got %d: %s", w3.Code, w3.Body.String())
	}
}

// ─── Branch coverage: CreateSession ─────────────────────────────────────────

func TestQRLogin_CreateSession_RedisFailure(t *testing.T) {
	h, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Shut down Redis before the request arrives.
	mr.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_CreateSession_SessionIdIsHex64Chars(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	sid := resp["session_id"].(string)

	// 32 bytes → 64 hex characters.
	if len(sid) != 64 {
		t.Fatalf("session_id length: want 64, got %d", len(sid))
	}
	for _, ch := range sid {
		if !('0' <= ch && ch <= '9' || 'a' <= ch && ch <= 'f') {
			t.Fatalf("session_id not hex: %s", sid)
		}
	}
}

func TestQRLogin_CreateSession_QrUrlEmbedsSesssionId(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	sid := resp["session_id"].(string)
	qrURL := resp["qr_url"].(string)

	expected := fmt.Sprintf("lurus://qr-login/%s", sid)
	if qrURL != expected {
		t.Fatalf("qr_url mismatch: want %q, got %q", expected, qrURL)
	}
}

func TestQRLogin_CreateSession_ExpiresIn300(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["expires_in"].(float64) != 300 {
		t.Fatalf("expires_in: want 300, got %v", resp["expires_in"])
	}
}

// ─── Branch coverage: PollStatus ────────────────────────────────────────────

func TestQRLogin_PollStatus_EmptySessionId(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login//status", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}
	h.PollStatus(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_PollStatus_RedisError(t *testing.T) {
	h, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Close Redis to force an error.
	mr.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/someid/status?timeout=1", nil)
	c.Params = gin.Params{{Key: "id", Value: "someid"}}
	h.PollStatus(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_PollStatus_ConsumedStateDirectly(t *testing.T) {
	// Write a "consumed" state directly into Redis, bypassing handler logic.
	_, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	const sid = "deadbeefdeadbeef"
	key := "qr_login:" + sid
	data := `{"status":"consumed","account_id":0}`
	rdb.Set(context.Background(), key, data, 5*time.Minute)

	// Create a fresh handler pointing at the same miniredis.
	h := handler.NewQRLoginHandler(rdb, testSessionSecret)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=1", nil)
	c.Params = gin.Params{{Key: "id", Value: sid}}
	h.PollStatus(c)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_PollStatus_CorruptJsonInRedis(t *testing.T) {
	_, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	const sid = "corruptdata1234567890123456789012"
	key := "qr_login:" + sid
	rdb.Set(context.Background(), key, `{not valid json`, 5*time.Minute)

	h := handler.NewQRLoginHandler(rdb, testSessionSecret)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=1", nil)
	c.Params = gin.Params{{Key: "id", Value: sid}}
	h.PollStatus(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_PollStatus_EmptySecretFailsTokenIssue(t *testing.T) {
	// Build a handler with no session secret — IssueSessionToken should fail.
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	const sid = "aabbccddeeff00112233445566778899"
	key := "qr_login:" + sid

	// Pre-populate: session is already "confirmed" with account 99.
	data := `{"status":"confirmed","account_id":99}`
	rdb.Set(context.Background(), key, data, 5*time.Minute)

	// Handler with EMPTY secret — IssueSessionToken will return error.
	h := handler.NewQRLoginHandler(rdb, "")

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=1", nil)
	c.Params = gin.Params{{Key: "id", Value: sid}}
	h.PollStatus(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_PollStatus_TimeoutReturnsPendingAfterDeadline(t *testing.T) {
	// Verifies that when timeout=2 elapses with session still pending,
	// the handler returns 200 {"status":"pending"} rather than hanging forever.
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Create session.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)
	var cr map[string]any
	json.Unmarshal(w1.Body.Bytes(), &cr)
	sid := cr["session_id"].(string)

	start := time.Now()
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=2", nil)
	c2.Params = gin.Params{{Key: "id", Value: sid}}
	h.PollStatus(c2)
	elapsed := time.Since(start)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["status"] != "pending" {
		t.Fatalf("expected pending, got %v", resp["status"])
	}
	// Must have waited at least the timeout, but not too long.
	if elapsed < 2*time.Second {
		t.Fatalf("poll returned too quickly: %v", elapsed)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("poll hung too long: %v", elapsed)
	}
}

func TestQRLogin_PollStatus_TwoPollersBothConsumeRace(t *testing.T) {
	// Two concurrent PollStatus calls on a confirmed session:
	// only one should get the token (200), the other should get 410 Gone.
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	const sid = "raceconditiontest1234567890123456"
	key := "qr_login:" + sid
	data := `{"status":"confirmed","account_id":42}`
	rdb.Set(context.Background(), key, data, 5*time.Minute)

	h := handler.NewQRLoginHandler(rdb, testSessionSecret)
	gin.SetMode(gin.TestMode)

	type result struct {
		code int
	}
	ch := make(chan result, 2)

	for i := 0; i < 2; i++ {
		go func() {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=1", nil)
			c.Params = gin.Params{{Key: "id", Value: sid}}
			h.PollStatus(c)
			ch <- result{code: w.Code}
		}()
	}

	r1, r2 := <-ch, <-ch
	codes := []int{r1.code, r2.code}

	// Exactly one 200 and one 410.
	has200 := codes[0] == http.StatusOK || codes[1] == http.StatusOK
	has410 := codes[0] == http.StatusGone || codes[1] == http.StatusGone
	if !has200 || !has410 {
		t.Fatalf("expected one 200 and one 410, got %v and %v", codes[0], codes[1])
	}
}

// ─── Branch coverage: Confirm ────────────────────────────────────────────────

func TestQRLogin_Confirm_EmptySessionId(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/qr-login//confirm", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}
	c.Set("account_id", int64(1))
	h.Confirm(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_Confirm_RedisError(t *testing.T) {
	h, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	mr.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/someid/confirm", nil)
	c.Params = gin.Params{{Key: "id", Value: "someid"}}
	c.Set("account_id", int64(1))
	h.Confirm(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_Confirm_AlreadyConsumedReturnsConflict(t *testing.T) {
	_, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	const sid = "consumedsession123456789012345678"
	key := "qr_login:" + sid
	rdb.Set(context.Background(), key, `{"status":"consumed","account_id":7}`, 5*time.Minute)

	h := handler.NewQRLoginHandler(rdb, testSessionSecret)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/"+sid+"/confirm", nil)
	c.Params = gin.Params{{Key: "id", Value: sid}}
	c.Set("account_id", int64(99))
	h.Confirm(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestQRLogin_Confirm_NoAccountIdReturns401(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/someid/confirm", nil)
	c.Params = gin.Params{{Key: "id", Value: "someid"}}
	// Deliberately NOT setting account_id — simulates unauthenticated request.
	h.Confirm(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── Idempotency & session lifecycle ────────────────────────────────────────

func TestQRLogin_MultipleSessions_AreIndependent(t *testing.T) {
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Create two sessions.
	makeSession := func() string {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
		h.CreateSession(c)
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["session_id"].(string)
	}

	sid1 := makeSession()
	sid2 := makeSession()

	if sid1 == sid2 {
		t.Fatal("two sessions should have distinct IDs")
	}

	// Confirm session 1 only.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/"+sid1+"/confirm", nil)
	c.Params = gin.Params{{Key: "id", Value: sid1}}
	c.Set("account_id", int64(10))
	h.Confirm(c)

	// Session 2 must still be pending.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid2+"/status?timeout=1", nil)
	c2.Params = gin.Params{{Key: "id", Value: sid2}}
	h.PollStatus(c2)

	var r map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r)
	if r["status"] != "pending" {
		t.Fatalf("session2 should still be pending, got %v", r["status"])
	}
}

func TestQRLogin_SessionExpiry_KeyRemovedByTTL(t *testing.T) {
	h, mr := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Create session.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)
	var cr map[string]any
	json.Unmarshal(w1.Body.Bytes(), &cr)
	sid := cr["session_id"].(string)

	// Advance past TTL.
	mr.FastForward(6 * time.Minute)

	// Poll — should be 404.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=1", nil)
	c2.Params = gin.Params{{Key: "id", Value: sid}}
	h.PollStatus(c2)

	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after TTL, got %d", w2.Code)
	}
}

func TestQRLogin_IssuedToken_CanBeValidated(t *testing.T) {
	// Verify the token issued through the full flow validates correctly.
	h, _ := setupQRLoginTest(t)
	gin.SetMode(gin.TestMode)

	// Create.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/v1/public/qr-login/session", nil)
	h.CreateSession(c1)
	var cr map[string]any
	json.Unmarshal(w1.Body.Bytes(), &cr)
	sid := cr["session_id"].(string)

	// Confirm as account 77.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/"+sid+"/confirm", nil)
	c2.Params = gin.Params{{Key: "id", Value: sid}}
	c2.Set("account_id", int64(77))
	h.Confirm(c2)

	// Poll to get token.
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/v1/public/qr-login/"+sid+"/status?timeout=1", nil)
	c3.Params = gin.Params{{Key: "id", Value: sid}}
	h.PollStatus(c3)

	var pr map[string]any
	json.Unmarshal(w3.Body.Bytes(), &pr)
	token := pr["token"].(string)

	// Validate the token — must resolve to account 77.
	accountID, err := auth.ValidateSessionToken(token, testSessionSecret)
	if err != nil {
		t.Fatalf("token validation failed: %v", err)
	}
	if accountID != 77 {
		t.Fatalf("expected account 77, got %d", accountID)
	}
}
