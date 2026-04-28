// Package zitadel provides a client for the Zitadel Management API v2.
package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	requestTimeout = 10 * time.Second
	maxResponseBody = 8192
)

// Client wraps the Zitadel Management API for user provisioning.
type Client struct {
	issuer string // e.g. https://auth.lurus.cn
	pat    string // service account PAT
	http   *http.Client
}

// NewClient creates a Zitadel API client.
// Returns nil if pat is empty (Zitadel user management disabled).
func NewClient(issuer, pat string) *Client {
	if pat == "" {
		return nil
	}
	return &Client{
		issuer: issuer,
		pat:    pat,
		http:   &http.Client{Timeout: requestTimeout},
	}
}

// CreatedUser holds the result of creating a user in Zitadel.
type CreatedUser struct {
	UserID string `json:"userId"`
}

// CreateHumanUser creates a human user in Zitadel via POST /v2/users/human.
// Returns the Zitadel user ID on success.
func (c *Client) CreateHumanUser(ctx context.Context, email, password string) (*CreatedUser, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"username": email,
		"profile": map[string]any{
			"givenName":  "User",
			"familyName": "Lurus",
		},
		"email": map[string]any{
			"email": email,
			// Do not force email verification on registration — can be done later.
			"isVerified": false,
		},
		"password": map[string]any{
			"password":       password,
			"changeRequired": false,
		},
	})

	url := c.issuer + "/v2/users/human"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zitadel: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zitadel: create user request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("zitadel: user already exists in Zitadel (email=%s)", email)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("zitadel: create user returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("zitadel: decode response: %w", err)
	}
	if result.UserID == "" {
		return nil, fmt.Errorf("zitadel: empty userId in response: %s", respBody)
	}

	return &CreatedUser{UserID: result.UserID}, nil
}

// CreateHumanUserWithUsername creates a human user in Zitadel with a custom username.
// If email is empty, a placeholder noreply address is used.
func (c *Client) CreateHumanUserWithUsername(ctx context.Context, username, password, email string) (*CreatedUser, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	if email == "" {
		email = username + "@noreply.lurus.cn"
	}

	body, _ := json.Marshal(map[string]any{
		"username": username,
		"profile": map[string]any{
			"givenName":  username,
			"familyName": "Lurus",
		},
		"email": map[string]any{
			"email":      email,
			"isVerified": false,
		},
		"password": map[string]any{
			"password":       password,
			"changeRequired": false,
		},
	})

	url := c.issuer + "/v2/users/human"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zitadel: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zitadel: create user request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("zitadel: user already exists in Zitadel (username=%s)", username)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("zitadel: create user returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("zitadel: decode response: %w", err)
	}
	if result.UserID == "" {
		return nil, fmt.Errorf("zitadel: empty userId in response: %s", respBody)
	}

	return &CreatedUser{UserID: result.UserID}, nil
}

// RequestPasswordReset sends a password reset request via Zitadel.
// POST /v2/users/{userId}/password_reset
// Returns the verification code if returnCode is true, or sends email via Zitadel.
func (c *Client) RequestPasswordReset(ctx context.Context, userID string, returnCode bool) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	payload := map[string]any{}
	if returnCode {
		payload["returnCode"] = map[string]any{}
	} else {
		payload["sendLink"] = map[string]any{}
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/v2/users/%s/password_reset", c.issuer, userID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("zitadel: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("zitadel: password reset request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("zitadel: password reset returned %d: %s", resp.StatusCode, respBody)
	}

	if returnCode {
		var result struct {
			VerificationCode string `json:"verificationCode"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", fmt.Errorf("zitadel: decode reset response: %w", err)
		}
		return result.VerificationCode, nil
	}
	return "", nil
}

// SetNewPassword sets a new password for a user using a verification code.
// POST /v2/users/{userId}/password
func (c *Client) SetNewPassword(ctx context.Context, userID, verificationCode, newPassword string) error {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"newPassword": map[string]any{
			"password":       newPassword,
			"changeRequired": false,
		},
		"verificationCode": verificationCode,
	})

	url := fmt.Sprintf("%s/v2/users/%s/password", c.issuer, userID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("zitadel: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel: set password request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("zitadel: set password returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// DeactivateUser disables a Zitadel user so subsequent OIDC logins
// for that subject are rejected. POST /v2/users/:userID/deactivate.
//
// Idempotent: if the user is already inactive Zitadel returns 412
// "user is in invalid state" — we treat that as success. Likewise if
// the user no longer exists (404) — the desired end-state already
// holds. Any other 4xx/5xx surfaces as an error so the caller can
// flip the audit row to 'failed'.
func (c *Client) DeactivateUser(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("zitadel: deactivate user requires non-empty userID")
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := c.issuer + "/v2/users/" + userID + "/deactivate"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return fmt.Errorf("zitadel: build deactivate request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel: deactivate user request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		// Already gone — purge cascade is idempotent on this branch.
		return nil
	case http.StatusPreconditionFailed:
		// "user is in invalid state" — already deactivated. Treat as
		// success so a re-run of the cascade doesn't mark the audit
		// row failed.
		return nil
	default:
		return fmt.Errorf("zitadel: deactivate user returned %d: %s", resp.StatusCode, respBody)
	}
}

// FindUserByEmail searches for a user by email.
// POST /v2/users (list with filter)
// Returns the Zitadel user ID, or empty string if not found.
func (c *Client) FindUserByEmail(ctx context.Context, email string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"queries": []map[string]any{
			{
				"emailQuery": map[string]any{
					"emailAddress": email,
					"method":       "TEXT_QUERY_METHOD_EQUALS",
				},
			},
		},
	})

	url := c.issuer + "/v2/users"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("zitadel: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("zitadel: find user request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zitadel: find user returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Result []struct {
			UserID string `json:"userId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("zitadel: decode find user response: %w", err)
	}
	if len(result.Result) == 0 {
		return "", nil
	}
	return result.Result[0].UserID, nil
}

// CreateMachineUser provisions a machine (service) user via the
// management API. POST /management/v1/users/machine.
//
// username is the stable identifier (must be unique within the org);
// displayName is shown in the Zitadel console. description is free-form.
//
// accessTokenType is "BEARER" (default) — Zitadel also supports JWT but
// callers using Lurus's API-key abstraction never need that variant.
//
// Returns Zitadel's userId on success; surfaces 409 as a typed error so
// the caller can decide whether to look up the existing user.
func (c *Client) CreateMachineUser(ctx context.Context, username, displayName, description string) (string, error) {
	if username == "" || displayName == "" {
		return "", fmt.Errorf("zitadel: create machine user requires non-empty username + displayName")
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"userName":        username,
		"name":            displayName,
		"description":     description,
		"accessTokenType": "ACCESS_TOKEN_TYPE_BEARER",
	})

	url := c.issuer + "/management/v1/users/machine"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("zitadel: build create-machine request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("zitadel: create machine user request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var out struct {
			UserID string `json:"userId"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return "", fmt.Errorf("zitadel: decode create machine response: %w (body=%s)", err, respBody)
		}
		if out.UserID == "" {
			return "", fmt.Errorf("zitadel: create machine returned empty userId (body=%s)", respBody)
		}
		return out.UserID, nil
	case http.StatusConflict:
		return "", fmt.Errorf("zitadel: machine user already exists (username=%s)", username)
	default:
		return "", fmt.Errorf("zitadel: create machine user returned %d: %s", resp.StatusCode, respBody)
	}
}

// CreatePAT issues a Personal Access Token for a machine user.
// POST /management/v1/users/{userId}/pats.
//
// expiresAt is required; pass time.Time{} for "never expires" (Zitadel
// uses absent expirationDate for that). Returns the PAT's tokenId AND
// the token string itself — the latter is shown only once and the
// caller must persist or hand it off immediately.
func (c *Client) CreatePAT(ctx context.Context, userID string, expiresAt time.Time) (tokenID, token string, err error) {
	if userID == "" {
		return "", "", fmt.Errorf("zitadel: create PAT requires userID")
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	payload := map[string]any{}
	if !expiresAt.IsZero() {
		// Zitadel expects RFC3339 timestamp.
		payload["expirationDate"] = expiresAt.UTC().Format(time.RFC3339)
	}
	body, _ := json.Marshal(payload)

	url := c.issuer + "/management/v1/users/" + userID + "/pats"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("zitadel: build create-pat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zitadel: create PAT request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("zitadel: create PAT returned %d: %s", resp.StatusCode, respBody)
	}

	var out struct {
		TokenID string `json:"tokenId"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", fmt.Errorf("zitadel: decode create PAT response: %w (body=%s)", err, respBody)
	}
	if out.Token == "" {
		return "", "", fmt.Errorf("zitadel: create PAT returned empty token (body=%s)", respBody)
	}
	return out.TokenID, out.Token, nil
}

// DeletePAT revokes a single PAT by id.
// DELETE /management/v1/users/{userId}/pats/{tokenId}.
//
// 404 is treated as success (already revoked / never existed).
func (c *Client) DeletePAT(ctx context.Context, userID, tokenID string) error {
	if userID == "" || tokenID == "" {
		return fmt.Errorf("zitadel: delete PAT requires userID + tokenID")
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := c.issuer + "/management/v1/users/" + userID + "/pats/" + tokenID
	req, err := http.NewRequestWithContext(reqCtx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("zitadel: build delete-pat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel: delete PAT request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("zitadel: delete PAT returned %d: %s", resp.StatusCode, respBody)
	}
}

// DeleteUser hard-deletes a user (cascades PATs).
// DELETE /management/v1/users/{userId}.
//
// 404 is treated as success.
func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("zitadel: delete user requires userID")
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := c.issuer + "/management/v1/users/" + userID
	req, err := http.NewRequestWithContext(reqCtx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("zitadel: build delete-user request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("zitadel: delete user request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("zitadel: delete user returned %d: %s", resp.StatusCode, respBody)
	}
}
