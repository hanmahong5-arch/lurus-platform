package ratelimit

import (
	"testing"
	"time"
)

func newTestAuthLimiter() *AuthLimiter {
	return NewAuthLimiter(AuthLimiterConfig{
		MaxAttempts:     3,
		WindowDuration:  1 * time.Second,
		LockoutDuration: 2 * time.Second,
		PruneInterval:   10 * time.Second, // long to prevent interference
	})
}

func TestAuthLimiter_AllowsWithinLimit(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	for i := 0; i < 2; i++ {
		r := l.Check("10.0.0.1", AuthScopePassword)
		if !r.Allowed {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.RecordFailure("10.0.0.1", AuthScopePassword)
		time.Sleep(time.Millisecond) // ensure unique timestamps
	}
}

func TestAuthLimiter_LocksOutAfterMaxAttempts(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	for i := 0; i < 3; i++ {
		l.RecordFailure("10.0.0.2", AuthScopePassword)
		time.Sleep(time.Millisecond)
	}

	r := l.Check("10.0.0.2", AuthScopePassword)
	if r.Allowed {
		t.Fatal("should be locked out after 3 failures")
	}
	if r.RetryAfterMs <= 0 {
		t.Errorf("RetryAfterMs = %d, want > 0", r.RetryAfterMs)
	}
}

func TestAuthLimiter_LockoutExpires(t *testing.T) {
	l := NewAuthLimiter(AuthLimiterConfig{
		MaxAttempts:     2,
		WindowDuration:  5 * time.Second,
		LockoutDuration: 100 * time.Millisecond, // short for test
		PruneInterval:   10 * time.Second,
	})
	defer l.Stop()

	l.RecordFailure("10.0.0.3", AuthScopePassword)
	time.Sleep(time.Millisecond)
	l.RecordFailure("10.0.0.3", AuthScopePassword)

	r := l.Check("10.0.0.3", AuthScopePassword)
	if r.Allowed {
		t.Fatal("should be locked out")
	}

	time.Sleep(150 * time.Millisecond) // wait for lockout to expire

	r = l.Check("10.0.0.3", AuthScopePassword)
	if !r.Allowed {
		t.Fatal("lockout should have expired")
	}
}

func TestAuthLimiter_SuccessResetsCounter(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	l.RecordFailure("10.0.0.4", AuthScopePassword)
	time.Sleep(time.Millisecond)
	l.RecordFailure("10.0.0.4", AuthScopePassword)

	r := l.Check("10.0.0.4", AuthScopePassword)
	if r.Remaining != 1 {
		t.Errorf("Remaining = %d, want 1", r.Remaining)
	}

	l.RecordSuccess("10.0.0.4", AuthScopePassword)

	r = l.Check("10.0.0.4", AuthScopePassword)
	if r.Remaining != 3 {
		t.Errorf("after success, Remaining = %d, want 3 (reset)", r.Remaining)
	}
}

func TestAuthLimiter_ScopesIndependent(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	// Fill up password scope.
	for i := 0; i < 3; i++ {
		l.RecordFailure("10.0.0.5", AuthScopePassword)
		time.Sleep(time.Millisecond)
	}

	// Password should be locked.
	r := l.Check("10.0.0.5", AuthScopePassword)
	if r.Allowed {
		t.Fatal("password scope should be locked")
	}

	// API key scope should still be allowed.
	r = l.Check("10.0.0.5", AuthScopeAPIKey)
	if !r.Allowed {
		t.Fatal("api_key scope should be independent and allowed")
	}
}

func TestAuthLimiter_DifferentIPsIndependent(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	for i := 0; i < 3; i++ {
		l.RecordFailure("10.0.0.6", AuthScopePassword)
		time.Sleep(time.Millisecond)
	}

	r := l.Check("10.0.0.7", AuthScopePassword)
	if !r.Allowed {
		t.Fatal("different IP should be independent")
	}
}

func TestAuthLimiter_LoopbackExempt(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	// Fill up failures for loopback.
	for i := 0; i < 10; i++ {
		l.RecordFailure("127.0.0.1", AuthScopePassword)
	}

	// Loopback should always be allowed.
	r := l.Check("127.0.0.1", AuthScopePassword)
	if !r.Allowed {
		t.Fatal("loopback should be exempt from rate limiting")
	}
}

func TestAuthLimiter_IPv6LoopbackExempt(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	for i := 0; i < 10; i++ {
		l.RecordFailure("::1", AuthScopePassword)
	}

	r := l.Check("::1", AuthScopePassword)
	if !r.Allowed {
		t.Fatal("IPv6 loopback should be exempt")
	}
}

func TestAuthLimiter_IPv4MappedIPv6Normalized(t *testing.T) {
	l := newTestAuthLimiter()
	defer l.Stop()

	// Record failures via IPv4-mapped IPv6.
	for i := 0; i < 3; i++ {
		l.RecordFailure("::ffff:10.0.0.8", AuthScopePassword)
		time.Sleep(time.Millisecond)
	}

	// Check via plain IPv4 — should see the same counter.
	r := l.Check("10.0.0.8", AuthScopePassword)
	if r.Allowed {
		t.Fatal("IPv4-mapped IPv6 should be normalized to IPv4")
	}
}

func TestAuthLimiter_WindowExpiry(t *testing.T) {
	l := NewAuthLimiter(AuthLimiterConfig{
		MaxAttempts:     3,
		WindowDuration:  100 * time.Millisecond, // very short
		LockoutDuration: 5 * time.Second,
		PruneInterval:   10 * time.Second,
	})
	defer l.Stop()

	l.RecordFailure("10.0.0.9", AuthScopePassword)
	l.RecordFailure("10.0.0.9", AuthScopePassword)

	time.Sleep(150 * time.Millisecond) // wait for window to expire

	// Old failures should have fallen out of the window.
	r := l.Check("10.0.0.9", AuthScopePassword)
	if r.Remaining != 3 {
		t.Errorf("Remaining = %d, want 3 (old attempts should be expired)", r.Remaining)
	}
}

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"10.0.0.1", "10.0.0.1"},
		{"::ffff:10.0.0.1", "10.0.0.1"},
		{"::1", "::1"},
		{"2001:db8::1", "2001:db8::1"},
	}
	for _, tc := range tests {
		if got := normalizeIP(tc.input); got != tc.want {
			t.Errorf("normalizeIP(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
