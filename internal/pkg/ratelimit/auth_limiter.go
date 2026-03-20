// auth_limiter.go provides in-memory auth-failure rate limiting with per-scope
// tracking, lockout after N failures, and automatic cleanup.
//
// Borrowed from OpenClaw's auth-rate-limit.ts pattern:
// - Multiple scopes (password, API key, device token) track independently
// - Loopback addresses are exempt (local dev convenience)
// - Successful auth resets the counter (prevents lockout after recovery)
// - Periodic pruning prevents memory leaks from transient IPs
package ratelimit

import (
	"net"
	"strings"
	"sync"
	"time"
)

// AuthLimiterConfig configures the auth failure rate limiter.
type AuthLimiterConfig struct {
	// MaxAttempts is the number of failures before lockout.
	MaxAttempts int
	// WindowDuration is the sliding window for counting failures.
	WindowDuration time.Duration
	// LockoutDuration is how long an IP is locked out after exceeding MaxAttempts.
	LockoutDuration time.Duration
	// PruneInterval is how often expired entries are cleaned up.
	PruneInterval time.Duration
}

// DefaultAuthLimiterConfig returns production-safe defaults.
func DefaultAuthLimiterConfig() AuthLimiterConfig {
	return AuthLimiterConfig{
		MaxAttempts:     10,
		WindowDuration:  5 * time.Minute,
		LockoutDuration: 15 * time.Minute,
		PruneInterval:   2 * time.Minute,
	}
}

// AuthScope identifies the authentication method being rate-limited.
type AuthScope string

const (
	AuthScopePassword    AuthScope = "password"
	AuthScopeAPIKey      AuthScope = "api_key"
	AuthScopeOAuth       AuthScope = "oauth"
	AuthScopeSessionToken AuthScope = "session_token"
)

// AuthCheckResult holds the result of a rate limit check.
type AuthCheckResult struct {
	Allowed      bool
	Remaining    int
	RetryAfterMs int64
}

type authEntry struct {
	attempts    []int64    // Unix millisecond timestamps of failures
	lockedUntil *time.Time // nil = not locked
}

// AuthLimiter tracks per-IP, per-scope authentication failure rates.
type AuthLimiter struct {
	mu      sync.Mutex
	entries map[string]*authEntry
	cfg     AuthLimiterConfig
	stopCh  chan struct{}
}

// NewAuthLimiter creates an auth rate limiter with background pruning.
// Call Stop() to release resources.
func NewAuthLimiter(cfg AuthLimiterConfig) *AuthLimiter {
	l := &AuthLimiter{
		entries: make(map[string]*authEntry),
		cfg:     cfg,
		stopCh:  make(chan struct{}),
	}
	go l.pruneLoop()
	return l
}

// Check reports whether the IP+scope combination is allowed to attempt auth.
func (l *AuthLimiter) Check(ip string, scope AuthScope) AuthCheckResult {
	if isLoopback(ip) {
		return AuthCheckResult{Allowed: true, Remaining: l.cfg.MaxAttempts}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	key := makeKey(ip, scope)
	entry := l.entries[key]
	if entry == nil {
		return AuthCheckResult{Allowed: true, Remaining: l.cfg.MaxAttempts}
	}

	now := time.Now()

	// Check lockout.
	if entry.lockedUntil != nil && now.Before(*entry.lockedUntil) {
		return AuthCheckResult{
			Allowed:      false,
			Remaining:    0,
			RetryAfterMs: entry.lockedUntil.Sub(now).Milliseconds(),
		}
	}

	// Lockout expired — clear it.
	if entry.lockedUntil != nil {
		entry.lockedUntil = nil
		entry.attempts = nil
	}

	l.slideWindow(entry, now)
	remaining := l.cfg.MaxAttempts - len(entry.attempts)
	if remaining < 0 {
		remaining = 0
	}
	return AuthCheckResult{Allowed: true, Remaining: remaining}
}

// RecordFailure records a failed authentication attempt.
func (l *AuthLimiter) RecordFailure(ip string, scope AuthScope) {
	if isLoopback(ip) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	key := makeKey(ip, scope)
	entry := l.entries[key]
	if entry == nil {
		entry = &authEntry{}
		l.entries[key] = entry
	}

	now := time.Now()
	l.slideWindow(entry, now)
	entry.attempts = append(entry.attempts, now.UnixMilli())

	if len(entry.attempts) >= l.cfg.MaxAttempts {
		lockUntil := now.Add(l.cfg.LockoutDuration)
		entry.lockedUntil = &lockUntil
	}
}

// RecordSuccess resets the failure counter for the IP+scope.
// Call after successful authentication to prevent residual lockout.
func (l *AuthLimiter) RecordSuccess(ip string, scope AuthScope) {
	if isLoopback(ip) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.entries, makeKey(ip, scope))
}

// Stop halts the background pruning goroutine.
func (l *AuthLimiter) Stop() {
	close(l.stopCh)
}

// slideWindow removes attempts outside the current window (must hold lock).
func (l *AuthLimiter) slideWindow(entry *authEntry, now time.Time) {
	cutoff := now.Add(-l.cfg.WindowDuration).UnixMilli()
	i := 0
	for i < len(entry.attempts) && entry.attempts[i] < cutoff {
		i++
	}
	if i > 0 {
		entry.attempts = entry.attempts[i:]
	}
}

func (l *AuthLimiter) pruneLoop() {
	ticker := time.NewTicker(l.cfg.PruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.prune()
		}
	}
}

func (l *AuthLimiter) prune() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for key, entry := range l.entries {
		// Remove entries with expired lockouts and no recent attempts.
		if entry.lockedUntil != nil && now.After(*entry.lockedUntil) {
			delete(l.entries, key)
			continue
		}
		// Remove entries with no attempts in the window.
		l.slideWindow(entry, now)
		if len(entry.attempts) == 0 && entry.lockedUntil == nil {
			delete(l.entries, key)
		}
	}
}

func makeKey(ip string, scope AuthScope) string {
	return string(scope) + ":" + normalizeIP(ip)
}

func normalizeIP(ip string) string {
	// Handle IPv4-mapped IPv6: ::ffff:1.2.3.4 → 1.2.3.4
	if strings.HasPrefix(ip, "::ffff:") {
		return ip[7:]
	}
	return ip
}

func isLoopback(ip string) bool {
	normalized := normalizeIP(ip)
	parsed := net.ParseIP(normalized)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback()
}
