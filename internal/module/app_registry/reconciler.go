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
	rotation *RotationState
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
//
// The rotation argument is optional: when nil, the reconciler still runs
// project / app / secret reconciliation but skips the auto-rotation
// stage. Manual rotation invoked via the admin endpoint also remains
// available because that path constructs its own RotationState (or none).
func NewReconciler(spec *Spec, zit *zitadel.Client, k8s *K8sClient, rotation *RotationState, opts Options) (*Reconciler, error) {
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
		rotation: rotation,
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
	if env.RestartDeployment != "" {
		if err := r.k8s.TriggerRolloutRestart(ctx, env.Secret.Namespace, env.RestartDeployment); err != nil {
			slog.Warn("app_registry: rollout restart failed",
				"app", app.Name, "env", env.Env,
				"deployment", env.RestartDeployment, "err", err)
			metrics.RecordAppRegistryReconcile("rollout_failed")
		} else {
			slog.Info("app_registry: rollout triggered",
				"app", app.Name, "env", env.Env, "deployment", env.RestartDeployment)
			metrics.RecordAppRegistryReconcile("rollout_triggered")
		}
	}

	// Phase 3: confidential clients with rotation enabled also have
	// their last-rotated clock checked here. Done after the secret/rollout
	// path so a rotation-driven restart isn't doubled up with an unrelated
	// client_id update from EnsureOIDCApp.
	if app.OIDC.AuthMethod == "basic" && app.SecretRotation.Enabled {
		r.maybeRotateSecret(ctx, projectID, app, env)
	}
}

// maybeRotateSecret performs a single auto-rotation check for one
// (app, env) pair. It is a no-op when:
//   - rotation state isn't wired (nil RotationState — running without Redis);
//   - the last-rotated timestamp is younger than the configured interval;
//   - the Zitadel app cannot be looked up (lookup error or missing).
//
// On rotation it reuses the same K8s + Zitadel surfaces as the initial
// provisioning path: write the new secret into the K8s Secret, mark the
// rotation in Redis, then fire the deployment rollout.
func (r *Reconciler) maybeRotateSecret(ctx context.Context, projectID string, app App, env Environment) {
	if r.rotation == nil {
		return
	}
	last, err := r.rotation.GetLastRotated(ctx, app.Name, env.Env)
	if err != nil {
		slog.Warn("app_registry: rotation state read failed",
			"app", app.Name, "env", env.Env, "err", err)
		metrics.RecordAppRegistryReconcile("rotation_state_read_failed")
		return
	}
	interval := time.Duration(app.SecretRotation.IntervalDays) * 24 * time.Hour
	// Bootstrap case: no recorded rotation yet. We still need a clock so
	// future ticks can compute "due". Mark "now" without rotating; the
	// initial secret was minted by EnsureOIDCApp on first provisioning.
	if last.IsZero() {
		if err := r.rotation.MarkRotated(ctx, app.Name, env.Env); err != nil {
			slog.Warn("app_registry: rotation bootstrap mark failed",
				"app", app.Name, "env", env.Env, "err", err)
			metrics.RecordAppRegistryReconcile("rotation_state_write_failed")
		}
		return
	}
	if time.Since(last) < interval {
		// Still in the validity window — leave as-is.
		return
	}

	zitAppName := app.Name + "-" + env.Env
	creds, err := r.zitadel.LookupOIDCApp(ctx, projectID, zitAppName)
	if err != nil || creds == nil {
		slog.Warn("app_registry: rotation skipped — lookup failed",
			"app", app.Name, "env", env.Env, "err", err)
		metrics.RecordAppRegistryReconcile("rotation_lookup_failed")
		return
	}

	if err := r.performRotation(ctx, projectID, creds.AppID, app, env, "auto"); err != nil {
		// performRotation already logged + recorded the metric — caller
		// just bails out so the next tick retries.
		return
	}
}

// performRotation is the shared rotation primitive used by both the
// scheduled (auto) path and the admin-triggered (manual) path. It is
// trigger-aware because the metric and log fields are the only things
// that differ between the two sources.
func (r *Reconciler) performRotation(ctx context.Context, projectID, appID string, app App, env Environment, trigger string) error {
	newSecret, err := r.zitadel.RotateOIDCSecret(ctx, projectID, appID)
	if err != nil {
		slog.Error("app_registry: zitadel rotate failed",
			"app", app.Name, "env", env.Env, "trigger", trigger, "err", err)
		metrics.RecordAppRegistryReconcile("rotation_zitadel_failed")
		return err
	}
	updates := map[string][]byte{
		env.Secret.ClientSecretKey: []byte(newSecret),
	}
	if _, err := r.k8s.MergeSecretData(ctx, env.Secret.Namespace, env.Secret.Name, updates); err != nil {
		slog.Error("app_registry: rotation secret write failed",
			"app", app.Name, "env", env.Env, "trigger", trigger, "err", err)
		metrics.RecordAppRegistryReconcile("rotation_secret_write_failed")
		return err
	}
	if r.rotation != nil {
		if err := r.rotation.MarkRotated(ctx, app.Name, env.Env); err != nil {
			// Don't fail the rotation — the secret already lives in
			// Zitadel + K8s. Worst case: next tick rotates again, which
			// is annoying but not unsafe.
			slog.Warn("app_registry: rotation state write failed",
				"app", app.Name, "env", env.Env, "trigger", trigger, "err", err)
			metrics.RecordAppRegistryReconcile("rotation_state_write_failed")
		}
	}
	if env.RestartDeployment != "" {
		if err := r.k8s.TriggerRolloutRestart(ctx, env.Secret.Namespace, env.RestartDeployment); err != nil {
			slog.Warn("app_registry: rotation rollout failed",
				"app", app.Name, "env", env.Env, "trigger", trigger,
				"deployment", env.RestartDeployment, "err", err)
			metrics.RecordAppRegistryReconcile("rotation_rollout_failed")
			// Still considered a successful rotation: the secret is
			// rotated. Operators can manually restart the deployment.
		}
	}
	slog.Info("app_registry: client_secret rotated",
		"app", app.Name, "env", env.Env, "trigger", trigger,
		"namespace", env.Secret.Namespace, "secret", env.Secret.Name)
	metrics.RecordAppRegistryReconcile("rotation_succeeded")
	metrics.RecordOIDCSecretRotation(app.Name, env.Env, trigger)
	return nil
}

// RotateOnce performs one manual rotation for the given app + env and
// returns the moment the rotation completed. Exposed so the admin
// endpoint can drive an immediate rotation without waiting for the next
// reconcile tick. The caller is expected to enforce auth.
func (r *Reconciler) RotateOnce(ctx context.Context, appName, envName string) (time.Time, error) {
	if r.zitadel == nil {
		return time.Time{}, errors.New("app_registry: rotate: zitadel client not configured")
	}
	app, env, ok := r.findAppEnv(appName, envName)
	if !ok {
		return time.Time{}, fmt.Errorf("app_registry: rotate: %s/%s not declared in apps.yaml", appName, envName)
	}
	if app.OIDC.AuthMethod != "basic" {
		return time.Time{}, fmt.Errorf("app_registry: rotate: %s is auth_method=%s, only 'basic' has a client_secret", appName, app.OIDC.AuthMethod)
	}
	if env.Secret.ClientSecretKey == "" {
		return time.Time{}, fmt.Errorf("app_registry: rotate: %s/%s has no secret.client_secret_key configured", appName, envName)
	}
	projectID, err := r.zitadel.EnsureProject(ctx, r.spec.Project)
	if err != nil {
		return time.Time{}, fmt.Errorf("ensure project: %w", err)
	}
	zitAppName := app.Name + "-" + env.Env
	creds, err := r.zitadel.LookupOIDCApp(ctx, projectID, zitAppName)
	if err != nil {
		return time.Time{}, fmt.Errorf("lookup oidc app %s: %w", zitAppName, err)
	}
	if creds == nil {
		return time.Time{}, fmt.Errorf("oidc app %s not provisioned in zitadel — wait for reconciler", zitAppName)
	}
	if err := r.performRotation(ctx, projectID, creds.AppID, app, env, "manual"); err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC(), nil
}

// findAppEnv looks up the (app, env) tuple in the loaded spec. Returns
// (zero, zero, false) when no match — the caller maps that to a 404.
func (r *Reconciler) findAppEnv(appName, envName string) (App, Environment, bool) {
	for _, a := range r.spec.Apps {
		if a.Name != appName {
			continue
		}
		for _, e := range a.Environments {
			if e.Env == envName {
				return a, e, true
			}
		}
	}
	return App{}, Environment{}, false
}

// SecretRotationInterval returns the configured rotation interval for
// (app, env), or zero when rotation is not enabled. Used by the admin
// handler to compute next_due_at after a manual rotation.
func (r *Reconciler) SecretRotationInterval(appName, envName string) time.Duration {
	app, _, ok := r.findAppEnv(appName, envName)
	if !ok || !app.SecretRotation.Enabled {
		return 0
	}
	return time.Duration(app.SecretRotation.IntervalDays) * 24 * time.Hour
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
