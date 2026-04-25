package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// EnsureProject returns the Zitadel project id for the given project
// name, creating the project first if it doesn't already exist. The
// operation is idempotent: calling it twice with the same name returns
// the same id and makes no extra write.
func (c *Client) EnsureProject(ctx context.Context, name string) (string, error) {
	// 1. Look up by exact name.
	id, err := c.findProjectByName(ctx, name)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	// 2. Not found — create.
	return c.createProject(ctx, name)
}

func (c *Client) findProjectByName(ctx context.Context, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"queries": []map[string]any{
			{"nameQuery": map[string]any{
				"name":   name,
				"method": "TEXT_QUERY_METHOD_EQUALS",
			}},
		},
	})
	respBody, err := c.doManagement(ctx, http.MethodPost, "/management/v1/projects/_search", body)
	if err != nil {
		return "", err
	}
	var res struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("zitadel: decode project search: %w", err)
	}
	for _, p := range res.Result {
		if p.Name == name {
			return p.ID, nil
		}
	}
	return "", nil
}

func (c *Client) createProject(ctx context.Context, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{"name": name})
	respBody, err := c.doManagement(ctx, http.MethodPost, "/management/v1/projects", body)
	if err != nil {
		return "", err
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("zitadel: decode create project: %w", err)
	}
	if res.ID == "" {
		return "", fmt.Errorf("zitadel: create project returned empty id")
	}
	return res.ID, nil
}

// OIDCAppSpec describes the desired state of an OIDC application inside
// a Zitadel project. Fields map 1:1 to Zitadel Management API v1 OIDC
// app shape but use idiomatic Go terms (auth_method="none"|"basic"
// instead of the OIDC_AUTH_METHOD_TYPE_* enum values).
type OIDCAppSpec struct {
	Name                   string
	AppType                string   // web | native | user_agent
	AuthMethod             string   // none | basic
	GrantTypes             []string // authorization_code | refresh_token | implicit | device_code
	ResponseTypes          []string // code | id_token | id_token_token
	RedirectURIs           []string // full https://… URIs
	PostLogoutRedirectURIs []string
}

// OIDCAppCredentials is the subset of Zitadel's OIDC app response that
// callers care about. ClientSecret is only populated when AuthMethod is
// "basic" (confidential client); PKCE apps get client_id only.
type OIDCAppCredentials struct {
	AppID        string
	ClientID     string
	ClientSecret string
}

// EnsureOIDCApp reconciles one OIDC application inside the given project:
//   - If an app with the same name already exists, its OIDC config is
//     patched to match the desired spec; the existing client_id is
//     returned (Zitadel does not re-roll the client_id on config change).
//   - Otherwise the app is created fresh and the newly issued client_id
//     is returned.
//
// client_secret is returned only at create time for confidential apps;
// existing apps return an empty ClientSecret because Zitadel will not
// echo it back. Callers that need a new secret should call a dedicated
// rotate endpoint (out of scope for Phase 1).
func (c *Client) EnsureOIDCApp(ctx context.Context, projectID string, spec OIDCAppSpec) (*OIDCAppCredentials, error) {
	existingID, err := c.findOIDCAppByName(ctx, projectID, spec.Name)
	if err != nil {
		return nil, err
	}
	if existingID != "" {
		// Patch config + fetch client_id.
		if err := c.updateOIDCAppConfig(ctx, projectID, existingID, spec); err != nil {
			return nil, err
		}
		clientID, err := c.fetchOIDCClientID(ctx, projectID, existingID)
		if err != nil {
			return nil, err
		}
		return &OIDCAppCredentials{AppID: existingID, ClientID: clientID}, nil
	}
	return c.createOIDCApp(ctx, projectID, spec)
}

func (c *Client) findOIDCAppByName(ctx context.Context, projectID, name string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"queries": []map[string]any{
			{"nameQuery": map[string]any{
				"name":   name,
				"method": "TEXT_QUERY_METHOD_EQUALS",
			}},
		},
	})
	path := "/management/v1/projects/" + projectID + "/apps/_search"
	respBody, err := c.doManagement(ctx, http.MethodPost, path, body)
	if err != nil {
		return "", err
	}
	var res struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("zitadel: decode app search: %w", err)
	}
	for _, a := range res.Result {
		if a.Name == name {
			return a.ID, nil
		}
	}
	return "", nil
}

func (c *Client) createOIDCApp(ctx context.Context, projectID string, spec OIDCAppSpec) (*OIDCAppCredentials, error) {
	body, _ := json.Marshal(oidcBody(spec))
	path := "/management/v1/projects/" + projectID + "/apps/oidc"
	respBody, err := c.doManagement(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	var res struct {
		AppID        string `json:"appId"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("zitadel: decode create oidc app: %w", err)
	}
	if res.ClientID == "" {
		return nil, fmt.Errorf("zitadel: create oidc app returned empty client_id")
	}
	return &OIDCAppCredentials{
		AppID:        res.AppID,
		ClientID:     res.ClientID,
		ClientSecret: res.ClientSecret,
	}, nil
}

func (c *Client) updateOIDCAppConfig(ctx context.Context, projectID, appID string, spec OIDCAppSpec) error {
	body, _ := json.Marshal(oidcBody(spec))
	path := "/management/v1/projects/" + projectID + "/apps/" + appID + "/oidc_config"
	_, err := c.doManagement(ctx, http.MethodPut, path, body)
	// Zitadel returns 400 "No changes (COMMAND-1m88i)" when the live config
	// already matches the requested state. That's the steady-state outcome
	// of an idempotent reconciler, not a failure — collapse it to nil.
	if err != nil && isZitadelNoChangesError(err) {
		return nil
	}
	return err
}

// isZitadelNoChangesError detects Zitadel's "No changes" 400 response.
// We string-match the COMMAND error id rather than the human message to
// stay robust against future i18n / wording tweaks.
func isZitadelNoChangesError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "COMMAND-1m88i")
}

func (c *Client) fetchOIDCClientID(ctx context.Context, projectID, appID string) (string, error) {
	path := "/management/v1/projects/" + projectID + "/apps/" + appID
	respBody, err := c.doManagement(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var res struct {
		App struct {
			OIDCConfig struct {
				ClientID string `json:"clientId"`
			} `json:"oidcConfig"`
		} `json:"app"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("zitadel: decode get oidc app: %w", err)
	}
	return res.App.OIDCConfig.ClientID, nil
}

// oidcBody translates an OIDCAppSpec into the exact JSON shape Zitadel
// Management API expects. The enum values are Zitadel's OIDC_* constants
// (not OIDC standard names) so the mapping lives here, not in callers.
func oidcBody(spec OIDCAppSpec) map[string]any {
	return map[string]any{
		"name":                   spec.Name,
		"redirectUris":           spec.RedirectURIs,
		"postLogoutRedirectUris": spec.PostLogoutRedirectURIs,
		"responseTypes":          toZitadelResponseTypes(spec.ResponseTypes),
		"grantTypes":             toZitadelGrantTypes(spec.GrantTypes),
		"appType":                toZitadelAppType(spec.AppType),
		"authMethodType":         toZitadelAuthMethod(spec.AuthMethod),
	}
}

func toZitadelAppType(s string) string {
	switch strings.ToLower(s) {
	case "web":
		return "OIDC_APP_TYPE_WEB"
	case "native":
		return "OIDC_APP_TYPE_NATIVE"
	case "user_agent":
		return "OIDC_APP_TYPE_USER_AGENT"
	}
	return "OIDC_APP_TYPE_WEB"
}

func toZitadelAuthMethod(s string) string {
	switch strings.ToLower(s) {
	case "basic":
		return "OIDC_AUTH_METHOD_TYPE_BASIC"
	case "post":
		return "OIDC_AUTH_METHOD_TYPE_POST"
	case "private_key_jwt":
		return "OIDC_AUTH_METHOD_TYPE_PRIVATE_KEY_JWT"
	}
	// Default and explicit "none" both map to PKCE-style public client.
	return "OIDC_AUTH_METHOD_TYPE_NONE"
}

func toZitadelGrantTypes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		switch strings.ToLower(g) {
		case "authorization_code":
			out = append(out, "OIDC_GRANT_TYPE_AUTHORIZATION_CODE")
		case "refresh_token":
			out = append(out, "OIDC_GRANT_TYPE_REFRESH_TOKEN")
		case "implicit":
			out = append(out, "OIDC_GRANT_TYPE_IMPLICIT")
		case "device_code":
			out = append(out, "OIDC_GRANT_TYPE_DEVICE_CODE")
		}
	}
	return out
}

func toZitadelResponseTypes(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		switch strings.ToLower(r) {
		case "code":
			out = append(out, "OIDC_RESPONSE_TYPE_CODE")
		case "id_token":
			out = append(out, "OIDC_RESPONSE_TYPE_ID_TOKEN")
		case "id_token_token":
			out = append(out, "OIDC_RESPONSE_TYPE_ID_TOKEN_TOKEN")
		}
	}
	return out
}

// doManagement is a shared HTTP helper for the Zitadel Management API v1
// endpoints added by app_registry. Handles auth, body serialisation, and
// bounded response reading so every caller gets the same error surface.
func (c *Client) doManagement(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, c.issuer+path, reader)
	if err != nil {
		return nil, fmt.Errorf("zitadel: build request %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zitadel: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("zitadel: %s %s returned %d: %s", method, path, resp.StatusCode, respBody)
	}
	return respBody, nil
}
