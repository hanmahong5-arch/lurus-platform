// Package retry provides configurable retry with exponential backoff,
// jitter, and conditional retry support.
//
// Borrowed from OpenClaw's retryAsync pattern:
// - Dual mode: simple (N attempts) and configurable (full options)
// - Exponential backoff with jitter to prevent thundering herd
// - Conditional retry via ShouldRetry predicate
// - Retry-After header support for upstream rate limiting
package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Config holds retry parameters.
type Config struct {
	// MaxAttempts is the total number of attempts (including the first).
	MaxAttempts int
	// InitialDelay is the base delay before the first retry.
	InitialDelay time.Duration
	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration
	// Jitter adds randomness to prevent thundering herd (0.0 = none, 1.0 = full).
	Jitter float64
}

// DefaultConfig returns production-safe retry defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 300 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Jitter:       0.2,
	}
}

// Options provides fine-grained control over retry behavior.
type Options struct {
	Config Config
	// Label is included in retry logs for debugging.
	Label string
	// ShouldRetry decides whether to retry based on the error and attempt number.
	// Return false to stop retrying immediately (e.g. for 4xx errors).
	// If nil, all errors are retried.
	ShouldRetry func(err error, attempt int) bool
	// OnRetry is called before each retry sleep (for logging/metrics).
	OnRetry func(attempt, maxAttempts int, delay time.Duration, err error)
}

// Do executes fn with retries according to the provided options.
// Respects context cancellation between retries.
func Do(ctx context.Context, opts Options, fn func(ctx context.Context) error) error {
	cfg := opts.Config
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		// Last attempt — don't sleep.
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Check if we should retry this error.
		if opts.ShouldRetry != nil && !opts.ShouldRetry(lastErr, attempt) {
			break
		}

		// Calculate delay: min(initialDelay * 2^(attempt-1), maxDelay).
		delay := cfg.InitialDelay * time.Duration(math.Pow(2, float64(attempt-1)))
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}

		// Apply jitter.
		if cfg.Jitter > 0 {
			jitterRange := float64(delay) * cfg.Jitter
			delta := rand.Float64()*2*jitterRange - jitterRange // [-jitter, +jitter]
			delay = time.Duration(float64(delay) + delta)
			if delay < 0 {
				delay = 0
			}
		}

		if opts.OnRetry != nil {
			opts.OnRetry(attempt, cfg.MaxAttempts, delay, lastErr)
		}

		// Sleep with context cancellation support.
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("retry exhausted (%d attempts): %w", cfg.MaxAttempts, lastErr)
}

// Simple retries fn up to maxAttempts times with exponential backoff.
// For callers that don't need fine-grained control.
func Simple(ctx context.Context, maxAttempts int, fn func(ctx context.Context) error) error {
	return Do(ctx, Options{
		Config: Config{
			MaxAttempts:  maxAttempts,
			InitialDelay: 300 * time.Millisecond,
			MaxDelay:     5 * time.Second,
			Jitter:       0.2,
		},
	}, fn)
}
