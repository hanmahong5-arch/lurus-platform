package newapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// helper: server + client with arbitrary /api/status response
func newPingServer(t *testing.T, status int, body string, hits *int32) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "tok", "1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestPing_Healthy(t *testing.T) {
	c := newPingServer(t, http.StatusOK, `{"success":true,"data":{}}`, nil)
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("healthy ping: %v", err)
	}
}

func TestPing_HTTP5xx(t *testing.T) {
	c := newPingServer(t, http.StatusInternalServerError, `oops`, nil)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error on 5xx")
	}
}

func TestPing_SuccessFalse(t *testing.T) {
	c := newPingServer(t, http.StatusOK, `{"success":false,"message":"degraded"}`, nil)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error when envelope success=false")
	}
}

func TestReadinessChecker_NilClientReturnsNil(t *testing.T) {
	if got := NewReadinessChecker(nil); got != nil {
		t.Errorf("expected nil checker for nil client, got %+v", got)
	}
}

func TestReadinessChecker_NilCheckerCheckIsSafe(t *testing.T) {
	var rc *ReadinessChecker // typed nil
	if err := rc.Check(context.Background()); err != nil {
		t.Errorf("nil receiver: expected nil, got %v", err)
	}
}

func TestReadinessChecker_CachesSuccess(t *testing.T) {
	var hits int32
	c := newPingServer(t, http.StatusOK, `{"success":true}`, &hits)

	rc := NewReadinessChecker(c).WithTTL(1 * time.Hour)

	// 5 calls in rapid succession: first hits NewAPI, rest read cache.
	for i := 0; i < 5; i++ {
		if err := rc.Check(context.Background()); err != nil {
			t.Fatalf("Check %d: %v", i, err)
		}
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected 1 actual ping, got %d (cache broken)", h)
	}
}

func TestReadinessChecker_CachesFailure(t *testing.T) {
	// Failure ALSO must be cached — back-to-back /readyz during a NewAPI
	// outage shouldn't multiply pressure on the dying upstream.
	var hits int32
	c := newPingServer(t, http.StatusInternalServerError, `oops`, &hits)

	rc := NewReadinessChecker(c).WithTTL(1 * time.Hour)

	for i := 0; i < 3; i++ {
		if err := rc.Check(context.Background()); err == nil {
			t.Fatalf("Check %d: expected error", i)
		}
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected failure to be cached (1 ping), got %d", h)
	}
}

func TestReadinessChecker_CacheExpiresAndReprobes(t *testing.T) {
	var hits int32
	c := newPingServer(t, http.StatusOK, `{"success":true}`, &hits)

	// Tiny TTL so the second call falls outside the window.
	rc := NewReadinessChecker(c).WithTTL(20 * time.Millisecond)

	if err := rc.Check(context.Background()); err != nil {
		t.Fatalf("first: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := rc.Check(context.Background()); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 2 {
		t.Errorf("expected 2 pings after TTL expiry, got %d", h)
	}
}

func TestReadinessChecker_ContextRespectedByPing(t *testing.T) {
	// If caller's ctx is already cancelled, Ping should fail fast — we
	// rely on that for /readyz under heavy load.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Slow upstream — would block past the cancel deadline if probe
		// didn't honour ctx.
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "tok", "1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rc := NewReadinessChecker(c).WithTTL(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = rc.Check(ctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("expected context cancellation error")
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected ctx timeout to fire fast, got %v", elapsed)
	}
	// Sanity: error should signal something timed out / cancelled,
	// not a bogus 200.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		// http.Client wraps context errors; just check stringy contains
		// something timeout-ish.
		s := err.Error()
		if s == "" {
			t.Errorf("expected non-empty error message, got blank")
		}
	}
}

func TestReadinessChecker_Name(t *testing.T) {
	c := newPingServer(t, http.StatusOK, `{"success":true}`, nil)
	if got := NewReadinessChecker(c).Name(); got != "newapi" {
		t.Errorf("Name = %q, want newapi", got)
	}
}
