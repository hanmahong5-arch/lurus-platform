package app_registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

// Reconciler drives the apps.yaml → Zitadel + K8s convergence loop.
//
// Lifecycle:
//  1. NewReconciler: validates deps, does not reach out.
//  2. Run: blocks until ctx cancel. On entry runs ReconcileOnce so any
//     drift that appeared while the pod was down is fixed before the
//     periodic tick kicks in.
//
// Safe to run in a goroutine from main; every error path logs + metric-
// records and never panics the process.
type Reconciler struct {
	spec     *Spec
	zitadel  *zitadel.Client
	k8s      *K8sClient
	interval time.Duration
}

// Options configures a Reconciler; zero values pick sensible defaults.
type Options struct {
	// Interval between full reconcile passes. Default 5 min — app
	// registration drifts slowly, so chasing every second wastes API
	// quota. Manual kubectl-delete-secret incidents are caught on the
	// next tick (≤5 min) which is well within typical ops response.
	Interval time.Duration
}

// NewReconciler constructs a Reconciler. The Zitadel client may be nil
// (e.g. when ZITADEL_SERVICE_ACCOUNT_PAT is unset) — Run will log-and-
// exit cleanly in that case instead of panicking. The K8s client must
// be non-nil; the caller checks ErrNotInCluster before invoking us.
func NewReconciler(spec *Spec, zit *zitadel.Client, k8s *K8sClient, opts Options) (*Reconciler, error) {
	if spec == nil {
		return nil, errors.New("app_registry: spec is required")
	}
	if k8s == nil {
		return nil, errors.New("app_registry: k8s client is required (use ErrNotInCluster to skip)")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Reconciler{
		spec:     spec,
		zitadel:  zit,
		k8s:      k8s,
		interval: interval,
	}, nil
}

// Run blocks until ctx is cancelled, running ReconcileOnce on entry and
// then on every tick of the configured interval. Errors from individual
// apps do not stop the loop; they are logged and the next tick retries.
func (r *Reconciler) Run(ctx context.Context) {
	if r.zitadel == nil {
		slog.Info("app_registry: zitadel client not configured (ZITADEL_SERVICE_ACCOUNT_PAT empty) — reconciler disabled")
		return
	}
	slog.Info("app_registry: starting", "interval", r.interval, "apps", len(r.spec.Apps))

	// Initial sync on boot.
	r.ReconcileOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("app_registry: stopped")
			return
		case <-ticker.C:
			r.ReconcileOnce(ctx)
		}
	}
}

// ReconcileOnce walks every enabled app × environment pair and ensures
// Zitadel + K8s match the declared state. Exposed for tests and for a
// possible `/admin/reconcile-now` endpoint later.
func (r *Reconciler) ReconcileOnce(ctx context.Context) {
	projectID, err := r.zitadel.EnsureProject(ctx, r.spec.Project)
	if err != nil {
		slog.Error("app_registry: ensure project failed", "project", r.spec.Project, "err", err)
		metrics.RecordAppRegistryReconcile("project_ensure_failed")
		return
	}

	for _, app := range r.spec.Apps {
		if !app.IsEnabled() {
			metrics.RecordAppRegistryReconcile("skipped_disabled")
			continue
		}
		for _, env := range app.Environments {
			r.reconcileAppEnv(ctx, projectID, app, env)
		}
	}
}

// reconcileAppEnv is the per-environment unit of work. Kept small so
// each phase's error surface is clear in logs.
func (r *Reconciler) reconcileAppEnv(ctx context.Context, projectID string, app App, env Environment) {
	// Zitadel app name combines slug + env so (tally, stage) and
	// (tally, prod) live side-by-side inside one project.
	zitAppName := app.Name + "-" + env.Env

	spec := zitadel.OIDCAppSpec{
		Name:                   zitAppName,
		AppType:                app.OIDC.AppType,
		AuthMethod:             app.OIDC.AuthMethod,
		GrantTypes:             app.OIDC.GrantTypes,
		ResponseTypes:          app.OIDC.ResponseTypes,
		RedirectURIs:           []string{env.RedirectURI(app.OIDC)},
		PostLogoutRedirectURIs: []string{env.PostLogoutURI(app.OIDC)},
	}

	creds, err := r.zitadel.EnsureOIDCApp(ctx, projectID, spec)
	if err != nil {
		slog.Error("app_registry: ensure oidc app failed",
			"app", app.Name, "env", env.Env, "err", err)
		metrics.RecordAppRegistryReconcile("oidc_ensure_failed")
		return
	}

	// Write the issued credentials into K8s Secret (merge — never
	// clobber unrelated keys owned by other controllers / operators).
	updates := map[string][]byte{
		env.Secret.ClientIDKey: []byte(creds.ClientID),
	}
	if app.OIDC.AuthMethod == "basic" && creds.ClientSecret != "" && env.Secret.ClientSecretKey != "" {
		updates[env.Secret.ClientSecretKey] = []byte(creds.ClientSecret)
	}
	changed, err := r.k8s.MergeSecretData(ctx, env.Secret.Namespace, env.Secret.Name, updates)
	if err != nil {
		slog.Error("app_registry: secret write failed",
			"app", app.Name, "env", env.Env,
			"namespace", env.Secret.Namespace, "secret", env.Secret.Name, "err", err)
		metrics.RecordAppRegistryReconcile("secret_write_failed")
		return
	}
	if !changed {
		metrics.RecordAppRegistryReconcile("noop")
		return
	}

	slog.Info("app_registry: secret updated",
		"app", app.Name, "env", env.Env,
		"namespace", env.Secret.Namespace, "secret", env.Secret.Name,
		"zitadel_app", zitAppName, "client_id_preview", previewClientID(creds.ClientID))
	metrics.RecordAppRegistryReconcile("secret_updated")

	// Trigger a rolling restart so the target deployment picks up the
	// new credential. Failure here is not fatal — the Secret is already
	// in the desired state; the next normal restart will load it.
	if env.RestartDeployment == "" {
		return
	}
	if err := r.k8s.TriggerRolloutRestart(ctx, env.Secret.Namespace, env.RestartDeployment); err != nil {
		slog.Warn("app_registry: rollout restart failed",
			"app", app.Name, "env", env.Env,
			"deployment", env.RestartDeployment, "err", err)
		metrics.RecordAppRegistryReconcile("rollout_failed")
		return
	}
	slog.Info("app_registry: rollout triggered",
		"app", app.Name, "env", env.Env, "deployment", env.RestartDeployment)
	metrics.RecordAppRegistryReconcile("rollout_triggered")
}

// previewClientID returns the first 12 chars of the client_id for log
// context. We never log the full id to reduce risk of accidental leaks
// into Loki-indexed access logs (client_ids are not secret per se but
// they widen the attack surface when exposed with user emails).
func previewClientID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

// ConfigPath is the conventional location of the app registry YAML
// inside the platform-core container. Exported so main.go uses the
// same constant.
const ConfigPath = "/etc/lurus-platform/apps.yaml"

// ErrConfigMissing is returned by LoadSpec via a boot helper when the
// file doesn't exist. Treated as "app_registry feature not configured"
// rather than a hard failure — legacy single-app deployments keep
// working unchanged.
var ErrConfigMissing = fmt.Errorf("app_registry: %s not found", ConfigPath)
