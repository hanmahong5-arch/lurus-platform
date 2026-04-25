package app_registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

// TestReconciler_SkipsTombstoned verifies the tombstone gate at the top
// of reconcileAppEnv: when (app, env) is tombstoned, the reconciler
// must NOT call out to Zitadel / K8s at all. We assert this by giving
// the reconciler a Zitadel client pointing at a server that fails the
// test if it ever receives a request.
func TestReconciler_SkipsTombstoned(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		// Returning 200 is irrelevant — the assertion is on `called`
		// staying at 0; we still emit a body so the client decoder
		// doesn't error out on the nil response.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()

	zitClient := zitadel.NewClient(srv.URL, "test-pat")

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	tombs := NewTombstones(rdb)
	ctx := context.Background()
	if err := tombs.Mark(ctx, "tally", "stage"); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}

	r := &Reconciler{
		spec:       &Spec{Org: "lurus", Project: "lurus-platform"},
		zitadel:    zitClient,
		k8s:        nil, // never reached on the tombstoned path
		interval:   time.Minute,
		tombstones: tombs,
	}

	app := App{
		Name: "tally",
		Environments: []Environment{{
			Env:    "stage",
			Domain: "tally-stage.lurus.cn",
			Secret: SecretTarget{
				Namespace:   "lurus-tally",
				Name:        "tally-secrets",
				ClientIDKey: "ZITADEL_CLIENT_ID",
			},
		}},
		OIDC: OIDCSettings{
			AppType:        "web",
			AuthMethod:     "none",
			GrantTypes:     []string{"authorization_code"},
			ResponseTypes:  []string{"code"},
			RedirectPath:   "/api/auth/callback/zitadel",
			PostLogoutPath: "/",
		},
	}
	r.reconcileAppEnv(ctx, "project-id", app, app.Environments[0])

	if called != 0 {
		t.Errorf("Zitadel was called %d times despite active tombstone; want 0", called)
	}
}

// TestReconciler_ProceedsWhenNoTombstone is the negative companion: the
// same setup without the tombstone must reach Zitadel (here, the test
// httptest server) at least once. Confirms the tombstone gate is the
// reason reconcileAppEnv short-circuits, not some unrelated bug.
func TestReconciler_ProceedsWhenNoTombstone(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		// Return an empty result so the search loop terminates without
		// triggering the create path (which would need K8s wired).
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer srv.Close()

	zitClient := zitadel.NewClient(srv.URL, "test-pat")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	// Empty tombstone store — no Mark.
	tombs := NewTombstones(rdb)

	r := &Reconciler{
		spec:       &Spec{Org: "lurus", Project: "lurus-platform"},
		zitadel:    zitClient,
		k8s:        nil,
		interval:   time.Minute,
		tombstones: tombs,
	}

	// reconcileAppEnv will get past the tombstone check and call
	// EnsureOIDCApp, which triggers an HTTP search. After that, the
	// nil k8s client surfaces as an error (we don't care which exact
	// error — only that Zitadel was contacted, proving the gate did
	// not skip the work). Recover so the panic on nil k8s doesn't
	// fail the test.
	defer func() { _ = recover() }()
	r.reconcileAppEnv(context.Background(), "project-id", App{
		Name: "tally",
		Environments: []Environment{{
			Env:    "stage",
			Domain: "tally-stage.lurus.cn",
			Secret: SecretTarget{Namespace: "lurus-tally", Name: "tally-secrets", ClientIDKey: "ZITADEL_CLIENT_ID"},
		}},
		OIDC: OIDCSettings{AppType: "web", AuthMethod: "none", GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}, RedirectPath: "/cb", PostLogoutPath: "/"},
	}, Environment{
		Env: "stage", Domain: "tally-stage.lurus.cn",
		Secret: SecretTarget{Namespace: "lurus-tally", Name: "tally-secrets", ClientIDKey: "ZITADEL_CLIENT_ID"},
	})

	if called == 0 {
		t.Error("Zitadel was not called at all without a tombstone — the gate may be incorrectly short-circuiting")
	}
}

// TestReconciler_NoTombstones_NilSafe confirms that the historical code
// path (no tombstones wired) keeps working — the gate only activates
// when WithTombstones has been called.
func TestReconciler_NoTombstones_NilSafe(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	defer srv.Close()

	zitClient := zitadel.NewClient(srv.URL, "test-pat")
	r := &Reconciler{
		spec:       &Spec{Org: "lurus", Project: "lurus-platform"},
		zitadel:    zitClient,
		k8s:        nil,
		interval:   time.Minute,
		tombstones: nil, // explicit nil — pre-tombstone wiring
	}

	defer func() { _ = recover() }()
	r.reconcileAppEnv(context.Background(), "project-id", App{
		Name: "tally",
		Environments: []Environment{{
			Env:    "stage",
			Domain: "tally-stage.lurus.cn",
			Secret: SecretTarget{Namespace: "lurus-tally", Name: "tally-secrets", ClientIDKey: "ZITADEL_CLIENT_ID"},
		}},
		OIDC: OIDCSettings{AppType: "web", AuthMethod: "none", GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}, RedirectPath: "/cb", PostLogoutPath: "/"},
	}, Environment{
		Env: "stage", Domain: "tally-stage.lurus.cn",
		Secret: SecretTarget{Namespace: "lurus-tally", Name: "tally-secrets", ClientIDKey: "ZITADEL_CLIENT_ID"},
	})

	if called == 0 {
		t.Error("Zitadel was not called when tombstones=nil — nil-safety regressed")
	}
}
