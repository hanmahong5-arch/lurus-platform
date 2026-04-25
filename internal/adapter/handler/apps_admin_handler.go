package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/app_registry"
	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/zitadel"
)

// AppsAdminHandler exposes a read-only view of the declarative app
// registry (config/apps.yaml) joined with Zitadel's live OIDC state.
// Designed as the backend for the /apps admin UI — gives operators a
// visible answer to "which apps are registered + are they healthy?"
// without requiring kubectl or git.
//
// Mutations are intentionally narrow:
//   - List: pure read-only viewer.
//   - RotateSecret: shared primitive with the reconciler's auto-rotation
//     loop (Phase 3 / Track 2).
//   - DeleteRequest: starts a QR-delegate flow; the destructive Zitadel
//     call only happens once the boss scans + biometric-confirms on the
//     APP. apps.yaml remains the source of truth — operators still
//     follow up with a PR to drop the YAML entry inside the tombstone
//     window.
type AppsAdminHandler struct {
	configPath string
	zitadel    *zitadel.Client
	// recon is optional — when nil the rotate endpoint short-circuits
	// to 503. Read-only listing still works because List uses the
	// declarative loader directly.
	recon *app_registry.Reconciler
	// k8s + tombstones power the delegate (delete) execution path. Nil
	// values keep the read-only viewer working but cause DeleteRequest
	// to return 501 with a clear "delete flow not wired" message.
	k8s        *app_registry.K8sClient
	tombstones *app_registry.Tombstones
	// qr is the session minter for delegate flows. Nil = DeleteRequest
	// is gated, same as above.
	qr *QRHandler
}

// NewAppsAdminHandler wires the handler. When zitadel is nil (PAT not
// configured) the handler still serves the declarative view but omits
// the live-state columns. When recon is nil the rotate endpoint returns
// 503 — the dependency tree (Zitadel + K8s + Redis) wasn't fully wired,
// usually because the pod isn't in K8s or apps.yaml is missing.
func NewAppsAdminHandler(configPath string, zit *zitadel.Client, recon *app_registry.Reconciler) *AppsAdminHandler {
	return &AppsAdminHandler{
		configPath: configPath,
		zitadel:    zit,
		recon:      recon,
	}
}

// WithDeleteFlow wires the dependencies needed for the QR-delegate
// delete flow. All four are required for DeleteRequest to succeed; any
// missing one keeps the endpoint gated at 501 so partially-configured
// deployments fail loudly rather than silently mis-behave. Chainable.
func (h *AppsAdminHandler) WithDeleteFlow(qr *QRHandler, k8s *app_registry.K8sClient, t *app_registry.Tombstones) *AppsAdminHandler {
	h.qr = qr
	h.k8s = k8s
	h.tombstones = t
	if qr != nil {
		// Self-register as the delegate executor so QR confirm calls
		// back into this handler. Idempotent — calling twice with the
		// same handler is a harmless overwrite.
		qr.WithDelegateExecutor(h)
	}
	return h
}

// appsView is the API response shape — flat + JSON-stable so the React
// page can render directly without further massage.
type appsView struct {
	Org      string         `json:"org"`
	Project  string         `json:"project"`
	LiveSync bool           `json:"live_sync"` // true when the Zitadel-side column is populated
	Apps     []appEntryView `json:"apps"`
}

type appEntryView struct {
	Name         string            `json:"name"`
	DisplayName  string            `json:"display_name"`
	Enabled      bool              `json:"enabled"`
	AuthMethod   string            `json:"auth_method"`
	AppType      string            `json:"app_type"`
	Environments []environmentView `json:"environments"`
}

type environmentView struct {
	Env             string `json:"env"`
	Domain          string `json:"domain"`
	RedirectURI     string `json:"redirect_uri"`
	SecretNamespace string `json:"secret_namespace"`
	SecretName      string `json:"secret_name"`
	// ZitadelAppID is non-empty when the Zitadel-side lookup succeeded
	// (i.e. the reconciler has created the app). Empty when Zitadel is
	// unreachable or the app hasn't been provisioned yet.
	ZitadelAppID string `json:"zitadel_app_id,omitempty"`
	// ClientIDPreview is the first 12 chars of the OIDC client_id —
	// sufficient to correlate with logs / secrets without exposing the
	// full identifier in UIs that may be viewed over shoulder surfing.
	ClientIDPreview string `json:"client_id_preview,omitempty"`
}

// List returns the current apps.yaml joined with Zitadel state.
// GET /api/v1/admin/apps  (admin-JWT required — wired at the router)
func (h *AppsAdminHandler) List(c *gin.Context) {
	spec, err := app_registry.LoadSpec(h.configPath)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "config_unavailable",
			"message": "apps.yaml could not be loaded — app_registry may be unconfigured",
		})
		return
	}

	view := appsView{
		Org:      spec.Org,
		Project:  spec.Project,
		LiveSync: h.zitadel != nil,
		Apps:     make([]appEntryView, 0, len(spec.Apps)),
	}

	var projectID string
	if h.zitadel != nil {
		if pid, err := h.zitadel.LookupProject(c.Request.Context(), spec.Project); err == nil && pid != "" {
			projectID = pid
		} else {
			view.LiveSync = false
		}
	}

	for _, app := range spec.Apps {
		entry := appEntryView{
			Name:         app.Name,
			DisplayName:  app.DisplayName,
			Enabled:      app.IsEnabled(),
			AuthMethod:   app.OIDC.AuthMethod,
			AppType:      app.OIDC.AppType,
			Environments: make([]environmentView, 0, len(app.Environments)),
		}
		for _, env := range app.Environments {
			ev := environmentView{
				Env:             env.Env,
				Domain:          env.Domain,
				RedirectURI:     env.RedirectURI(app.OIDC),
				SecretNamespace: env.Secret.Namespace,
				SecretName:      env.Secret.Name,
			}
			if projectID != "" && app.IsEnabled() {
				zitAppName := app.Name + "-" + env.Env
				if clientID, appID := h.lookupZitadelApp(c, projectID, zitAppName); clientID != "" {
					ev.ZitadelAppID = appID
					ev.ClientIDPreview = previewClientID(clientID)
				}
			}
			entry.Environments = append(entry.Environments, ev)
		}
		view.Apps = append(view.Apps, entry)
	}

	c.JSON(http.StatusOK, view)
}

// lookupZitadelApp returns (client_id, app_id) for a given app name in
// the project. Pure read — never creates or mutates Zitadel state.
func (h *AppsAdminHandler) lookupZitadelApp(c *gin.Context, projectID, appName string) (string, string) {
	creds, err := h.zitadel.LookupOIDCApp(c.Request.Context(), projectID, appName)
	if err != nil || creds == nil {
		return "", ""
	}
	return creds.ClientID, creds.AppID
}

// previewClientID truncates a client_id for display.
func previewClientID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

// ── Manual client_secret rotation (Phase 3 / Track 2) ──────────────────────

// rotateSecretResponse is the JSON shape returned to the UI after a
// successful manual rotation. NextDueAt is a hint only — the
// reconciler is the final authority on when the next auto-rotation
// fires; this value lets the UI display "next: in 90 days" without a
// second round-trip.
type rotateSecretResponse struct {
	App       string `json:"app"`
	Env       string `json:"env"`
	RotatedAt string `json:"rotated_at"`            // RFC3339 UTC
	NextDueAt string `json:"next_due_at,omitempty"` // RFC3339 UTC; empty when rotation is not auto-scheduled
	Trigger   string `json:"trigger"`               // always "manual" for this endpoint
}

// RotateSecret triggers an immediate rotation of one (app, env)'s OIDC
// client_secret.
//
//	POST /admin/v1/apps/:name/:env/rotate-secret  (admin JWT required)
func (h *AppsAdminHandler) RotateSecret(c *gin.Context) {
	if h.recon == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "rotation_unavailable",
			"message": "app_registry reconciler is not wired — verify Zitadel PAT, in-cluster ServiceAccount, and apps.yaml",
		})
		return
	}
	appName := c.Param("name")
	envName := c.Param("env")
	if appName == "" || envName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "app name and env path params are required",
		})
		return
	}

	rotatedAt, err := h.recon.RotateOnce(c.Request.Context(), appName, envName)
	if err != nil {
		msg := err.Error()
		switch {
		case rotateErrContains(msg, "not declared in apps.yaml"):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": msg})
		case rotateErrContains(msg, "only 'basic' has a client_secret", "no secret.client_secret_key"):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_target", "message": msg})
		case rotateErrContains(msg, "not provisioned in zitadel"):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_provisioned", "message": msg})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": "rotation_failed", "message": msg})
		}
		return
	}

	resp := rotateSecretResponse{
		App:       appName,
		Env:       envName,
		RotatedAt: rotatedAt.UTC().Format(time.RFC3339),
		Trigger:   "manual",
	}
	if interval := h.recon.SecretRotationInterval(appName, envName); interval > 0 {
		resp.NextDueAt = rotatedAt.Add(interval).UTC().Format(time.RFC3339)
	}
	c.JSON(http.StatusOK, resp)
}

// rotateErrContains matches a RotateOnce error string against a fixed
// list of substrings.
func rotateErrContains(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// ── QR-delegate destructive flow (Phase 3 / Track 1) ───────────────────────

// deleteRequestResponse mirrors the QR session shape so the Web UI can
// render a QR immediately without an extra round-trip.
type deleteRequestResponse struct {
	ID        string `json:"id"`
	QRPayload string `json:"qr_payload"`
	ExpiresAt string `json:"expires_at"`
	ExpiresIn int    `json:"expires_in"`
	App       string `json:"app"`
	Env       string `json:"env"`
}

// DeleteRequest — POST /admin/v1/apps/:name/:env/delete-request
//
// Mints a delegate-action QR session for "delete this OIDC app". The
// caller is already authenticated as an admin by AdminAuth on the
// /admin/v1 router group. The Zitadel app is not touched here — that
// happens only when the scanner confirms on the APP (via Confirm →
// confirmDelegate → ExecuteDelegate).
func (h *AppsAdminHandler) DeleteRequest(c *gin.Context) {
	if h.qr == nil || h.k8s == nil || h.tombstones == nil || h.zitadel == nil {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error":   "delete_flow_not_wired",
			"message": "QR-delegate delete flow is not configured on this deployment",
		})
		return
	}
	callerID, ok := requireAccountID(c)
	if !ok {
		return
	}
	name := c.Param("name")
	env := c.Param("env")
	if name == "" || env == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "Both :name and :env path params are required",
		})
		return
	}

	spec, err := app_registry.LoadSpec(h.configPath)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "config_unavailable",
			"message": "apps.yaml could not be loaded",
		})
		return
	}
	if !appEnvDeclared(spec, name, env) {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "app_env_not_found",
			"message": fmt.Sprintf("(%s, %s) is not declared in apps.yaml", name, env),
		})
		return
	}

	session, err := h.qr.CreateDelegateSession(c.Request.Context(), callerID, qrDelegateOpDeleteOIDCApp, name, env)
	if err != nil {
		if errors.Is(err, ErrUnsupportedDelegateOp) {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error":   "delete_flow_not_wired",
				"message": err.Error(),
			})
			return
		}
		respondInternalError(c, "AppsAdmin.DeleteRequest", err)
		return
	}
	slog.InfoContext(c.Request.Context(), "apps_admin.delete_requested",
		"app", name, "env", env, "initiator", callerID, "session_id", session.ID)

	c.JSON(http.StatusOK, deleteRequestResponse{
		ID:        session.ID,
		QRPayload: session.QRPayload,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
		ExpiresIn: session.ExpiresIn,
		App:       name,
		Env:       env,
	})
}

// appEnvDeclared returns true when the spec contains a matching app
// slug + environment tag.
func appEnvDeclared(spec *app_registry.Spec, name, env string) bool {
	for _, app := range spec.Apps {
		if app.Name != name {
			continue
		}
		for _, e := range app.Environments {
			if e.Env == env {
				return true
			}
		}
		return false
	}
	return false
}

// ── QRDelegateExecutor implementation ───────────────────────────────────────

// SupportedOps returns the delegate ops this executor handles.
func (h *AppsAdminHandler) SupportedOps() []string {
	return []string{qrDelegateOpDeleteOIDCApp}
}

// ExecuteDelegate runs the destructive side effect after the boss has
// confirmed on his APP.
func (h *AppsAdminHandler) ExecuteDelegate(ctx context.Context, op, app, env string, _ int64) error {
	if op != qrDelegateOpDeleteOIDCApp {
		return fmt.Errorf("%w: %q", ErrUnsupportedDelegateOp, op)
	}
	if h.zitadel == nil || h.k8s == nil || h.tombstones == nil {
		return errors.New("apps_admin: delete flow dependencies not wired")
	}

	spec, err := app_registry.LoadSpec(h.configPath)
	if err != nil {
		return fmt.Errorf("apps_admin: load spec: %w", err)
	}
	target, targetEnv, ok := findAppEnv(spec, app, env)
	if !ok {
		return fmt.Errorf("apps_admin: (%s, %s) not declared in apps.yaml", app, env)
	}

	projectID, err := h.zitadel.LookupProject(ctx, spec.Project)
	if err != nil {
		return fmt.Errorf("apps_admin: lookup project: %w", err)
	}
	if projectID == "" {
		slog.WarnContext(ctx, "apps_admin.delete: project not found, skipping zitadel delete",
			"project", spec.Project, "app", app, "env", env)
	} else {
		zitAppName := target.Name + "-" + targetEnv.Env
		creds, lookupErr := h.zitadel.LookupOIDCApp(ctx, projectID, zitAppName)
		if lookupErr != nil {
			return fmt.Errorf("apps_admin: lookup oidc app: %w", lookupErr)
		}
		if creds == nil {
			slog.WarnContext(ctx, "apps_admin.delete: oidc app already absent",
				"project", spec.Project, "app", app, "env", env, "zit_app_name", zitAppName)
		} else if delErr := h.zitadel.DeleteOIDCApp(ctx, projectID, creds.AppID); delErr != nil {
			return fmt.Errorf("apps_admin: delete oidc app: %w", delErr)
		}
	}

	keysToDrop := []string{}
	if k := targetEnv.Secret.ClientIDKey; k != "" {
		keysToDrop = append(keysToDrop, k)
	}
	if k := targetEnv.Secret.ClientSecretKey; k != "" {
		keysToDrop = append(keysToDrop, k)
	}
	if len(keysToDrop) > 0 {
		removed, secretErr := h.k8s.RemoveSecretKeys(ctx, targetEnv.Secret.Namespace, targetEnv.Secret.Name, keysToDrop...)
		if secretErr != nil {
			return fmt.Errorf("apps_admin: remove secret keys: %w", secretErr)
		}
		slog.InfoContext(ctx, "apps_admin.delete: secret keys removed",
			"namespace", targetEnv.Secret.Namespace, "secret", targetEnv.Secret.Name,
			"keys_removed", removed, "keys_requested", len(keysToDrop))
	}

	if err := h.tombstones.Mark(ctx, app, env); err != nil {
		// Tombstone failure is not fatal — Zitadel + Secret already
		// cleaned up. Log loudly so operators can manually pause the
		// reconciler if needed; but return success so the user sees the
		// destructive action took effect.
		slog.WarnContext(ctx, "apps_admin.delete: tombstone mark failed (reconciler may recreate)",
			"app", app, "env", env, "err", err)
	}
	return nil
}

// findAppEnv returns the matching App + Environment pair from the spec
// or ok=false when nothing matches.
func findAppEnv(spec *app_registry.Spec, name, env string) (app_registry.App, app_registry.Environment, bool) {
	for _, a := range spec.Apps {
		if a.Name != name {
			continue
		}
		for _, e := range a.Environments {
			if e.Env == env {
				return a, e, true
			}
		}
	}
	return app_registry.App{}, app_registry.Environment{}, false
}
