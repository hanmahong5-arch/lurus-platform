// Package lurusapi provides an HTTP client for calling lurus-api internal endpoints.
package lurusapi

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
	requestTimeout  = 10 * time.Second
	maxResponseBody = 32768 // 32 KB
)

// Client calls lurus-api /internal/* endpoints with bearer key auth.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates a new lurus-api internal client.
// Returns nil if baseURL or apiKey is empty (disabled).
func NewClient(baseURL, apiKey string) *Client {
	if baseURL == "" || apiKey == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// ExchangeRequest is the payload for POST /internal/currency/exchange.
type ExchangeRequest struct {
	UserID          int     `json:"user_id"`
	LucAmount       float64 `json:"luc_amount"`
	VIPLevel        int     `json:"vip_level"`
	ReferenceID     string  `json:"reference_id"`
	PlatformOrderNo string  `json:"platform_order_no"`
	Note            string  `json:"note"`
}

// ExchangeResponse is the response from a successful exchange.
type ExchangeResponse struct {
	ExchangeID   int     `json:"exchange_id"`
	LucAmount    float64 `json:"luc_amount"`
	LutAmount    int     `json:"lut_amount"`
	ExchangeRate float64 `json:"exchange_rate"`
	VIPBonus     float64 `json:"vip_bonus"`
	UserBalance  int     `json:"user_balance"`
	BalanceLuc   float64 `json:"balance_luc"`
	BalanceCN    string  `json:"balance_cn"`
}

// CurrencyInfo holds the currency system configuration from lurus-api.
type CurrencyInfo struct {
	ExchangeRates struct {
		LugToLuc float64 `json:"lug_to_luc"`
		LucToLut float64 `json:"luc_to_lut"`
		LugToLut float64 `json:"lug_to_lut"`
	} `json:"exchange_rates"`
}

// ExchangeLucToLut calls lurus-api to convert platform credits (LUC) to API credits (LUT).
func (c *Client) ExchangeLucToLut(ctx context.Context, req *ExchangeRequest) (*ExchangeResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("lurusapi: marshal exchange request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/internal/currency/exchange", body)
	if err != nil {
		return nil, fmt.Errorf("lurusapi: exchange request: %w", err)
	}

	var result struct {
		Success    bool              `json:"success"`
		Idempotent bool             `json:"idempotent"`
		Message    string            `json:"message"`
		Data       *ExchangeResponse `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("lurusapi: unmarshal exchange response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("lurusapi: exchange failed: %s", result.Message)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("lurusapi: exchange returned no data")
	}
	return result.Data, nil
}

// GetCurrencyInfo retrieves the currency system configuration.
func (c *Client) GetCurrencyInfo(ctx context.Context) (*CurrencyInfo, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/internal/currency/info", nil)
	if err != nil {
		return nil, fmt.Errorf("lurusapi: get currency info: %w", err)
	}

	var result struct {
		Success bool          `json:"success"`
		Data    *CurrencyInfo `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("lurusapi: unmarshal currency info: %w", err)
	}
	if !result.Success || result.Data == nil {
		return nil, fmt.Errorf("lurusapi: currency info request failed")
	}
	return result.Data, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
