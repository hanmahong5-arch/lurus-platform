package lurusapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_DisabledWhenEmpty(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		apiKey  string
	}{
		{"both empty", "", ""},
		{"empty baseURL", "", "key"},
		{"empty apiKey", "http://localhost", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(tc.baseURL, tc.apiKey)
			if c != nil {
				t.Error("expected nil client when baseURL or apiKey is empty")
			}
		})
	}
}

func TestNewClient_Enabled(t *testing.T) {
	c := NewClient("http://localhost:8850", "test-key")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "http://localhost:8850" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestExchangeLucToLut_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want 'Bearer test-key'", auth)
		}
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/internal/currency/exchange") {
			t.Errorf("Path = %q", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"exchange_id":   42,
				"luc_amount":    10.0,
				"lut_amount":    100,
				"exchange_rate": 10.0,
				"vip_bonus":     0.1,
				"user_balance":  500,
				"balance_luc":   50.0,
				"balance_cn":    "50.00 CNY",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	resp, err := c.ExchangeLucToLut(context.Background(), &ExchangeRequest{
		UserID:    1,
		LucAmount: 10.0,
		VIPLevel:  1,
	})
	if err != nil {
		t.Fatalf("ExchangeLucToLut: %v", err)
	}
	if resp.ExchangeID != 42 {
		t.Errorf("ExchangeID = %d, want 42", resp.ExchangeID)
	}
	if resp.LucAmount != 10.0 {
		t.Errorf("LucAmount = %f, want 10", resp.LucAmount)
	}
	if resp.LutAmount != 100 {
		t.Errorf("LutAmount = %d, want 100", resp.LutAmount)
	}
}

func TestExchangeLucToLut_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, err := c.ExchangeLucToLut(context.Background(), &ExchangeRequest{LucAmount: 10})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "http 500") {
		t.Errorf("error = %q, want containing 'http 500'", err.Error())
	}
}

func TestExchangeLucToLut_FailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "insufficient balance",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, err := c.ExchangeLucToLut(context.Background(), &ExchangeRequest{LucAmount: 10})
	if err == nil {
		t.Fatal("expected error for failed exchange")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Errorf("error = %q, want containing 'insufficient balance'", err.Error())
	}
}

func TestExchangeLucToLut_NoData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    nil,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, err := c.ExchangeLucToLut(context.Background(), &ExchangeRequest{LucAmount: 10})
	if err == nil {
		t.Fatal("expected error for nil data")
	}
	if !strings.Contains(err.Error(), "no data") {
		t.Errorf("error = %q, want containing 'no data'", err.Error())
	}
}

func TestGetCurrencyInfo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"exchange_rates": map[string]interface{}{
					"lug_to_luc": 1.0,
					"luc_to_lut": 10.0,
					"lug_to_lut": 10.0,
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	info, err := c.GetCurrencyInfo(context.Background())
	if err != nil {
		t.Fatalf("GetCurrencyInfo: %v", err)
	}
	if info.ExchangeRates.LucToLut != 10.0 {
		t.Errorf("LucToLut = %f, want 10", info.ExchangeRates.LucToLut)
	}
}

func TestGetCurrencyInfo_Failed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, err := c.GetCurrencyInfo(context.Background())
	if err == nil {
		t.Fatal("expected error for failed response")
	}
}

func TestDoRequest_ConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "test-key")
	_, err := c.ExchangeLucToLut(context.Background(), &ExchangeRequest{LucAmount: 1})
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestDoRequest_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	_, err := c.ExchangeLucToLut(context.Background(), &ExchangeRequest{LucAmount: 1})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %q, want containing 'unmarshal'", err.Error())
	}
}

func TestDoRequest_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Slow response — will be cancelled.
		select {}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.ExchangeLucToLut(ctx, &ExchangeRequest{LucAmount: 1})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
