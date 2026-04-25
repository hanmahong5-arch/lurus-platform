package app_registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/app_registry"
)

// writeYAML writes a temp file with the given content and returns the
// path. t.TempDir cleanup handles deletion.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return path
}

func TestLoadSpec_Happy(t *testing.T) {
	path := writeYAML(t, `
org: lurus
project: lurus-platform
apps:
  - name: tally
    environments:
      - env: stage
        domain: tally-stage.lurus.cn
        secret:
          namespace: lurus-tally
          name: tally-secrets
          client_id_key: ZITADEL_CLIENT_ID
        restart_deployment: tally-web
    oidc:
      app_type: web
      auth_method: none
      grant_types: [authorization_code, refresh_token]
      response_types: [code]
      redirect_path: /api/auth/callback/zitadel
      post_logout_path: /
`)
	spec, err := app_registry.LoadSpec(path)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if spec.Org != "lurus" || spec.Project != "lurus-platform" {
		t.Errorf("spec = %+v", spec)
	}
	if len(spec.Apps) != 1 {
		t.Fatalf("want 1 app, got %d", len(spec.Apps))
	}
	if !spec.Apps[0].IsEnabled() {
		t.Error("apps without explicit enabled flag should default to enabled")
	}
	if got := spec.Apps[0].Environments[0].RedirectURI(spec.Apps[0].OIDC); got != "https://tally-stage.lurus.cn/api/auth/callback/zitadel" {
		t.Errorf("redirect uri = %q", got)
	}
}

func TestLoadSpec_DisabledFlag(t *testing.T) {
	path := writeYAML(t, `
org: lurus
project: lurus-platform
apps:
  - name: admin
    enabled: false
    environments:
      - env: prod
        domain: admin.lurus.cn
        secret:
          namespace: lurus-admin
          name: admin-secrets
          client_id_key: ZITADEL_CLIENT_ID
          client_secret_key: ZITADEL_CLIENT_SECRET
    oidc:
      app_type: web
      auth_method: basic
      grant_types: [authorization_code, refresh_token]
      response_types: [code]
      redirect_path: /auth/callback
      post_logout_path: /
`)
	spec, err := app_registry.LoadSpec(path)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if spec.Apps[0].IsEnabled() {
		t.Error("explicit enabled: false should disable the app")
	}
}

// TestLoadSpec_Errors covers every validation path so a bad YAML fails
// fast at startup rather than running a broken reconciler.
func TestLoadSpec_Errors(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name:    "missing org",
			yaml:    "project: p\napps: []",
			wantSub: "org is required",
		},
		{
			name:    "missing project",
			yaml:    "org: o\napps: []",
			wantSub: "project is required",
		},
		{
			name:    "slug with uppercase",
			yaml:    fullYAML("Tally"),
			wantSub: "lowercase slug",
		},
		{
			name:    "duplicate app name",
			yaml:    dupYAML(),
			wantSub: "duplicate app name",
		},
		{
			name:    "missing redirect path slash",
			yaml:    strings.Replace(fullYAML("tally"), "/api/auth/callback/zitadel", "api/auth/callback/zitadel", 1),
			wantSub: "redirect_path must start with /",
		},
		{
			name: "basic auth without client_secret_key",
			yaml: strings.Replace(
				strings.Replace(fullYAML("tally"), "auth_method: none", "auth_method: basic", 1),
				"client_id_key: ZITADEL_CLIENT_ID",
				"client_id_key: ZITADEL_CLIENT_ID",
				1,
			),
			wantSub: "client_secret_key is required",
		},
		{
			name:    "bad app_type",
			yaml:    strings.Replace(fullYAML("tally"), "app_type: web", "app_type: mobile", 1),
			wantSub: "app_type",
		},
		{
			name:    "empty grant_types",
			yaml:    strings.Replace(fullYAML("tally"), "grant_types: [authorization_code, refresh_token]", "grant_types: []", 1),
			wantSub: "grant_types must have at least one entry",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeYAML(t, c.yaml)
			_, err := app_registry.LoadSpec(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("want error containing %q, got: %v", c.wantSub, err)
			}
		})
	}
}

func TestLoadSpec_FileMissing(t *testing.T) {
	_, err := app_registry.LoadSpec("/nonexistent/apps.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// fullYAML is a reusable valid YAML template with one app whose name is
// substituted from the caller. Mutating the output via strings.Replace
// lets the error tests target one invariant each.
func fullYAML(appName string) string {
	return `
org: lurus
project: lurus-platform
apps:
  - name: ` + appName + `
    environments:
      - env: stage
        domain: tally-stage.lurus.cn
        secret:
          namespace: lurus-tally
          name: tally-secrets
          client_id_key: ZITADEL_CLIENT_ID
    oidc:
      app_type: web
      auth_method: none
      grant_types: [authorization_code, refresh_token]
      response_types: [code]
      redirect_path: /api/auth/callback/zitadel
      post_logout_path: /
`
}

func dupYAML() string {
	return `
org: lurus
project: lurus-platform
apps:
  - name: tally
    environments:
      - env: stage
        domain: a.example.com
        secret: {namespace: ns, name: s, client_id_key: k}
    oidc:
      app_type: web
      auth_method: none
      grant_types: [authorization_code]
      response_types: [code]
      redirect_path: /cb
      post_logout_path: /
  - name: tally
    environments:
      - env: prod
        domain: b.example.com
        secret: {namespace: ns, name: s2, client_id_key: k}
    oidc:
      app_type: web
      auth_method: none
      grant_types: [authorization_code]
      response_types: [code]
      redirect_path: /cb
      post_logout_path: /
`
}
