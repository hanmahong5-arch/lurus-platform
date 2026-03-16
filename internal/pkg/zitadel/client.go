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
