package handler

import (
	"net/http"

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
}

// NewAppsAdminHandler wires the handler. When zitadel is nil (PAT not
// configured) the handler still serves the declarative view but omits
// the live-state columns.
func NewAppsAdminHandler(configPath string, zit *zitadel.Client) *AppsAdminHandler {
	return &AppsAdminHandler{
		configPath: configPath,
		zitadel:    zit,
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
