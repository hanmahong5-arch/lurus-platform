package app_registry

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadSpec reads the given path and returns a validated Spec. Invalid
// YAML, duplicate app names, or missing required fields all produce an
// error — the caller should fail-fast on startup rather than run with a
// broken config.
func LoadSpec(path string) (*Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var spec Spec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := validate(&spec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &spec, nil
}

// validate enforces schema invariants that yaml.Unmarshal alone does not
// catch. Runs only at load time; the reconciler trusts the returned Spec.
func validate(s *Spec) error {
	if strings.TrimSpace(s.Org) == "" {
		return fmt.Errorf("org is required")
	}
	if strings.TrimSpace(s.Project) == "" {
		return fmt.Errorf("project is required")
	}
	seenApps := map[string]bool{}
	for i, app := range s.Apps {
		if err := validateApp(app); err != nil {
			return fmt.Errorf("apps[%d] (%q): %w", i, app.Name, err)
		}
		if seenApps[app.Name] {
			return fmt.Errorf("duplicate app name %q", app.Name)
		}
		seenApps[app.Name] = true
	}
	return nil
}

func validateApp(a App) error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !isSlug(a.Name) {
		return fmt.Errorf("name %q must be lowercase slug (a-z0-9-)", a.Name)
	}
	if len(a.Environments) == 0 {
		return fmt.Errorf("at least one environment is required")
	}
	if err := validateOIDC(a.OIDC, a.Name); err != nil {
		return err
	}
	seenEnvs := map[string]bool{}
	seenDomains := map[string]bool{}
	for i, env := range a.Environments {
		if err := validateEnv(env, a.OIDC); err != nil {
			return fmt.Errorf("environments[%d] (%q): %w", i, env.Env, err)
		}
		if seenEnvs[env.Env] {
			return fmt.Errorf("duplicate env %q", env.Env)
		}
		if seenDomains[env.Domain] {
			return fmt.Errorf("duplicate domain %q across envs", env.Domain)
		}
		seenEnvs[env.Env] = true
		seenDomains[env.Domain] = true
	}
	return nil
}

func validateEnv(e Environment, o OIDCSettings) error {
	if strings.TrimSpace(e.Env) == "" {
		return fmt.Errorf("env tag is required")
	}
	if strings.TrimSpace(e.Domain) == "" {
		return fmt.Errorf("domain is required")
	}
	if strings.TrimSpace(e.Secret.Namespace) == "" {
		return fmt.Errorf("secret.namespace is required")
	}
	if strings.TrimSpace(e.Secret.Name) == "" {
		return fmt.Errorf("secret.name is required")
	}
	if strings.TrimSpace(e.Secret.ClientIDKey) == "" {
		return fmt.Errorf("secret.client_id_key is required")
	}
	// Confidential clients must declare where the secret lands.
	if o.AuthMethod == "basic" && strings.TrimSpace(e.Secret.ClientSecretKey) == "" {
		return fmt.Errorf("secret.client_secret_key is required when oidc.auth_method=basic")
	}
	return nil
}

func validateOIDC(o OIDCSettings, appName string) error {
	switch o.AppType {
	case "web", "native", "user_agent":
	default:
		return fmt.Errorf("oidc.app_type %q is not one of web|native|user_agent (app=%s)", o.AppType, appName)
	}
	switch o.AuthMethod {
	case "none", "basic":
	default:
		return fmt.Errorf("oidc.auth_method %q is not one of none|basic (app=%s)", o.AuthMethod, appName)
	}
	if len(o.GrantTypes) == 0 {
		return fmt.Errorf("oidc.grant_types must have at least one entry (app=%s)", appName)
	}
	if len(o.ResponseTypes) == 0 {
		return fmt.Errorf("oidc.response_types must have at least one entry (app=%s)", appName)
	}
	if !strings.HasPrefix(o.RedirectPath, "/") {
		return fmt.Errorf("oidc.redirect_path must start with / (app=%s)", appName)
	}
	if !strings.HasPrefix(o.PostLogoutPath, "/") {
		return fmt.Errorf("oidc.post_logout_path must start with / (app=%s)", appName)
	}
	return nil
}

// isSlug returns true when s is a non-empty string of lowercase letters,
// digits, and hyphens. Used to keep Zitadel app names deterministic and
// URL-safe.
func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}
