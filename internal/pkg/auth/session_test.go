package auth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// buildCustomSessionToken constructs a session token with arbitrary claims for edge-case testing.
// Uses the same HMAC signing as IssueSessionToken so the signature is always valid.
func buildCustomSessionToken(t *testing.T, iss, sub string, exp int64, secret string) string {
	t.Helper()
	header := `{"typ":"JWT","alg":"HS256"}`
	payload, _ := json.Marshal(map[string]any{
		"iss": iss,
		"sub": sub,
		"iat": time.Now().Unix(),
		"exp": exp,
	})
	body := sessionB64([]byte(header)) + "." + sessionB64(payload)
	sig := sessionHMAC([]byte(body), []byte(secret))
	return body + "." + sessionB64(sig)
}

func TestSessionToken_IssueAndValidate(t *testing.T) {
	secret := "test-secret-32-bytes-long-please!!"
	token, err := IssueSessionToken(42, time.Hour, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken error: %v", err)
	}
	id, err := ValidateSessionToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateSessionToken error: %v", err)
	}
	if id != 42 {
		t.Errorf("accountID = %d, want 42", id)
	}
}

func TestSessionToken_Expired(t *testing.T) {
	secret := "test-secret-32-bytes-long-please!!"
	token, err := IssueSessionToken(1, -time.Second, secret)
	if err != nil {
		t.Fatalf("IssueSessionToken error: %v", err)
	}
	_, err = ValidateSessionToken(token, secret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error should mention 'expired', got: %v", err)
	}
}

func TestSessionToken_WrongSecret(t *testing.T) {
	token, err := IssueSessionToken(5, time.Hour, "secret-a-padding-xxxxxxxxxx!!")
	if err != nil {
		t.Fatalf("IssueSessionToken error: %v", err)
	}
	_, err = ValidateSessionToken(token, "secret-b-padding-xxxxxxxxxx!!")
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestSessionToken_Malformed(t *testing.T) {
	_, err := ValidateSessionToken("bad.token.xyz", "any-secret-long-enough!!xxxx")
	if err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
}

func TestSessionToken_WrongIssuer(t *testing.T) {
	secret := "test-secret-32-bytes-long-please!!"
	token := buildCustomSessionToken(t, "wrong-issuer", "lurus:1",
		time.Now().Add(time.Hour).Unix(), secret)
	_, err := ValidateSessionToken(token, secret)
	if err == nil {
		t.Fatal("expected error for wrong issuer, got nil")
	}
	if !strings.Contains(err.Error(), "issuer") {
		t.Errorf("error should mention 'issuer', got: %v", err)
	}
}

func TestSessionToken_WrongSubPrefix(t *testing.T) {
	secret := "test-secret-32-bytes-long-please!!"
	token := buildCustomSessionToken(t, SessionIssuer, "notlurus:1",
		time.Now().Add(time.Hour).Unix(), secret)
	_, err := ValidateSessionToken(token, secret)
	if err == nil {
		t.Fatal("expected error for wrong sub prefix, got nil")
	}
}

func TestSessionToken_EmptySecret(t *testing.T) {
	_, err := IssueSessionToken(1, time.Hour, "")
	if err == nil {
		t.Fatal("expected error for empty secret, got nil")
	}
}

func TestSessionToken_InvalidBase64Signature(t *testing.T) {
	// Construct a token whose 3rd part contains characters invalid in base64url (e.g. '+')
	// so that base64.RawURLEncoding.DecodeString returns an error.
	secret := "test-secret-32-bytes-long-please!!"
	token := buildCustomSessionToken(t, SessionIssuer, "lurus:1",
		time.Now().Add(time.Hour).Unix(), secret)
	parts := strings.SplitN(token, ".", 3)
	// Replace valid signature with a string containing '+' which is invalid in base64url
	invalid := parts[0] + "." + parts[1] + ".invalid+sig!!!"
	_, err := ValidateSessionToken(invalid, secret)
	if err == nil {
		t.Fatal("expected error for invalid base64 signature, got nil")
	}
}
