package readiness_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/readiness"
)

// fakeChecker is a minimal Checker impl driven entirely by its fields,
// so each test can declare the name/error inline without touching the
// production prebuilt checkers (which require real redis/sql/nats conns).
type fakeChecker struct {
	name string
	err  error
}

func (f *fakeChecker) Name() string                  { return f.name }
func (f *fakeChecker) Check(_ context.Context) error { return f.err }

// serve executes the handler against a fresh Gin test context and returns
// the response recorder for assertions.
func serve(t *testing.T, set *readiness.Set) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	set.HTTPHandler()(c)
	return w
}

// decode parses the JSON body into a shape-agnostic map so tests can
// assert on both the "ready" boolean and the "failures" list.
func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode readiness body: %v — body=%s", err, w.Body.String())
	}
	return out
}

// TestSet_AllHealthy_Returns200 covers the happy path: every checker
// succeeds, response is {"ready": true}, no failures key.
func TestSet_AllHealthy_Returns200(t *testing.T) {
	set := readiness.NewSet(
		&fakeChecker{name: "redis"},
		&fakeChecker{name: "postgres"},
	)

	w := serve(t, set)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body=%s)", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["ready"] != true {
		t.Errorf("ready = %v; want true", body["ready"])
	}
	if _, hasFailures := body["failures"]; hasFailures {
		t.Errorf("failures key should be omitted when all healthy; body=%s", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "" {
		t.Errorf("Content-Type header is empty; want application/json variant")
	}
}

// TestSet_OneFailing_Returns503 covers the failure path: at least one
// checker returns an error, so the response must be 503 with the
// failing checker's name+err surfaced verbatim for the alerting rule.
func TestSet_OneFailing_Returns503(t *testing.T) {
	set := readiness.NewSet(
		&fakeChecker{name: "redis"},
		&fakeChecker{name: "postgres", err: errors.New("connection refused")},
	)

	w := serve(t, set)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 (body=%s)", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["ready"] != false {
		t.Errorf("ready = %v; want false", body["ready"])
	}

	raw, ok := body["failures"].([]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("failures = %v; want single entry (body=%s)", body["failures"], w.Body.String())
	}
	entry := raw[0].(map[string]any)
	if entry["name"] != "postgres" {
		t.Errorf("failure name = %v; want postgres", entry["name"])
	}
	if entry["err"] != "connection refused" {
		t.Errorf("failure err = %v; want connection refused", entry["err"])
	}
}

// TestSet_Empty_ReturnsReady asserts that an empty Set (no checkers
// wired yet) is considered ready. This mirrors the behaviour of a
// freshly-booted process where dependencies haven't been attached to
// the router and we don't want to accidentally 503 every pod at boot.
func TestSet_Empty_ReturnsReady(t *testing.T) {
	set := readiness.NewSet()

	w := serve(t, set)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body=%s)", w.Code, w.Body.String())
	}
	if decode(t, w)["ready"] != true {
		t.Errorf("empty set should be ready")
	}
}

// TestSet_NilChecker_Dropped verifies that a nil entry (what
// RedisChecker/PostgresChecker/NATSChecker return when their client is
// nil) is silently skipped rather than causing a nil-pointer panic.
func TestSet_NilChecker_Dropped(t *testing.T) {
	// The prebuilt *Checker returns a nil interface when given a nil
	// client; Set must tolerate that so optional deployments don't
	// crash the probe handler.
	set := readiness.NewSet(
		readiness.RedisChecker(nil),
		readiness.PostgresChecker(nil),
		readiness.NATSChecker(nil),
		&fakeChecker{name: "always-healthy"},
	)

	w := serve(t, set)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (nil checkers should be dropped)", w.Code)
	}
}
