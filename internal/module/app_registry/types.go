// Package app_registry reconciles a declarative list of OIDC applications
// against Zitadel (Management API) and Kubernetes (Secret + Deployment).
//
// The declarative source is config/apps.yaml; the reconciler ensures each
// declared app has a matching Zitadel OIDC application, captures the
// issued client_id, writes it to the named K8s Secret in the target
// namespace, and bounces the associated Deployment so the new client_id
// is loaded. The whole loop is idempotent: when live state already
// matches the declaration every phase is a no-op.
//
// Phase 1 does NOT delete live Zitadel apps when an entry is removed
// from YAML — destructive operations are scheduled for Phase 2 via the
// QR `delegate` action so a human approves each deletion on their APP.
package app_registry

// Spec is the top-level structure parsed from apps.yaml.
type Spec struct {
	// Org is the Zitadel organization slug. All apps managed by this
	// reconciler live under a single org; cross-org moves require a
	// manual procedure.
	Org string `yaml:"org"`

	// Project is the Zitadel project that groups all managed OIDC apps.
	// Splitting into multiple projects needs a YAML schema change.
	Project string `yaml:"project"`

	// Apps is the list of applications to ensure exist in Zitadel.
	Apps []App `yaml:"apps"`
}

// App is one logical application; it may span multiple environments
// (stage / prod) each of which is materialised as a separate Zitadel
// OIDC application because Zitadel forbids overlapping redirect URIs
// within a single app.
type App struct {
	// Name is the stable slug used for Zitadel app naming and secret
	// lookup (e.g. "tally", "admin"). Lowercase, no spaces.
	Name string `yaml:"name"`

	// DisplayName is the human-readable label shown in Zitadel UI.
	// Optional; falls back to Name.
	DisplayName string `yaml:"display_name,omitempty"`

	// Enabled gates reconciliation. Disabled entries are accepted in
	// the parser (so YAML stays valid across phased rollouts) but the
	// reconciler skips them. Default true.
	Enabled *bool `yaml:"enabled,omitempty"`

	// Environments lists one (env, domain, secret) tuple per stage.
	Environments []Environment `yaml:"environments"`

	// OIDC carries flow-level settings shared across environments.
	OIDC OIDCSettings `yaml:"oidc"`

	// SecretRotation is the optional auto-rotation policy for confidential
	// clients (auth_method=basic). Ignored for PKCE/public clients which
	// have no client_secret. When Enabled is false (or the whole stanza
	// is omitted) the reconciler never touches client_secret after the
	// initial provisioning — operators rotate manually via the admin UI.
	SecretRotation SecretRotation `yaml:"secret_rotation,omitempty"`
}

// SecretRotation declares the auto-rotation policy for an App's
// client_secret. Only consulted when OIDC.AuthMethod == "basic".
type SecretRotation struct {
	// Enabled gates the auto-rotation loop. When false the reconciler
	// leaves the existing secret in place; manual rotation via the admin
	// endpoint still works.
	Enabled bool `yaml:"enabled,omitempty"`

	// IntervalDays is the maximum age allowed for a client_secret before
	// the reconciler rotates it on the next tick. Required when Enabled.
	// Validation enforces > 0 to avoid an accidental rotate-every-tick
	// runaway.
	IntervalDays int `yaml:"interval_days,omitempty"`
}

// Environment is one stage of an App — a unique domain + Secret location
// + deployment to bounce when the client_id changes.
type Environment struct {
	// Env is a free-form tag ("stage", "prod"); currently used only
	// for log context and uniqueness checks.
	Env string `yaml:"env"`

	// Domain is the public host this env serves. Drives the redirect
	// URI and post-logout URI unless explicit overrides are given.
	Domain string `yaml:"domain"`

	// Secret names the K8s Secret that will receive client_id (and
	// optionally client_secret) under the declared key names.
	Secret SecretTarget `yaml:"secret"`

	// RestartDeployment is the name of the Deployment to rollout-
	// restart after a Secret update. Empty = don't restart; the caller
	// must have their own mechanism to pick up the change.
	RestartDeployment string `yaml:"restart_deployment,omitempty"`
}

// SecretTarget describes where to persist the issued OIDC credentials.
// All keys are under data:<key>, base64-encoded as usual for K8s Secrets.
type SecretTarget struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
	// ClientIDKey is the key in Secret.data that will hold the issued
	// client_id. Required.
	ClientIDKey string `yaml:"client_id_key"`
	// ClientSecretKey is populated for confidential clients only
	// (auth_method=basic). Empty for PKCE-only apps.
	ClientSecretKey string `yaml:"client_secret_key,omitempty"`
}

// OIDCSettings is the shared OIDC flow configuration for an App.
// Reconciler uses these to call Zitadel Management API.
type OIDCSettings struct {
	// AppType is almost always "web" in our stack. Reserved for future
	// "native" (mobile) or "user_agent" (SPA) values.
	AppType string `yaml:"app_type"`

	// AuthMethod is "none" (PKCE — public client) or "basic" (confidential
	// client holding a secret). Historically the manual-setup footgun;
	// now enforced in YAML.
	AuthMethod string `yaml:"auth_method"`

	GrantTypes    []string `yaml:"grant_types"`
	ResponseTypes []string `yaml:"response_types"`

	// RedirectPath is appended to each environment's domain to form the
	// OIDC redirect URI. Leading slash required.
	RedirectPath string `yaml:"redirect_path"`

	// PostLogoutPath is appended to each environment's domain to form
	// the post-logout URI. Leading slash required.
	PostLogoutPath string `yaml:"post_logout_path"`
}

// IsEnabled returns false only when the YAML explicitly says so; a
// missing `enabled:` field is treated as enabled.
func (a App) IsEnabled() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

// RedirectURI builds the full redirect URI for an environment by
// joining its domain with the OIDC redirect_path.
func (e Environment) RedirectURI(o OIDCSettings) string {
	return "https://" + e.Domain + o.RedirectPath
}

// PostLogoutURI builds the full post-logout URI for an environment.
func (e Environment) PostLogoutURI(o OIDCSettings) string {
	return "https://" + e.Domain + o.PostLogoutPath
}
