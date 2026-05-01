package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// setupCredAgeRedis spins a per-test miniredis + go-redis client. The
// miniredis instance is auto-cleaned by t.Cleanup via miniredis.RunT.
func setupCredAgeRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

// scrapeMetricLine fetches /metrics once and returns the matching line
// for `metricName` containing `labelMatch`, or "" if absent. Used to
// verify sampleOnce actually wrote the gauge — calling Set with the
// wrong value or skipping the call entirely both surface here without
// reflective access into the Prometheus registry.
func scrapeMetricLine(t *testing.T, metricName, labelMatch string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler returned status=%d, want 200", rec.Code)
	}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, metricName) && strings.Contains(line, labelMatch) {
			return line
		}
	}
	return ""
}

// metricValue parses the trailing float value out of a Prometheus
// exposition line. Returns (value, true) on success.
func metricValue(line string) (float64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// TestCredentialAgeWorker_Disabled_NoSampling: when CRON_CRED_AGE_ENABLED
// is false, Run blocks on ctx and never touches Redis. We verify by
// counting commands miniredis observed — a buggy implementation that
// sampled anyway would issue at least one GET.
func TestCredentialAgeWorker_Disabled_NoSampling(t *testing.T) {
	mr, rdb := setupCredAgeRedis(t)
	w := NewCredentialAgeWorker(rdb, false)
	// Tight interval so a buggy "ignore disabled flag" implementation
	// would fire several samples before ctx cancel.
	w.interval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run returned err: %v", err)
	}
	if got := mr.CommandCount(); got > 0 {
		t.Errorf("disabled worker issued %d redis commands, want 0", got)
	}
}

// TestCredentialAgeWorker_Bootstrap_NoTimestampDoesNotCrash: cold-boot
// case — Redis has no key yet. The worker must warn-and-continue
// rather than fabricate a fake "now" stamp (which would silently hide
// a forgotten rotation). Contract: gauge series for that name is NOT
// written when the timestamp is missing.
func TestCredentialAgeWorker_Bootstrap_NoTimestampDoesNotCrash(t *testing.T) {
	_, rdb := setupCredAgeRedis(t)
	w := NewCredentialAgeWorker(rdb, true)
	w.credentials = []TrackedCredential{{
		Name:      "test_bootstrap",
		SoftLimit: DefaultCredentialSoftLimit,
		HardLimit: DefaultCredentialHardLimit,
	}}

	// Should not panic.
	w.sampleOnce(context.Background())

	if line := scrapeMetricLine(t, "lurus_platform_credential_age_days", `name="test_bootstrap"`); line != "" {
		t.Errorf("bootstrap case unexpectedly wrote gauge: %s", line)
	}
}

// TestCredentialAgeWorker_SoftLimit_EmitsGauge stamps a credential as
// 100 days old and asserts the gauge reports the age. Crossing the
// soft limit is the visible "warn" path; the alerting rule does the
// notification, so the test only needs to confirm the gauge >=99.
func TestCredentialAgeWorker_SoftLimit_EmitsGauge(t *testing.T) {
	_, rdb := setupCredAgeRedis(t)
	w := NewCredentialAgeWorker(rdb, true)
	w.credentials = []TrackedCredential{{
		Name:      "test_soft",
		SoftLimit: DefaultCredentialSoftLimit,
		HardLimit: DefaultCredentialHardLimit,
	}}

	rotated := time.Now().Add(-100 * 24 * time.Hour).UTC()
	if err := rdb.Set(context.Background(), credentialRotatedKeyPrefix+"test_soft", rotated.Format(time.RFC3339Nano), 0).Err(); err != nil {
		t.Fatalf("seed Redis: %v", err)
	}
	w.sampleOnce(context.Background())

	line := scrapeMetricLine(t, "lurus_platform_credential_age_days", `name="test_soft"`)
	if line == "" {
		t.Fatal("expected gauge line for test_soft, got none")
	}
	v, ok := metricValue(line)
	if !ok {
		t.Fatalf("could not parse value from line: %s", line)
	}
	if v < 99 {
		t.Errorf("expected age >=99 days, got %.2f (line: %s)", v, line)
	}
}

// TestCredentialAgeWorker_HardLimit_EmitsGauge mirrors the soft-limit
// test but at 200 days, exercising the path that escalates to ERROR.
func TestCredentialAgeWorker_HardLimit_EmitsGauge(t *testing.T) {
	_, rdb := setupCredAgeRedis(t)
	w := NewCredentialAgeWorker(rdb, true)
	w.credentials = []TrackedCredential{{
		Name:      "test_hard",
		SoftLimit: DefaultCredentialSoftLimit,
		HardLimit: DefaultCredentialHardLimit,
	}}

	rotated := time.Now().Add(-200 * 24 * time.Hour).UTC()
	if err := rdb.Set(context.Background(), credentialRotatedKeyPrefix+"test_hard", rotated.Format(time.RFC3339Nano), 0).Err(); err != nil {
		t.Fatalf("seed Redis: %v", err)
	}
	w.sampleOnce(context.Background())

	line := scrapeMetricLine(t, "lurus_platform_credential_age_days", `name="test_hard"`)
	if line == "" {
		t.Fatal("expected gauge line for test_hard, got none")
	}
	v, ok := metricValue(line)
	if !ok {
		t.Fatalf("could not parse value from line: %s", line)
	}
	if v < 199 {
		t.Errorf("expected age >=199 days, got %.2f (line: %s)", v, line)
	}
}

// TestCredentialAgeWorker_MarkRotated_IsReadable: round-trip — stamp
// now, then sample, then expect age <1 day. This is the happy-path
// operator flow after a manual rotation.
func TestCredentialAgeWorker_MarkRotated_IsReadable(t *testing.T) {
	_, rdb := setupCredAgeRedis(t)
	w := NewCredentialAgeWorker(rdb, true)
	w.credentials = []TrackedCredential{{
		Name:      "test_mark",
		SoftLimit: DefaultCredentialSoftLimit,
		HardLimit: DefaultCredentialHardLimit,
	}}

	if err := w.MarkRotated(context.Background(), "test_mark"); err != nil {
		t.Fatalf("MarkRotated: %v", err)
	}
	w.sampleOnce(context.Background())

	line := scrapeMetricLine(t, "lurus_platform_credential_age_days", `name="test_mark"`)
	if line == "" {
		t.Fatal("expected gauge line for test_mark after MarkRotated, got none")
	}
	v, ok := metricValue(line)
	if !ok {
		t.Fatalf("could not parse value from line: %s", line)
	}
	if v >= 1 {
		t.Errorf("expected age <1 day right after MarkRotated, got %.4f (line: %s)", v, line)
	}
}

// TestCredentialAgeWorker_MarkRotated_RejectsEmptyName ensures the
// guard fires — protecting the "cred:rotated:" prefix against drift
// into a useless single-key namespace via accidental "" name.
func TestCredentialAgeWorker_MarkRotated_RejectsEmptyName(t *testing.T) {
	_, rdb := setupCredAgeRedis(t)
	w := NewCredentialAgeWorker(rdb, true)
	if err := w.MarkRotated(context.Background(), "   "); err == nil {
		t.Fatal("expected error for whitespace-only name, got nil")
	}
}
