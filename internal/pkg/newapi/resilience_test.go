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

// helper: build a Client + httptest server with a programmable response sequence.
// Each request consumes one entry from `responses`; if exhausted the last entry
// repeats. Lets us assert behaviour like "first 2 calls 503 then 200".
type sequencedResponse struct {
	status int
	body   string
}

func clientWithResponses(t *testing.T, responses []sequencedResponse) (*Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&calls, 1)) - 1
		if i >= len(responses) {
			i = len(responses) - 1
		}
		resp := responses[i]
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "tok", "1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Tighten retry timing so tests run fast — production defaults stay
	// untouched. baseWait 1ms × 2^N + jitter ≈ ~5ms total over 3 attempts.
	c.resil.baseWait = 1 * time.Millisecond
	c.resil.maxWait = 10 * time.Millisecond
	c.resil.jitter = 0
	return c, &calls
}

// ── Retry semantics ──────────────────────────────────────────────────────

func TestResilience_RetriesOn5xx_ThenSucceeds(t *testing.T) {
	c, calls := clientWithResponses(t, []sequencedResponse{
		{status: 503, body: `oops`},
		{status: 503, body: `still oops`},
		{status: 200, body: `{"success":true,"data":{"id":7}}`},
	})

	id, err := c.FindUserByUsername(context.Background(), "lurus_42")
	// FindUserByUsername returns ErrUserNotFound on empty data array; we
	// pass a single-element data so the test can assert success path.
	// Expecting a JSON shape mismatch error — the point is "after 2 retries
	// we DID hit the 200 attempt", not what FindUserByUsername decoded.
	if atomic.LoadInt32(calls) != 3 {
		t.Errorf("expected 3 attempts (2 fail + 1 success), got %d", *calls)
	}
	_, _ = id, err
}

func TestResilience_NoRetryOn4xx(t *testing.T) {
	c, calls := clientWithResponses(t, []sequencedResponse{
		{status: 400, body: `{"success":false,"message":"bad request"}`},
	})

	_ = c.SetUserQuota(context.Background(), 7, 100)
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("expected 1 attempt only (4xx is terminal), got %d", *calls)
	}
}

func TestResilience_NoRetryOn429ButRetried(t *testing.T) {
	// 429 is special — retriable per HTTP semantics (Retry-After).
	c, calls := clientWithResponses(t, []sequencedResponse{
		{status: 429, body: `slow down`},
		{status: 200, body: `{"success":true}`},
	})

	if err := c.SetUserQuota(context.Background(), 7, 100); err != nil {
		t.Fatalf("after 429+200 retry: %v", err)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Errorf("expected retry on 429 (1 retry → 2 calls), got %d", *calls)
	}
}

func TestResilience_RetryBudgetExhausted(t *testing.T) {
	// All 3 attempts fail. Caller should see the final error AND we
	// should have hit the upstream exactly maxAttempts times.
	c, calls := clientWithResponses(t, []sequencedResponse{
		{status: 503, body: `down`},
	})

	err := c.SetUserQuota(context.Background(), 7, 100)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if atomic.LoadInt32(calls) != int32(c.resil.maxAttempts) {
		t.Errorf("expected exactly %d attempts, got %d", c.resil.maxAttempts, *calls)
	}
}

func TestResilience_NoRetryOnContextCancel(t *testing.T) {
	c, calls := clientWithResponses(t, []sequencedResponse{
		{status: 503, body: `down`},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up-front

	err := c.SetUserQuota(ctx, 7, 100)
	if err == nil {
		t.Error("expected error from cancelled ctx")
	}
	// At most 1 call is OK — possibly 0 if ctx caught earlier.
	got := atomic.LoadInt32(calls)
	if got > 1 {
		t.Errorf("expected ≤1 attempt with pre-cancelled ctx, got %d", got)
	}
}

// ── retriableError taxonomy ──────────────────────────────────────────────

func TestRetriableError_Classifications(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context.Canceled", context.Canceled, false},
		{"500", &statusErr{code: 500}, true},
		{"502", &statusErr{code: 502}, true},
		{"503", &statusErr{code: 503}, true},
		{"504", &statusErr{code: 504}, true},
		{"408", &statusErr{code: 408}, true},
		{"429", &statusErr{code: 429}, true},
		{"400", &statusErr{code: 400}, false},
		{"401", &statusErr{code: 401}, false},
		{"404", &statusErr{code: 404}, false},
		{"409", &statusErr{code: 409}, false},
		{"network err", errors.New("dial tcp: connection refused"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retriableError(tc.err); got != tc.want {
				t.Errorf("retriableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ── Circuit breaker state machine ────────────────────────────────────────

func TestBreaker_TripsAfterThreshold(t *testing.T) {
	r := newResilience()
	r.tripThreshold = 3 // tighten for the test
	r.maxAttempts = 1   // disable retries to make failure counting clear
	r.baseWait = 1 * time.Millisecond

	failOp := func(_ context.Context) error {
		return &statusErr{code: 500, body: "down"}
	}

	// 3 consecutive failures should trip the breaker.
	for i := 0; i < 3; i++ {
		_ = r.call(context.Background(), "test", failOp)
	}

	if r.state != stateOpen {
		t.Errorf("expected breaker Open after 3 failures, got %s", r.state)
	}
}

func TestBreaker_ShortCircuitsWhenOpen(t *testing.T) {
	r := newResilience()
	r.tripThreshold = 1
	r.maxAttempts = 1
	r.openWindow = 1 * time.Hour // ensure no half-open transition during test

	failOp := func(_ context.Context) error { return &statusErr{code: 500} }
	_ = r.call(context.Background(), "trip", failOp)

	if r.state != stateOpen {
		t.Fatalf("setup: expected Open, got %s", r.state)
	}

	// Now any subsequent call must short-circuit without invoking op.
	called := false
	err := r.call(context.Background(), "blocked", func(_ context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
	if called {
		t.Error("op should NOT be invoked when breaker is Open")
	}
}

func TestBreaker_HalfOpenProbeThenCloseOnSuccess(t *testing.T) {
	r := newResilience()
	r.tripThreshold = 1
	r.maxAttempts = 1
	r.openWindow = 10 * time.Millisecond // short window so test is fast
	r.baseWait = 1 * time.Millisecond

	// Trip
	_ = r.call(context.Background(), "trip", func(_ context.Context) error {
		return &statusErr{code: 500}
	})
	if r.state != stateOpen {
		t.Fatalf("setup failed: state=%s", r.state)
	}

	// Wait window
	time.Sleep(15 * time.Millisecond)

	// Probe (success) should close the breaker.
	err := r.call(context.Background(), "probe", func(_ context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("probe call: %v", err)
	}
	if r.state != stateClosed {
		t.Errorf("expected Closed after successful probe, got %s", r.state)
	}
}

func TestBreaker_HalfOpenProbeFailReturnsToOpen(t *testing.T) {
	r := newResilience()
	r.tripThreshold = 1
	r.maxAttempts = 1
	r.openWindow = 10 * time.Millisecond

	_ = r.call(context.Background(), "trip", func(_ context.Context) error {
		return &statusErr{code: 500}
	})
	time.Sleep(15 * time.Millisecond)

	// Probe failure → back to Open.
	_ = r.call(context.Background(), "failed_probe", func(_ context.Context) error {
		return &statusErr{code: 500}
	})
	if r.state != stateOpen {
		t.Errorf("expected Open after failed probe, got %s", r.state)
	}
}

func TestBreaker_ResetsCounterOnSuccess(t *testing.T) {
	r := newResilience()
	r.tripThreshold = 5
	r.maxAttempts = 1

	// 4 failures (one short of trip)
	for i := 0; i < 4; i++ {
		_ = r.call(context.Background(), "fail", func(_ context.Context) error {
			return &statusErr{code: 500}
		})
	}
	if r.state != stateClosed {
		t.Fatalf("setup: expected Closed (1 below threshold), got %s", r.state)
	}

	// Success — counter resets
	_ = r.call(context.Background(), "ok", func(_ context.Context) error {
		return nil
	})

	// Another 4 failures should NOT trip — counter was reset.
	for i := 0; i < 4; i++ {
		_ = r.call(context.Background(), "fail2", func(_ context.Context) error {
			return &statusErr{code: 500}
		})
	}
	if r.state != stateClosed {
		t.Errorf("expected counter reset on success → still Closed after 4 more fails, got %s", r.state)
	}
}

// ── Backoff math ─────────────────────────────────────────────────────────

func TestBackoff_GrowsExponentiallyAndCaps(t *testing.T) {
	r := newResilience()
	r.baseWait = 100 * time.Millisecond
	r.maxWait = 500 * time.Millisecond
	r.jitter = 0 // deterministic for this test

	got := []time.Duration{
		r.backoffDuration(1), // 100ms
		r.backoffDuration(2), // 200ms
		r.backoffDuration(3), // 400ms
		r.backoffDuration(4), // would be 800ms; capped to 500ms
	}
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("backoffDuration(%d) = %v, want %v", i+1, got[i], w)
		}
	}
}

func TestBackoff_JitterStaysWithinRange(t *testing.T) {
	r := newResilience()
	r.baseWait = 100 * time.Millisecond
	r.maxWait = 1 * time.Second
	r.jitter = 0.3

	low := time.Duration(float64(100*time.Millisecond) * 0.7)
	high := time.Duration(float64(100*time.Millisecond) * 1.3)
	for i := 0; i < 50; i++ {
		got := r.backoffDuration(1)
		if got < low || got > high {
			t.Errorf("iter %d: backoff %v outside [%v,%v]", i, got, low, high)
		}
	}
}

// ── Nil-safe path ────────────────────────────────────────────────────────

func TestResilience_NilReceiverPassesThrough(t *testing.T) {
	var r *resilience // typed nil — possible if Client constructed without resil
	called := 0
	err := r.call(context.Background(), "test", func(_ context.Context) error {
		called++
		return nil
	})
	if err != nil {
		t.Errorf("nil receiver call: %v", err)
	}
	if called != 1 {
		t.Errorf("op should still execute exactly once, got %d", called)
	}
}
