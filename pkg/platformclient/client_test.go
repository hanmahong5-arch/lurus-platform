package platformclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	client := New(srv.URL, "test-key")
	return srv, client
}

// ── Account Operations ──────────────────────────────────────────────────────

func TestGetAccountByZitadelSub_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(Account{ID: 1, LurusID: "LU0000001", DisplayName: "Alice"})
	})
	defer srv.Close()

	acct, err := client.GetAccountByZitadelSub(context.Background(), "sub-123")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if acct.ID != 1 {
		t.Errorf("ID = %d", acct.ID)
	}
	if acct.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q", acct.DisplayName)
	}
}

func TestGetAccountByID_NotFound(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})
	defer srv.Close()

	_, err := client.GetAccountByID(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetAccountByEmail_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(Account{ID: 2, Email: "bob@lurus.cn"})
	})
	defer srv.Close()

	acct, err := client.GetAccountByEmail(context.Background(), "bob@lurus.cn")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if acct.Email != "bob@lurus.cn" {
		t.Errorf("Email = %q", acct.Email)
	}
}

// ── Wallet Operations ───────────────────────────────────────────────────────

func TestDebitWallet_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		var req DebitRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Amount != 10.0 {
			t.Errorf("Amount = %f", req.Amount)
		}
		json.NewEncoder(w).Encode(DebitResult{TransactionID: 42, BalanceAfter: 90.0})
	})
	defer srv.Close()

	result, err := client.DebitWallet(context.Background(), 1, 10.0, "usage", "test", "product")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.TransactionID != 42 {
		t.Errorf("TransactionID = %d", result.TransactionID)
	}
}

func TestDebitWallet_InsufficientBalance(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(402)
		json.NewEncoder(w).Encode(map[string]string{"error": "insufficient_balance"})
	})
	defer srv.Close()

	_, err := client.DebitWallet(context.Background(), 1, 10.0, "usage", "test", "product")
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Errorf("err = %v, want ErrInsufficientBalance", err)
	}
}

func TestGetWalletBalance_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(WalletBalance{Balance: 100.5, Frozen: 20.0})
	})
	defer srv.Close()

	bal, err := client.GetWalletBalance(context.Background(), 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if bal.Balance != 100.5 {
		t.Errorf("Balance = %f", bal.Balance)
	}
}

// ── Pre-Authorization ───────────────────────────────────────────────────────

func TestPreAuthorize_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(PreAuthResult{PreAuthID: 7, Amount: 50.0})
	})
	defer srv.Close()

	result, err := client.PreAuthorize(context.Background(), 1, PreAuthRequest{
		Amount: 50.0, ProductID: "llm-api", ReferenceID: "call-1",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.PreAuthID != 7 {
		t.Errorf("PreAuthID = %d", result.PreAuthID)
	}
}

func TestSettlePreAuth_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})
	defer srv.Close()

	err := client.SettlePreAuth(context.Background(), 7, 30.0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestReleasePreAuth_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})
	defer srv.Close()

	err := client.ReleasePreAuth(context.Background(), 7)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// ── Checkout ────────────────────────────────────────────────────────────────

func TestCreateCheckoutSession_Success(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(CheckoutResult{
			OrderNo: "LO20260321abc", PayURL: "https://pay.example.com/123", Status: "pending",
		})
	})
	defer srv.Close()

	result, err := client.CreateCheckoutSession(context.Background(), CheckoutRequest{
		AccountID: 1, AmountCNY: 50.0, PaymentMethod: "stripe", SourceService: "forge",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.PayURL == "" {
		t.Error("expected non-empty PayURL")
	}
}

// ── Error Handling ──────────────────────────────────────────────────────────

func TestUnauthorized(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	})
	defer srv.Close()

	_, err := client.GetAccountByID(context.Background(), 1)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestRateLimited(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	})
	defer srv.Close()

	_, err := client.GetEntitlements(context.Background(), 1, "prod")
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("err = %v, want ErrRateLimited", err)
	}
}

func TestServerError(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"message": "database timeout"})
	})
	defer srv.Close()

	_, err := client.GetAccountByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for 500")
	}
	// Error message should include the server's message.
	if err.Error() == "" {
		t.Error("expected non-empty error")
	}
}

func TestConnectionRefused(t *testing.T) {
	client := New("http://127.0.0.1:1", "key")
	_, err := client.GetAccountByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}
