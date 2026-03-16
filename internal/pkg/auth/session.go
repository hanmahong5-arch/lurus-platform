package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SessionIssuer is the iss claim used in lurus-platform-issued session tokens.
// Middleware distinguishes these from Zitadel JWTs by checking this value.
const SessionIssuer = "lurus-platform"

// IssueSessionToken creates a signed HS256 JWT for a lurus account.
// The sub claim is formatted as "lurus:<accountID>".
func IssueSessionToken(accountID int64, ttl time.Duration, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("session: empty secret")
	}
	headerJSON := `{"typ":"JWT","alg":"HS256"}`
	payload, err := json.Marshal(map[string]any{
		"iss": SessionIssuer,
		"sub": fmt.Sprintf("lurus:%d", accountID),
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(ttl).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("session: marshal payload: %w", err)
	}
	body := sessionB64([]byte(headerJSON)) + "." + sessionB64(payload)
	sig := sessionHMAC([]byte(body), []byte(secret))
	return body + "." + sessionB64(sig), nil
}

// ValidateSessionToken parses and verifies a lurus-issued HS256 session token.
// Returns the lurus account ID embedded in the sub claim.
func ValidateSessionToken(tokenStr, secret string) (int64, error) {
	if secret == "" {
		return 0, fmt.Errorf("session: secret not configured")
	}
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("session: malformed token: expected 3 parts")
	}

	// Verify HMAC-SHA256 signature before trusting any claims.
	body := parts[0] + "." + parts[1]
	expectedSig := sessionHMAC([]byte(body), []byte(secret))
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, fmt.Errorf("session: decode signature: %w", err)
	}
	if !hmac.Equal(expectedSig, gotSig) {
		return 0, fmt.Errorf("session: invalid signature")
	}

	// Decode and validate payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, fmt.Errorf("session: decode payload: %w", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return 0, fmt.Errorf("session: parse payload: %w", err)
	}
	if claims.Iss != SessionIssuer {
		return 0, fmt.Errorf("session: unexpected issuer %q", claims.Iss)
	}
	if time.Now().Unix() > claims.Exp {
		return 0, fmt.Errorf("session: token expired")
	}

	// Parse sub: "lurus:<accountID>".
	const subPrefix = "lurus:"
	if !strings.HasPrefix(claims.Sub, subPrefix) {
		return 0, fmt.Errorf("session: invalid sub format: %q", claims.Sub)
	}
	id, err := strconv.ParseInt(claims.Sub[len(subPrefix):], 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("session: invalid account id in sub")
	}
	return id, nil
}

func sessionHMAC(data, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sessionB64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
