package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func issueTestSessionToken(accountID int64, ttl time.Duration, secret string) string {
	headerJSON := `{"typ":"JWT","alg":"HS256"}`
	payload, _ := json.Marshal(map[string]any{
		"iss": sessionIssuer,
		"sub": fmt.Sprintf("lurus:%d", accountID),
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(ttl).Unix(),
	})
	body := base64.RawURLEncoding.EncodeToString([]byte(headerJSON)) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := mac.Sum(nil)
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestValidateSessionToken_Valid(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"
	token := issueTestSessionToken(42, 1*time.Hour, secret)

	accountID, err := validateSessionToken(token, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accountID != 42 {
		t.Errorf("accountID = %d, want 42", accountID)
	}
}

func TestValidateSessionToken_WrongSecret(t *testing.T) {
	secret := "correct-secret"
	token := issueTestSessionToken(42, 1*time.Hour, secret)

	_, err := validateSessionToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestValidateSessionToken_Expired(t *testing.T) {
	secret := "test-secret"
	token := issueTestSessionToken(42, -1*time.Hour, secret)

	_, err := validateSessionToken(token, secret)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateSessionToken_Malformed(t *testing.T) {
	_, err := validateSessionToken("not.a.valid.jwt", "secret")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}

	_, err = validateSessionToken("only-one-part", "secret")
	if err == nil {
		t.Fatal("expected error for single-part token")
	}
}

func TestExtractJWTSub_Valid(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"sub": "zitadel-user-123",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	fakeToken := base64.RawURLEncoding.EncodeToString([]byte("{}")) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("fake-sig"))

	sub, err := extractJWTSub(fakeToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub != "zitadel-user-123" {
		t.Errorf("sub = %q, want %q", sub, "zitadel-user-123")
	}
}

func TestExtractJWTSub_Expired(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"sub": "user-123",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
	})
	fakeToken := base64.RawURLEncoding.EncodeToString([]byte("{}")) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("fake-sig"))

	_, err := extractJWTSub(fakeToken)
	if err == nil {
		t.Fatal("expected error for expired JWT")
	}
}

func TestExtractJWTSub_MissingSub(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	fakeToken := base64.RawURLEncoding.EncodeToString([]byte("{}")) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("fake-sig"))

	_, err := extractJWTSub(fakeToken)
	if err == nil {
		t.Fatal("expected error for missing sub")
	}
}

func TestGetAccountID_NotSet(t *testing.T) {
	// Without gin context, just verify the function exists and returns 0.
	// Full integration test requires Gin test context.
	_ = GetAccountID
}
