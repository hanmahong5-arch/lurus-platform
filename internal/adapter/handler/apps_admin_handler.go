package handler

import (
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
// Write operations (create/delete) are deliberately NOT exposed here;
// apps.yaml remains the source of truth. Mutating live Zitadel directly
// from a UI would bypass the audit trail and the GitOps-reviewed PR
// flow. Phase 3 adds destructive ops via QR delegate approval.
type AppsAdminHandler struct {
	configPath string
	zitadel    *zitadel.Client
	// recon is optional — when nil the rotate endpoint short-circuits
	// to 503. Read-only listing still works because List uses the
	// declarative loader directly.
	recon *app_registry.Reconciler
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
		// LookupProject — pure read, never creates. An admin opening
		// the viewer page must never accidentally bootstrap missing
		// Zitadel state; that's the reconciler's job on its own loop.
		if pid, err := h.zitadel.LookupProject(c.Request.Context(), spec.Project); err == nil && pid != "" {
			projectID = pid
		} else {
			// Drop to YAML-only view so the page still renders when
			// Zitadel is transiently unreachable or the project hasn't
			// been created yet. live_sync=false in the response tells
			// the UI to hide the live columns.
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
				// Look up live Zitadel state. Failures are silently
				// dropped — the caller already knows live_sync status.
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
// the project. Errors and "not provisioned" both collapse to empty
// strings: the caller renders a "not yet provisioned" cell rather than
// failing the whole request. Crucially this is a PURE READ — unlike
// EnsureOIDCApp this never creates or mutates Zitadel state, so the
// viewer endpoint has no side effects.
func (h *AppsAdminHandler) lookupZitadelApp(c *gin.Context, projectID, appName string) (string, string) {
	creds, err := h.zitadel.LookupOIDCApp(c.Request.Context(), projectID, appName)
	if err != nil || creds == nil {
		return "", ""
	}
	return creds.ClientID, creds.AppID
}

// previewClientID truncates a client_id for display. Intentionally
// duplicated from app_registry.previewClientID so the handler package
// doesn't import the private helper.
func previewClientID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

// rotateSecretResponse is the JSON shape returned to the UI after a
// successful manual rotation. NextDueAt is a hint only — the
// reconciler is the final authority on when the next auto-rotation
// fires; this value lets the UI display "next: in 90 days" without a
// second round-trip.
type rotateSecretResponse struct {
	App        string `json:"app"`
	Env        string `json:"env"`
	RotatedAt  string `json:"rotated_at"`             // RFC3339 UTC
	NextDueAt  string `json:"next_due_at,omitempty"`  // RFC3339 UTC; empty when rotation is not auto-scheduled
	Trigger    string `json:"trigger"`                // always "manual" for this endpoint
}

// RotateSecret triggers an immediate rotation of one (app, env)'s OIDC
// client_secret.
//
//	POST /admin/v1/apps/:name/:env/rotate-secret  (admin JWT required)
//
// The handler is intentionally thin: parameter sanity checks → delegate
// to Reconciler.RotateOnce, which is the shared primitive that both the
// auto-rotation loop and this endpoint use. That keeps the audit trail,
// metric labels, and Zitadel/K8s/Redis side-effects identical between
// "operator hit the button" and "schedule fired".
//
// Errors map to:
//   - 503 when the reconciler isn't wired (Zitadel unconfigured, not in
//     a K8s pod, apps.yaml absent — operator can't fix at runtime).
//   - 404 when (app, env) isn't in apps.yaml or hasn't been provisioned
//     in Zitadel yet.
//   - 400 when the app exists but is not auth_method=basic (PKCE has no
//     client_secret to rotate).
//   - 502 for upstream Zitadel / K8s failures so the operator knows the
//     incident is on the dependency side, not on this endpoint.
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
		// Map well-known error shapes to specific HTTP statuses so the
		// UI can render actionable messages. Anything else is a 502 —
		// the rotation primitive itself is fine; an upstream is not.
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
// list of substrings. Named-scoped to this handler to avoid colliding
// with similar helpers in the package; the substrings themselves are
// stable contracts on app_registry.RotateOnce, locked in by the
// rotate-handler tests.
func rotateErrContains(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
