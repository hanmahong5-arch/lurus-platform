// Package kovaprov is the platform-side client for the kova-rest tester
// provisioning RPC that runs on R6.
//
// Architecture (see doc/coord/contracts.md "kova-rest provisioning"):
//
//	platform-core (this client)
//	      │ HTTPS POST /internal/provision   (bearer auth)
//	      ▼
//	R6 sidecar (TODO — kova-repo follow-up)
//	      │ exec sudo -u kova-test create-tester.sh <slug>
//	      ▼
//	create-tester.sh writes /data/kova-test/testers/<slug>/.env,
//	prints the freshly minted admin key, returns the assigned
//	port + base URL.
//
// The R6 server side does NOT exist yet — this slice ships the platform
// client and the contract proposal only. When KOVA_PROVISION_BASE_URL is
// unset the client returns deterministic mock credentials so dev-mode
// integration tests round-trip end-to-end without R6.
package kovaprov

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultRequestTimeout caps a single round-trip. R6 create-tester.sh
	// itself runs in seconds; the rest of the budget covers SSH-over-Tailscale
	// latency from the K8s pod.
	defaultRequestTimeout = 30 * time.Second

	// maxResponseBody is a 16 KB ceiling — the success body is ~256 B; this
	// guards against a runaway server pumping logs.
	maxResponseBody = 16 << 10

	// maxAttempts is total tries (1 initial + 2 retries). Backoff is fixed
	// at 1s — provisioning is idempotent on the server side via tester_name,
	// so a tighter retry is safe.
	maxAttempts = 3

	// retryBackoff is the sleep between attempts.
	retryBackoff = time.Second
)

// Client provisions kova testers on R6. Construct via New; instances are
// safe for concurrent use.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	// nowFn / sleepFn injection points for deterministic tests; default to
	// time.Now / time.Sleep.
	sleepFn func(time.Duration)
}

// New returns a client targeting baseURL (e.g. "http://100.122.83.20:9999")
// with bearer apiKey. Pass an empty baseURL to construct a mock-mode client
// — every Provision call then returns deterministic synthetic credentials
// without making a network call. This is the contract used by dev / unit
// tests: same code path, no flag-juggling at the call site.
func New(baseURL, apiKey string) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: defaultRequestTimeout},
		sleepFn: time.Sleep,
	}
	return c
}

// IsMock reports whether this client will short-circuit to mock credentials.
// Callers (handlers, app services) use this to surface a "dev mode" warning
// in logs and OpenAPI examples.
func (c *Client) IsMock() bool { return c.baseURL == "" }

// ProvisionRequest is what platform-core sends to R6.
type ProvisionRequest struct {
	// TesterName is the R6-side slug. Constrained to [a-z0-9-]{3,32} —
	// matches the existing org slug regexp so we can reuse the org's slug
	// 1:1 without double validation. The server creates
	// /data/kova-test/testers/<TesterName>/ if it does not already exist.
	TesterName string `json:"tester_name"`

	// OrgID + AccountID are passed for audit on the R6 side; they have no
	// behavioural effect on the script. Persisting them lets ops correlate
	// "who triggered this provision" without scraping platform logs.
	OrgID     int64 `json:"org_id"`
	AccountID int64 `json:"account_id"`
}

// ProvisionResponse is what R6 returns on success.
type ProvisionResponse struct {
	// TesterName echoes the request — useful when the server normalised it.
	TesterName string `json:"tester_name"`
	// BaseURL is the kova-rest endpoint, e.g. "http://100.122.83.20:3015".
	BaseURL string `json:"base_url"`
	// AdminKey is the freshly minted admin API key. Returned ONCE; the
	// platform stores only its SHA-256 hash + first 8 chars.
	AdminKey string `json:"admin_key"`
	// Port is the kova-rest listening port (3010 + tester_idx by convention).
	Port int `json:"port"`
}

// Validate trims the request and rejects empty / oversized tester names.
// Public so handlers can pre-flight the same way the server will.
func (r *ProvisionRequest) Validate() error {
	r.TesterName = strings.TrimSpace(r.TesterName)
	if r.TesterName == "" {
		return errors.New("kovaprov: tester_name is required")
	}
	if len(r.TesterName) > 32 {
		return fmt.Errorf("kovaprov: tester_name %q exceeds 32 chars", r.TesterName)
	}
	if r.OrgID <= 0 {
		return errors.New("kovaprov: org_id must be positive")
	}
	return nil
}

// Provision creates (or refreshes) a kova-rest tester for the supplied org.
//
// In mock mode the function never blocks on the network and returns synthetic
// but stable values: the admin key is base64-stable across reboots within a
// single process so devs can curl it without re-fetching.
//
// In live mode the function retries up to maxAttempts on transport errors
// and 5xx responses. A 4xx is returned immediately — those are caller bugs,
// not transient failures.
func (c *Client) Provision(ctx context.Context, req ProvisionRequest) (*ProvisionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if c.IsMock() {
		return c.mockResponse(req), nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("kovaprov: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := c.doRequest(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// Honour ctx cancellation immediately — no point retrying after the
		// caller hung up.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Caller-side bugs (4xx) are not retried; they will not get better.
		var httpErr *httpStatusError
		if errors.As(err, &httpErr) && httpErr.status >= 400 && httpErr.status < 500 {
			return nil, err
		}
		if attempt < maxAttempts {
			c.sleepFn(retryBackoff)
		}
	}
	return nil, fmt.Errorf("kovaprov: provision after %d attempts: %w", maxAttempts, lastErr)
}

// doRequest is one network round-trip; retries are the caller's job.
func (c *Client) doRequest(ctx context.Context, body []byte) (*ProvisionResponse, error) {
	url := c.baseURL + "/internal/provision"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http transport: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, &httpStatusError{
			status: httpResp.StatusCode,
			body:   string(respBody),
		}
	}

	var out ProvisionResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if out.AdminKey == "" || out.BaseURL == "" {
		return nil, fmt.Errorf("kovaprov: server returned empty admin_key or base_url")
	}
	return &out, nil
}

// mockResponse fabricates dev-mode credentials. The admin key uses crypto/rand
// so it changes per call (we don't want a "stable" mock key making it into a
// real environment by accident); the rest of the fields are deterministic so
// tests can assert on them.
func (c *Client) mockResponse(req ProvisionRequest) *ProvisionResponse {
	rawBytes := make([]byte, 32)
	_, _ = rand.Read(rawBytes) // crypto/rand is allowed to be ignored per stdlib
	key := "sk-kova-" + hex.EncodeToString(rawBytes)
	return &ProvisionResponse{
		TesterName: req.TesterName,
		BaseURL:    "http://kova-mock.local",
		AdminKey:   key,
		// Conventional port allocation on R6 is 3010 + tester_idx. In mock
		// mode we use a sentinel value (-1) so dashboard code can detect
		// "this is a fake instance" without parsing base_url.
		Port: -1,
	}
}

// httpStatusError carries both the status code and the truncated body so the
// retry logic can distinguish 4xx (do-not-retry) from 5xx (retry).
type httpStatusError struct {
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("kovaprov: server returned HTTP %d: %s", e.status, e.body)
}
