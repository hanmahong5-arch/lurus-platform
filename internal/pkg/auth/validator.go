package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"
)

// Claims holds the validated JWT claims from a Zitadel-issued access token.
type Claims struct {
	Sub   string   // Subject — Zitadel user ID
	Iss   string   // Issuer
	Aud   []string // Audience
	Email string
	Name  string
	Roles []string // Flattened role names from all role claim formats
}

// jwtHeader is the decoded JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// rawClaims is the full set of parsed JWT payload fields.
type rawClaims struct {
	Sub   string          `json:"sub"`
	Iss   string          `json:"iss"`
	Aud   json.RawMessage `json:"aud"` // string or []string
	Exp   int64           `json:"exp"`
	Iat   int64           `json:"iat"`
	Email string          `json:"email"`
	Name  string          `json:"name"`

	// Zitadel project roles — map[roleName]map[orgID]displayName
	ZitadelRoles map[string]map[string]string `json:"urn:zitadel:iam:org:project:roles"`
	// Simple string slice roles (non-Zitadel environments)
	PlainRoles []string `json:"roles"`
}

// ValidatorConfig holds all settings for JWT validation.
type ValidatorConfig struct {
	Issuer     string   // Expected iss claim (e.g. "https://auth.lurus.cn")
	Audience   string   // Expected aud value
	JWKSURL    string   // Zitadel JWKS endpoint
	JWKSTTL    time.Duration
	AdminRoles []string // Role names treated as admin (default: ["admin"])
}

// Validator validates Zitadel JWTs.
type Validator struct {
	cfg   ValidatorConfig
	jwks  *JWKSCache
}

// NewValidator creates a JWT validator. It does not pre-fetch keys — the first
// call to Validate will trigger the initial JWKS fetch.
func NewValidator(cfg ValidatorConfig) *Validator {
	if len(cfg.AdminRoles) == 0 {
		cfg.AdminRoles = []string{"admin"}
	}
	return &Validator{
		cfg:  cfg,
		jwks: NewJWKSCache(cfg.JWKSURL, cfg.JWKSTTL),
	}
}

// Validate parses, verifies, and returns the claims from a raw JWT string.
// Returns an error for any invalid token (expired, bad signature, wrong iss/aud).
func (v *Validator) Validate(ctx context.Context, tokenStr string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("auth: malformed token: expected 3 parts, got %d", len(parts))
	}

	// 1. Decode header.
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("auth: decode header: %w", err)
	}
	var hdr jwtHeader
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		return nil, fmt.Errorf("auth: parse header: %w", err)
	}

	// 2. Decode payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("auth: decode payload: %w", err)
	}
	var rc rawClaims
	if err := json.Unmarshal(payloadJSON, &rc); err != nil {
		return nil, fmt.Errorf("auth: parse claims: %w", err)
	}

	// 3. Validate standard claims before touching the signature (cheap checks first).
	if err := v.validateStandardClaims(&rc); err != nil {
		return nil, err
	}

	// 4. Fetch signing key and verify signature.
	pubKey, err := v.jwks.GetKey(ctx, hdr.Kid)
	if err != nil {
		return nil, fmt.Errorf("auth: resolve signing key: %w", err)
	}

	signingInput := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("auth: decode signature: %w", err)
	}

	if err := verifySignature(hdr.Alg, pubKey, []byte(signingInput), sigBytes); err != nil {
		return nil, fmt.Errorf("auth: signature verification failed: %w", err)
	}

	return buildClaims(&rc), nil
}

// HasAdminRole reports whether any of the token's roles is an admin role.
func (v *Validator) HasAdminRole(claims *Claims) bool {
	adminSet := make(map[string]struct{}, len(v.cfg.AdminRoles))
	for _, r := range v.cfg.AdminRoles {
		adminSet[r] = struct{}{}
	}
	for _, r := range claims.Roles {
		if _, ok := adminSet[r]; ok {
			return true
		}
	}
	return false
}

// validateStandardClaims checks exp, iss, aud.
func (v *Validator) validateStandardClaims(rc *rawClaims) error {
	now := time.Now().Unix()
	if rc.Exp == 0 {
		return fmt.Errorf("auth: token missing exp claim")
	}
	if rc.Exp < now {
		return fmt.Errorf("auth: token expired at %d (now %d)", rc.Exp, now)
	}
	if rc.Iss == "" {
		return fmt.Errorf("auth: token missing iss claim")
	}
	if rc.Iss != v.cfg.Issuer {
		return fmt.Errorf("auth: unexpected issuer %q, want %q", rc.Iss, v.cfg.Issuer)
	}
	if v.cfg.Audience != "" {
		aud, err := parseAudience(rc.Aud)
		if err != nil {
			return fmt.Errorf("auth: parse aud: %w", err)
		}
		if !containsString(aud, v.cfg.Audience) {
			return fmt.Errorf("auth: audience mismatch, token aud=%v, want %q", aud, v.cfg.Audience)
		}
	}
	return nil
}

// buildClaims converts rawClaims into the exported Claims struct.
func buildClaims(rc *rawClaims) *Claims {
	c := &Claims{
		Sub:   rc.Sub,
		Iss:   rc.Iss,
		Email: rc.Email,
		Name:  rc.Name,
	}

	// Audience
	if aud, err := parseAudience(rc.Aud); err == nil {
		c.Aud = aud
	}

	// Collect roles — support both Zitadel project roles and plain string slice.
	seen := make(map[string]struct{})
	addRole := func(r string) {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			c.Roles = append(c.Roles, r)
		}
	}
	for roleName := range rc.ZitadelRoles {
		addRole(roleName)
	}
	for _, r := range rc.PlainRoles {
		addRole(r)
	}

	return c
}

// verifySignature verifies the JWT signature for supported algorithms.
func verifySignature(alg string, pub interface{}, signingInput, sig []byte) error {
	switch alg {
	case "RS256":
		return verifyRSA(pub, signingInput, sig, sha256.New, "sha256")
	case "RS384":
		return verifyRSA(pub, signingInput, sig, sha512.New384, "sha384")
	case "RS512":
		return verifyRSA(pub, signingInput, sig, sha512.New, "sha512")
	case "ES256":
		return verifyEC(pub, signingInput, sig, sha256.New)
	case "ES384":
		return verifyEC(pub, signingInput, sig, sha512.New384)
	case "ES512":
		return verifyEC(pub, signingInput, sig, sha512.New)
	default:
		return fmt.Errorf("auth: unsupported algorithm %q", alg)
	}
}

func verifyRSA(pub interface{}, signingInput, sig []byte, newHash func() hash.Hash, hashName string) error {
	rsaKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("auth: key type mismatch for RSA algorithm")
	}
	h := newHash()
	h.Write(signingInput)
	digest := h.Sum(nil)

	// Use crypto/rsa.VerifyPKCS1v15 via reflect — we need the crypto hash constant.
	// Encode hash name to crypto.Hash.
	cryptoHash, err := hashNameToCryptoHash(hashName)
	if err != nil {
		return err
	}

	if err := rsa.VerifyPKCS1v15(rsaKey, cryptoHash, digest, sig); err != nil {
		return fmt.Errorf("auth: RSA verification: %w", err)
	}
	return nil
}

func verifyEC(pub interface{}, signingInput, sig []byte, newHash func() hash.Hash) error {
	ecKey, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("auth: key type mismatch for EC algorithm")
	}
	h := newHash()
	h.Write(signingInput)
	digest := h.Sum(nil)

	// EC signature is DER-encoded or raw (r||s) depending on JWT spec.
	// JWT uses raw concatenation: first half = r, second half = s.
	if len(sig)%2 != 0 {
		return fmt.Errorf("auth: EC signature has odd length")
	}
	half := len(sig) / 2
	r := new(big.Int).SetBytes(sig[:half])
	s := new(big.Int).SetBytes(sig[half:])

	if !ecdsa.Verify(ecKey, digest, r, s) {
		return fmt.Errorf("auth: EC signature invalid")
	}
	return nil
}

// parseAudience parses a JSON value that may be a string or string array.
func parseAudience(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Try string array first.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	// Fall back to single string.
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("aud is neither string nor []string")
	}
	return []string{s}, nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
