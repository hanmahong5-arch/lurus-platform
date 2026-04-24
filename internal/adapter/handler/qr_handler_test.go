package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
)

// qrSecret must be ≥32 bytes — Config.Validate enforces this in prod.
const qrTestSecret = "qr-test-secret-must-be-at-least-32-bytes!!"

func setupQR(t *testing.T) (*handler.QRHandler, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	gin.SetMode(gin.TestMode)
	return handler.NewQRHandler(rdb, qrTestSecret), mr, rdb
}

func postJSON(method, path string, body any, params ...gin.Param) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *bytes.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	if len(params) > 0 {
		c.Params = append(c.Params, params...)
	}
	return c, w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v — body=%s", err, w.Body.String())
	}
	return out
}

// parsePayload extracts id, action, issued_at (t), and h (sig) from the
// lurus://qr payload URI.
func parsePayload(t *testing.T, payload string) (id, action, issuedAt, sig string) {
	t.Helper()
	if !strings.HasPrefix(payload, "lurus://qr?") {
		t.Fatalf("payload missing scheme: %q", payload)
	}
	q, err := url.ParseQuery(strings.TrimPrefix(payload, "lurus://qr?"))
	if err != nil {
		t.Fatalf("parse payload query: %v", err)
	}
	return q.Get("id"), q.Get("a"), q.Get("t"), q.Get("h")
}

// ── CreateSession ───────────────────────────────────────────────────────────

func TestQR_Create_Login_HappyPath(t *testing.T) {
	h, _, _ := setupQR(t)

	c, w := postJSON(http.MethodPost, "/api/v2/qr/session", map[string]any{"action": "login"})
	h.CreateSession(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	id, _ := resp["id"].(string)
	if len(id) != 64 {
		t.Fatalf("id should be 64 hex chars (256-bit); got %d: %q", len(id), id)
	}
	if resp["action"] != "login" {
		t.Errorf("action = %v, want login", resp["action"])
	}
	if resp["expires_in"].(float64) != 300 {
		t.Errorf("expires_in = %v, want 300", resp["expires_in"])
	}
	expiresAt, _ := resp["expires_at"].(string)
	if expiresAt == "" {
		t.Error("expires_at missing from response")
	} else if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		t.Errorf("expires_at not RFC3339: %q (%v)", expiresAt, err)
	}
	payload, _ := resp["qr_payload"].(string)
	gotID, gotAction, gotT, sig := parsePayload(t, payload)
	if gotID != id || gotAction != "login" || len(sig) != 24 {
		t.Errorf("payload mismatch: id=%q a=%q sig=%q", gotID, gotAction, sig)
	}
	if gotT == "" {
		t.Error("payload missing t= issued-at field")
	}
}

func TestQR_Create_InvalidJSON(t *testing.T) {
	h, _, _ := setupQR(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/qr/session", strings.NewReader("{not json"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateSession(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
	if decode(t, w)["error"] != "invalid_request" {
		t.Error("expected invalid_request error")
	}
}

func TestQR_Create_InvalidAction(t *testing.T) {
	h, _, _ := setupQR(t)
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session", map[string]any{"action": "hacker"})
	h.CreateSession(c)
	if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_action" {
		t.Fatalf("expected 400 invalid_action, got %d %s", w.Code, w.Body.String())
	}
}

func TestQR_Create_GatedActions(t *testing.T) {
	h, _, _ := setupQR(t)
	for _, action := range []string{"join_org", "delegate"} {
		t.Run(action, func(t *testing.T) {
			c, w := postJSON(http.MethodPost, "/api/v2/qr/session", map[string]any{"action": action})
			h.CreateSession(c)
			if w.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d; want 501", w.Code)
			}
			if decode(t, w)["error"] != "action_not_supported_yet" {
				t.Errorf("error = %v", decode(t, w)["error"])
			}
		})
	}
}

// ── PollStatus ──────────────────────────────────────────────────────────────

func TestQR_Poll_Pending(t *testing.T) {
	h, _, _ := setupQR(t)

	id := createLoginSession(t, h)

	c, w := postJSON(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=1", id), nil, gin.Param{Key: "id", Value: id})
	// Override URL so the timeout query is parsed by Gin.
	c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=1", id), nil)
	c.Params = gin.Params{{Key: "id", Value: id}}

	start := time.Now()
	h.PollStatus(c)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if decode(t, w)["status"] != "pending" {
		t.Errorf("status = %v, want pending", decode(t, w)["status"])
	}
	// Should honour the 1s timeout — give some slack for CI.
	if elapsed > 3*time.Second {
		t.Errorf("poll took %v, expected ~1s", elapsed)
	}
}

func TestQR_Poll_NotFound(t *testing.T) {
	h, _, _ := setupQR(t)

	c, w := postJSON(http.MethodGet, "/api/v2/qr/nonexistent/status?timeout=1", nil, gin.Param{Key: "id", Value: "nonexistent"})
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v2/qr/nonexistent/status?timeout=1", nil)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}

	h.PollStatus(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", w.Code)
	}
}

func TestQR_Poll_MissingID(t *testing.T) {
	h, _, _ := setupQR(t)
	c, w := postJSON(http.MethodGet, "/api/v2/qr//status", nil)
	h.PollStatus(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
}

// TestQR_Poll_ConcurrencyLimit verifies that PollStatus enforces the
// max-inflight semaphore: with a sema size of 2, three concurrent pollers
// must see exactly one 503 server_overloaded rejection.
func TestQR_Poll_ConcurrencyLimit(t *testing.T) {
	h, _, _ := setupQR(t)
	h = h.WithMaxInflightPolls(2)

	// Create a pending login session the pollers can target.
	id := createLoginSession(t, h)

	const concurrent = 3
	type result struct {
		code int
		body string
	}
	results := make(chan result, concurrent)

	// Fire `concurrent` pollers, each with a long-ish timeout so two of
	// them block holding the semaphore slots while the 3rd arrives.
	// The 3rd must immediately return 503 without waiting.
	start := make(chan struct{})
	for i := 0; i < concurrent; i++ {
		go func() {
			<-start
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=2", id), nil)
			c.Params = gin.Params{{Key: "id", Value: id}}
			h.PollStatus(c)
			results <- result{code: w.Code, body: w.Body.String()}
		}()
	}
	close(start)

	codes := make([]int, 0, concurrent)
	deadline := time.After(5 * time.Second)
	for i := 0; i < concurrent; i++ {
		select {
		case r := <-results:
			codes = append(codes, r.code)
		case <-deadline:
			t.Fatalf("timed out waiting for pollers; got %d responses", len(codes))
		}
	}

	// Exactly one 503 should appear; the other two are either 200
	// (pending timeout) or 410/404 depending on interleaving — any
	// non-503 code proves the poller held a slot.
	var overloaded int
	for _, c := range codes {
		if c == http.StatusServiceUnavailable {
			overloaded++
		}
	}
	if overloaded != 1 {
		t.Fatalf("expected exactly 1 server_overloaded 503, got %d (codes=%v)", overloaded, codes)
	}
}

// ── Confirm ─────────────────────────────────────────────────────────────────

func TestQR_Confirm_HappyPath(t *testing.T) {
	h, _, _ := setupQR(t)

	id, sig, issuedAt := createLoginSessionWithSig(t, h)

	c, w := postJSON(http.MethodPost, fmt.Sprintf("/api/v2/qr/%s/confirm", id),
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	c.Set("account_id", int64(42))

	h.Confirm(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	if resp["confirmed"] != true {
		t.Errorf("confirmed = %v, want true", resp["confirmed"])
	}
	if resp["action"] != "login" {
		t.Errorf("action = %v, want login", resp["action"])
	}
}

func TestQR_Confirm_Unauthenticated(t *testing.T) {
	h, _, _ := setupQR(t)
	id, sig, issuedAt := createLoginSessionWithSig(t, h)
	c, w := postJSON(http.MethodPost, fmt.Sprintf("/api/v2/qr/%s/confirm", id),
		map[string]any{"sig": sig, "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	// Note: no account_id set → requireAccountID should 401.
	h.Confirm(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", w.Code)
	}
}

func TestQR_Confirm_InvalidBody(t *testing.T) {
	h, _, _ := setupQR(t)
	id, _, _ := createLoginSessionWithSig(t, h)
	c, w := postJSON(http.MethodPost, fmt.Sprintf("/api/v2/qr/%s/confirm", id),
		map[string]any{},
		gin.Param{Key: "id", Value: id},
	)
	c.Set("account_id", int64(42))
	h.Confirm(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
}

func TestQR_Confirm_MissingID(t *testing.T) {
	h, _, _ := setupQR(t)
	c, w := postJSON(http.MethodPost, "/api/v2/qr//confirm", map[string]any{"sig": "x"})
	c.Set("account_id", int64(42))
	h.Confirm(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
}

func TestQR_Confirm_WrongSignature(t *testing.T) {
	h, _, _ := setupQR(t)
	id, _, issuedAt := createLoginSessionWithSig(t, h)

	c, w := postJSON(http.MethodPost, fmt.Sprintf("/api/v2/qr/%s/confirm", id),
		map[string]any{"sig": "0000000000000000", "t": issuedAt},
		gin.Param{Key: "id", Value: id},
	)
	c.Set("account_id", int64(42))

	h.Confirm(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
	if decode(t, w)["error"] != "invalid_signature" {
		t.Errorf("error = %v", decode(t, w)["error"])
	}
}

func TestQR_Confirm_SessionNotFound(t *testing.T) {
	h, _, _ := setupQR(t)
	c, w := postJSON(http.MethodPost, "/api/v2/qr/ghost/confirm",
		map[string]any{"sig": "deadbeefdeadbeef"},
		gin.Param{Key: "id", Value: "ghost"},
	)
	c.Set("account_id", int64(42))
	h.Confirm(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", w.Code)
	}
}

func TestQR_Confirm_DoubleConfirm_Conflict(t *testing.T) {
	h, _, _ := setupQR(t)
	id, sig, issuedAt := createLoginSessionWithSig(t, h)

	// First confirm succeeds.
	c1, _ := postJSON(http.MethodPost, "/", map[string]any{"sig": sig, "t": issuedAt}, gin.Param{Key: "id", Value: id})
	c1.Set("account_id", int64(42))
	h.Confirm(c1)

	// Second confirm should 409.
	c2, w2 := postJSON(http.MethodPost, "/", map[string]any{"sig": sig, "t": issuedAt}, gin.Param{Key: "id", Value: id})
	c2.Set("account_id", int64(7))
	h.Confirm(c2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("second confirm status = %d; want 409", w2.Code)
	}
}

// ── End-to-end: login flow ──────────────────────────────────────────────────

func TestQR_LoginFlow_CreateConfirmPoll(t *testing.T) {
	h, _, _ := setupQR(t)

	// 1. Create
	cCreate, wCreate := postJSON(http.MethodPost, "/api/v2/qr/session", map[string]any{"action": "login"})
	h.CreateSession(cCreate)
	create := decode(t, wCreate)
	id := create["id"].(string)
	_, _, tStr, sig := parsePayload(t, create["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)

	// 2. Confirm (acting as account 42)
	cConfirm, wConfirm := postJSON(http.MethodPost, "/", map[string]any{"sig": sig, "t": issuedAt}, gin.Param{Key: "id", Value: id})
	cConfirm.Set("account_id", int64(42))
	h.Confirm(cConfirm)
	if wConfirm.Code != http.StatusOK {
		t.Fatalf("confirm failed: %d %s", wConfirm.Code, wConfirm.Body.String())
	}

	// 3. Poll — should now return token
	cPoll, wPoll := postJSON(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=1", id), nil, gin.Param{Key: "id", Value: id})
	cPoll.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=1", id), nil)
	cPoll.Params = gin.Params{{Key: "id", Value: id}}
	h.PollStatus(cPoll)

	if wPoll.Code != http.StatusOK {
		t.Fatalf("poll status = %d (body=%s)", wPoll.Code, wPoll.Body.String())
	}
	poll := decode(t, wPoll)
	if poll["status"] != "confirmed" {
		t.Errorf("poll status = %v; want confirmed", poll["status"])
	}
	token, _ := poll["token"].(string)
	if token == "" {
		t.Fatal("poll response missing token")
	}
	if poll["action"] != "login" {
		t.Errorf("action = %v", poll["action"])
	}

	// 4. Second poll should be 410 (already consumed).
	cPoll2, wPoll2 := postJSON(http.MethodGet, "/", nil, gin.Param{Key: "id", Value: id})
	cPoll2.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=1", id), nil)
	cPoll2.Params = gin.Params{{Key: "id", Value: id}}
	h.PollStatus(cPoll2)
	if wPoll2.Code != http.StatusGone {
		t.Fatalf("second poll status = %d; want 410", wPoll2.Code)
	}
}

// ── Expiry / TTL ────────────────────────────────────────────────────────────

func TestQR_Poll_AfterExpiry(t *testing.T) {
	h, mr, _ := setupQR(t)
	id := createLoginSession(t, h)

	// Fast-forward past TTL.
	mr.FastForward(6 * time.Minute)

	cPoll, wPoll := postJSON(http.MethodGet, "/", nil, gin.Param{Key: "id", Value: id})
	cPoll.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v2/qr/%s/status?timeout=1", id), nil)
	cPoll.Params = gin.Params{{Key: "id", Value: id}}
	h.PollStatus(cPoll)

	if wPoll.Code != http.StatusNotFound {
		t.Fatalf("poll after expiry = %d; want 404", wPoll.Code)
	}
}

// ── Metadata capture (IP / UA) ──────────────────────────────────────────────

func TestQR_Create_CapturesForwardedIPAndTruncatesUA(t *testing.T) {
	h, _, rdb := setupQR(t)

	longUA := strings.Repeat("X", 1024) // will be truncated to 256
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]any{"action": "login"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/qr/session", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.99")
	c.Request.Header.Set("User-Agent", longUA)

	h.CreateSession(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	id := decode(t, w)["id"].(string)

	raw, err := rdb.Get(context.Background(), "qr:"+id).Bytes()
	if err != nil {
		t.Fatalf("get stored session: %v", err)
	}
	var stored map[string]any
	_ = json.Unmarshal(raw, &stored)
	if stored["ip"] != "203.0.113.99" {
		t.Errorf("ip = %v, want X-Forwarded-For value", stored["ip"])
	}
	if ua, _ := stored["ua"].(string); len(ua) != 256 {
		t.Errorf("ua len = %d, want 256 (truncated)", len(ua))
	}
}

// ── B5: signed timestamp replay protection ─────────────────────────────────

// TestQR_Confirm_RejectsExpiredTimestamp ensures that even if a Redis record
// somehow survives its TTL (or a screenshot is replayed with the original t),
// the signed-timestamp check rejects the request.
func TestQR_Confirm_RejectsExpiredTimestamp(t *testing.T) {
	h, _, _ := setupQR(t)
	id, sig, issuedAt := createLoginSessionWithSig(t, h)

	// Build the "correct" HMAC for a t that's 6 minutes in the past (> TTL 5m
	// + 30s skew). This isolates the timestamp-window check from the HMAC
	// check: the signature itself is valid for that t, but t is stale so the
	// window check must reject it first.
	stale := issuedAt - int64((6 * time.Minute).Seconds())
	staleSig := currentHMACSig(qrTestSecret, id, "login", stale)
	_ = sig // fresh sig not used here — we want a matching-MAC-but-stale-t case
	c, w := postJSON(http.MethodPost, fmt.Sprintf("/api/v2/qr/%s/confirm", id),
		map[string]any{"sig": staleSig, "t": stale},
		gin.Param{Key: "id", Value: id},
	)
	c.Set("account_id", int64(42))
	h.Confirm(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (expired/invalid)", w.Code)
	}
	if decode(t, w)["error"] != "invalid_signature" {
		t.Errorf("error = %v; want invalid_signature", decode(t, w)["error"])
	}
}

// TestQR_Confirm_AcceptsLegacyNoTimestamp exercises the backward-compat path:
// an APP build that predates B5 will not include `t` in the confirm body and
// will send the legacy id|action HMAC. The server must fall back gracefully
// (and log a warning), not break old clients.
func TestQR_Confirm_AcceptsLegacyNoTimestamp(t *testing.T) {
	h, _, _ := setupQR(t)
	id, _, _ := createLoginSessionWithSig(t, h)

	// Compute the legacy HMAC(id|action) using a tiny duplicate of the server
	// algorithm so we're independent of the handler's internals.
	legacySig := legacyHMACSig(qrTestSecret, id, "login")

	c, w := postJSON(http.MethodPost, fmt.Sprintf("/api/v2/qr/%s/confirm", id),
		// Intentionally omit "t" to trigger the legacy path.
		map[string]any{"sig": legacySig},
		gin.Param{Key: "id", Value: id},
	)
	c.Set("account_id", int64(42))
	h.Confirm(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s); legacy path should accept", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	if resp["confirmed"] != true {
		t.Errorf("confirmed = %v, want true", resp["confirmed"])
	}
}

// legacyHMACSig reproduces the pre-B5 id|action HMAC used only by the
// backward-compat test above. Kept tiny and self-contained.
func legacyHMACSig(secret, id, action string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(action))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// currentHMACSig reproduces the B5 id|action|t HMAC used by the
// expired-timestamp test to synthesize a "valid MAC but stale t" input.
// Domain-separator byte (0x01) + 24-hex truncation must match handler.hmacHex.
func currentHMACSig(secret, id, action string, issuedAt int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte{0x01})
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(action))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(issuedAt, 10)))
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

// ── helpers ─────────────────────────────────────────────────────────────────

func createLoginSession(t *testing.T, h *handler.QRHandler) string {
	t.Helper()
	id, _, _ := createLoginSessionWithSig(t, h)
	return id
}

func createLoginSessionWithSig(t *testing.T, h *handler.QRHandler) (id, sig string, issuedAt int64) {
	t.Helper()
	c, w := postJSON(http.MethodPost, "/api/v2/qr/session", map[string]any{"action": "login"})
	h.CreateSession(c)
	if w.Code != http.StatusOK {
		t.Fatalf("create session failed: %d %s", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	id = resp["id"].(string)
	_, _, tStr, sig := parsePayload(t, resp["qr_payload"].(string))
	if tStr != "" {
		if v, err := strconv.ParseInt(tStr, 10, 64); err == nil {
			issuedAt = v
		}
	}
	return id, sig, issuedAt
}

// ── Keyring rotation (R1.2) ────────────────────────────────────────────────

// TestQR_Keyring_Rotation_OldKeyStillValid proves that a QR minted while the
// ring holds {k1} can be verified by a pod holding {k1, k2}, and that a QR
// minted by {k1, k2} (current=k2) fails against a pod that has rotated to
// {k2} only. This is the mid-rotation invariant we rely on: add a new key,
// wait ≥TTL, then drop the old one.
func TestQR_Keyring_Rotation_OldKeyStillValid(t *testing.T) {
	const (
		k1 = "00000000000000000000000000000001aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		k2 = "99999999999999999999999999999999bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	gin.SetMode(gin.TestMode)

	hOld := handler.NewQRHandlerWithKeyring(rdb, qrTestSecret, "k1:"+k1)           // pre-rotation
	hMid := handler.NewQRHandlerWithKeyring(rdb, qrTestSecret, "k1:"+k1+",k2:"+k2) // during
	hNew := handler.NewQRHandlerWithKeyring(rdb, qrTestSecret, "k2:"+k2)           // post

	// 1. Old handler mints → sig signed under k1. Mid handler (has k1+k2) accepts.
	id1, sig1, t1 := mintAndExtractPayload(t, hOld)
	if !confirmAccepts(t, hMid, id1, sig1, t1) {
		t.Error("mid-rotation pod must accept sig signed under k1 (still in ring)")
	}

	// 2. Old handler mints fresh session. New handler (only k2) rejects it.
	id2, sig2, t2 := mintAndExtractPayload(t, hOld)
	if confirmAccepts(t, hNew, id2, sig2, t2) {
		t.Error("post-rotation pod must REJECT sig signed under removed k1")
	}

	// 3. Inverse: sig signed by k2 (current on hNew) verifies on hMid.
	id3, sig3, t3 := mintAndExtractPayload(t, hNew)
	if !confirmAccepts(t, hMid, id3, sig3, t3) {
		t.Error("mid-rotation pod must accept sig signed under k2")
	}
}

// TestQR_Keyring_MalformedSpec_Panics catches config-drift cases where the
// env var value is syntactically invalid. Better to fail at boot than at
// first user request.
func TestQR_Keyring_MalformedSpec_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on malformed QR_SIGNING_KEYS")
		}
	}()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	_ = handler.NewQRHandlerWithKeyring(rdb, qrTestSecret, "no-colon-anywhere")
}

// mintAndExtractPayload creates a session via the given handler and returns
// (id, sig, issuedAt) extracted from the QR payload string. Caller can then
// feed this triple to any other handler to assert whether its keyring accepts
// the signature.
func mintAndExtractPayload(t *testing.T, h *handler.QRHandler) (string, string, int64) {
	t.Helper()
	c, w := postJSON("POST", "/api/v2/qr/session", map[string]any{"action": "login"})
	h.CreateSession(c)
	if w.Code != 200 {
		t.Fatalf("create session: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id, _, tStr, sig := parsePayload(t, resp["qr_payload"].(string))
	issuedAt, _ := strconv.ParseInt(tStr, 10, 64)
	return id, sig, issuedAt
}

// confirmAccepts posts to Confirm and returns true iff the handler returned
// 200. account_id is injected directly (bypassing JWT middleware in tests).
func confirmAccepts(t *testing.T, h *handler.QRHandler, id, sig string, tUnix int64) bool {
	t.Helper()
	c, w := postJSON("POST", "/api/v2/qr/"+id+"/confirm",
		map[string]any{"sig": sig, "t": tUnix},
		gin.Param{Key: "id", Value: id},
	)
	c.Set("account_id", int64(42))
	h.Confirm(c)
	return w.Code == 200
}

var _ = context.Background
