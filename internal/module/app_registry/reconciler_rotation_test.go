package app_registry

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

// fakeZitadelHandler returns an httptest.HandlerFunc that satisfies the
// minimal subset of Zitadel Management API the reconciler reaches for
// during rotation:
//
//   - POST /management/v1/projects/_search           → returns the project id
//   - POST /management/v1/projects/{id}/apps/_search → returns the app id
//   - GET  /management/v1/projects/{id}/apps/{id}    → returns the client_id
//   - POST /management/v1/projects/{id}/apps/{id}/oidc_config/_generate_client_secret → mints a new secret
//
// rotateCount is incremented every time the rotation endpoint is hit so
// tests can assert "rotated exactly once" / "didn't rotate".
type fakeZitadel struct {
	rotateCount   int32
	wantProjectID string
	wantAppID     string
}

func (f *fakeZitadel) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/management/v1/projects/_search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{{"id": f.wantProjectID, "name": "lurus-platform"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/apps/_search"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{{"id": f.wantAppID, "name": "admin-prod"}},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/apps/"+f.wantAppID):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"app": map[string]any{"oidcConfig": map[string]any{"clientId": "client-abc"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/_generate_client_secret"):
			atomic.AddInt32(&f.rotateCount, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"clientSecret": "rotated-secret"})
		default:
			t.Logf("unexpected zitadel call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// fakeK8s mocks the kube-apiserver endpoints the reconciler uses during
// rotation: GET / PATCH the Secret and PATCH the Deployment for rollout.
// Only the paths exercised in the rotation tests are implemented; any
// other path returns 200 with an empty object so secondary calls don't
// fail the test for the wrong reason.
type fakeK8s struct {
	patchedSecret int32
	rolloutHits   int32
}

func (f *fakeK8s) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{"OLD_KEY":"b2xkLXZhbA=="}}`))
		case http.MethodPatch:
			if strings.Contains(r.URL.Path, "/deployments/") {
				atomic.AddInt32(&f.rolloutHits, 1)
			} else {
				atomic.AddInt32(&f.patchedSecret, 1)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}
}

// newRotationFixture builds a Reconciler wired to fake Zitadel + fake
// kube-apiserver + miniredis, plus a one-app spec with rotation
// enabled. The returned closures let individual tests poke the inputs
// (e.g. backdate the last-rotated timestamp) and assert outputs.
func newRotationFixture(t *testing.T, intervalDays int) (
	*Reconciler,
	*fakeZitadel,
	*fakeK8s,
	*RotationState,
) {
	t.Helper()
	fz := &fakeZitadel{wantProjectID: "proj-x", wantAppID: "app-x"}
	zsrv := httptest.NewServer(fz.handler(t))
	t.Cleanup(zsrv.Close)
	// NewClient pins issuer + pat onto a Client with its own
	// http.Client; the issuer value is the test server's URL so all
	// management API calls are routed there.
	zc := zitadel.NewClient(zsrv.URL, "test-pat")
	if zc == nil {
		t.Fatal("zitadel.NewClient returned nil — pat must be non-empty")
	}

	fk := &fakeK8s{}
	ksrv := httptest.NewTLSServer(fk.handler())
	t.Cleanup(ksrv.Close)
	k := &K8sClient{
		apiBase: ksrv.URL,
		token:   "test-token",
		http:    &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}},
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	rs := NewRotationState(rdb)

	enabled := true
	spec := &Spec{
		Org:     "lurus",
		Project: "lurus-platform",
		Apps: []App{{
			Name:    "admin",
			Enabled: &enabled,
			OIDC: OIDCSettings{
				AppType:        "web",
				AuthMethod:     "basic",
				GrantTypes:     []string{"authorization_code"},
				ResponseTypes:  []string{"code"},
				RedirectPath:   "/cb",
				PostLogoutPath: "/",
			},
			Environments: []Environment{{
				Env:    "prod",
				Domain: "admin.lurus.cn",
				Secret: SecretTarget{
					Namespace:       "lurus-admin",
					Name:            "admin-secrets",
					ClientIDKey:     "ZITADEL_CLIENT_ID",
					ClientSecretKey: "ZITADEL_CLIENT_SECRET",
				},
				RestartDeployment: "admin-web",
			}},
			SecretRotation: SecretRotation{Enabled: true, IntervalDays: intervalDays},
		}},
	}
	r, err := NewReconciler(spec, zc, k, rs, Options{Interval: time.Hour})
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	return r, fz, fk, rs
}

// TestReconciler_Rotation_BootstrapsClock verifies that on first sight
// of an enabled-rotation app, the reconciler does NOT mint a new secret
// — instead it just plants a "last rotated = now" marker, because the
// initial secret was minted at provisioning time.
func TestReconciler_Rotation_BootstrapsClock(t *testing.T) {
	r, fz, _, rs := newRotationFixture(t, 90)

	r.maybeRotateSecret(context.Background(), "proj-x", r.spec.Apps[0], r.spec.Apps[0].Environments[0])

	if got := atomic.LoadInt32(&fz.rotateCount); got != 0 {
		t.Errorf("rotateCount = %d, want 0 (should bootstrap clock, not rotate)", got)
	}
	last, err := rs.GetLastRotated(context.Background(), "admin", "prod")
	if err != nil {
		t.Fatalf("GetLastRotated: %v", err)
	}
	if last.IsZero() {
		t.Error("expected bootstrap to set a non-zero timestamp")
	}
}

// TestReconciler_Rotation_BeforeInterval verifies that a recent rotation
// stamp short-circuits the rotation path — no Zitadel call, no K8s
// patch, regardless of how often we tick.
func TestReconciler_Rotation_BeforeInterval(t *testing.T) {
	r, fz, fk, rs := newRotationFixture(t, 90)
	ctx := context.Background()
	if err := rs.MarkRotated(ctx, "admin", "prod"); err != nil {
		t.Fatalf("seed timestamp: %v", err)
	}

	r.maybeRotateSecret(ctx, "proj-x", r.spec.Apps[0], r.spec.Apps[0].Environments[0])

	if got := atomic.LoadInt32(&fz.rotateCount); got != 0 {
		t.Errorf("rotateCount = %d, want 0 (still inside interval window)", got)
	}
	if got := atomic.LoadInt32(&fk.patchedSecret); got != 0 {
		t.Errorf("patchedSecret = %d, want 0 (rotation should not run)", got)
	}
}

// TestReconciler_Rotation_AfterInterval simulates an aged secret by
// writing a backdated timestamp to Redis, then asserts that the
// reconciler rotates exactly once and refreshes the timestamp.
func TestReconciler_Rotation_AfterInterval(t *testing.T) {
	r, fz, fk, rs := newRotationFixture(t, 90)
	ctx := context.Background()

	// Backdate by 100 days — well past the 90-day threshold.
	pastKey := rotationKey("admin", "prod")
	pastTs := time.Now().Add(-100 * 24 * time.Hour).Unix()
	if err := rs.rdb.Set(ctx, pastKey, formatInt(pastTs), 0).Err(); err != nil {
		t.Fatalf("seed past timestamp: %v", err)
	}

	r.maybeRotateSecret(ctx, "proj-x", r.spec.Apps[0], r.spec.Apps[0].Environments[0])

	if got := atomic.LoadInt32(&fz.rotateCount); got != 1 {
		t.Errorf("rotateCount = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&fk.patchedSecret); got != 1 {
		t.Errorf("patchedSecret = %d, want 1", got)
	}
	// Rollout should also have been triggered.
	if got := atomic.LoadInt32(&fk.rolloutHits); got != 1 {
		t.Errorf("rolloutHits = %d, want 1", got)
	}
	// Timestamp must be refreshed to ~now.
	last, err := rs.GetLastRotated(ctx, "admin", "prod")
	if err != nil {
		t.Fatalf("GetLastRotated: %v", err)
	}
	if delta := time.Since(last); delta > 5*time.Second {
		t.Errorf("timestamp not refreshed: %v ago", delta)
	}
}

// TestReconciler_RotateOnce_RejectsPKCE makes sure the manual path can't
// be tricked into hitting Zitadel for a PKCE app.
func TestReconciler_RotateOnce_RejectsPKCE(t *testing.T) {
	r, fz, _, _ := newRotationFixture(t, 90)
	r.spec.Apps[0].OIDC.AuthMethod = "none"

	_, err := r.RotateOnce(context.Background(), "admin", "prod")
	if err == nil {
		t.Fatal("expected error for PKCE rotation, got nil")
	}
	if !strings.Contains(err.Error(), "only 'basic' has a client_secret") {
		t.Errorf("error should mention auth_method requirement, got: %v", err)
	}
	if got := atomic.LoadInt32(&fz.rotateCount); got != 0 {
		t.Errorf("PKCE app should not call Zitadel rotate; got %d hits", got)
	}
}

// TestReconciler_RotateOnce_UnknownTarget covers the 404 path used by
// the admin handler — apps.yaml is the source of truth, so an arbitrary
// path param must not cause Zitadel calls.
func TestReconciler_RotateOnce_UnknownTarget(t *testing.T) {
	r, fz, _, _ := newRotationFixture(t, 90)
	_, err := r.RotateOnce(context.Background(), "ghost", "prod")
	if err == nil {
		t.Fatal("expected error for unknown app, got nil")
	}
	if !strings.Contains(err.Error(), "not declared in apps.yaml") {
		t.Errorf("error should mention apps.yaml, got: %v", err)
	}
	if got := atomic.LoadInt32(&fz.rotateCount); got != 0 {
		t.Errorf("unknown app should not call Zitadel; got %d hits", got)
	}
}

// formatInt avoids dragging strconv into the test for one call site.
func formatInt(v int64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
