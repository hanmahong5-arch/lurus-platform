// Package platform provides HTTP clients that call platform-core internal API.
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
)

// DigestFetcher implements app.DigestDataFetcher by calling platform-core internal API.
type DigestFetcher struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewDigestFetcher creates a DigestFetcher.
func NewDigestFetcher(baseURL, apiKey string) *DigestFetcher {
	return &DigestFetcher{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// accountOverview matches the platform internal API response for /internal/v1/accounts/:id/overview.
type accountOverview struct {
	AccountID   int64   `json:"account_id"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Balance     float64 `json:"balance"`
}

// activeAccountsResponse matches the platform internal list response.
type activeAccountsResponse struct {
	AccountIDs []int64 `json:"account_ids"`
}

// FetchActiveAccountIDs returns account IDs that were active in the past week.
// Calls platform-core GET /internal/v1/accounts/active?since=<7d ago>.
func (f *DigestFetcher) FetchActiveAccountIDs(ctx context.Context) ([]int64, error) {
	since := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	url := fmt.Sprintf("%s/internal/v1/accounts/active?since=%s", f.baseURL, since)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("digest fetcher: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("digest fetcher: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("digest fetcher: active accounts returned %d", resp.StatusCode)
	}

	var result activeAccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("digest fetcher: decode: %w", err)
	}
	return result.AccountIDs, nil
}

// FetchWeeklyData returns usage summary data for a single account.
// Calls platform-core GET /internal/v1/accounts/:id/overview to get basic info,
// then constructs the digest data.
func (f *DigestFetcher) FetchWeeklyData(ctx context.Context, accountID int64) (*app.WeeklyDigestData, error) {
	url := fmt.Sprintf("%s/internal/v1/accounts/%d/overview", f.baseURL, accountID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("digest fetcher: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("digest fetcher: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("digest fetcher: overview returned %d", resp.StatusCode)
	}

	var overview accountOverview
	if err := json.NewDecoder(resp.Body).Decode(&overview); err != nil {
		return nil, fmt.Errorf("digest fetcher: decode: %w", err)
	}

	return &app.WeeklyDigestData{
		AccountID:   overview.AccountID,
		Email:       overview.Email,
		DisplayName: overview.DisplayName,
	}, nil
}
