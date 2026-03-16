// Package auth provides Zitadel JWT validation using JWKS.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/tracing"
)

const (
	defaultJWKSTTL     = time.Hour
	defaultHTTPTimeout = 10 * time.Second
	maxJWKSBodyBytes   = 1 << 16 // 64 KiB
)

// jwkKey represents a single key in a JWKS response.
type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA
	N string `json:"n"`
	E string `json:"e"`
	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

// JWKSCache fetches and caches public keys from a JWKS endpoint.
type JWKSCache struct {
	url       string
	ttl       time.Duration
	httpCl    *http.Client
	mu        sync.RWMutex
	keys      map[string]interface{} // kid → *rsa.PublicKey | *ecdsa.PublicKey
	fetchedAt time.Time
	sfGroup   singleflight.Group // deduplicate concurrent refresh calls
}

// NewJWKSCache creates a new JWKS cache for the given endpoint URL.
func NewJWKSCache(jwksURL string, ttl time.Duration) *JWKSCache {
	if ttl <= 0 {
		ttl = defaultJWKSTTL
	}
	return &JWKSCache{
		url:    jwksURL,
		ttl:    ttl,
		httpCl: &http.Client{Timeout: defaultHTTPTimeout},
		keys:   make(map[string]interface{}),
	}
}

// GetKey returns the public key for the given kid. Fetches from remote if the
// local cache is stale or the kid is unknown.
// Concurrent callers that all find the cache stale will be collapsed into a
// single HTTP request via singleflight, preventing thundering-herd on the
// JWKS endpoint.
func (c *JWKSCache) GetKey(ctx context.Context, kid string) (interface{}, error) {
	// Fast path: read lock, check TTL and kid presence.
	c.mu.RLock()
	if time.Since(c.fetchedAt) < c.ttl {
		if k, ok := c.keys[kid]; ok {
			c.mu.RUnlock()
			return k, nil
		}
	}
	c.mu.RUnlock()

	// Slow path: cache miss or stale. Use singleflight so that N concurrent
	// callers share exactly one HTTP JWKS request instead of N requests.
	_, err, _ := c.sfGroup.Do("refresh", func() (interface{}, error) {
		return nil, c.refresh(ctx)
	})
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	k, ok := c.keys[kid]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("auth: kid %q not found in JWKS", kid)
	}
	return k, nil
}

// Refresh forces a JWKS refresh from the remote endpoint.
func (c *JWKSCache) Refresh(ctx context.Context) error {
	return c.refresh(ctx)
}

func (c *JWKSCache) refresh(ctx context.Context) error {
	ctx, span := tracing.Tracer("lurus-platform").Start(ctx, "jwks.refresh",
		trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("auth: build jwks request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpCl.Do(req)
	if err != nil {
		return fmt.Errorf("auth: fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: jwks endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBodyBytes))
	if err != nil {
		return fmt.Errorf("auth: read jwks body: %w", err)
	}

	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("auth: parse jwks: %w", err)
	}

	newKeys := make(map[string]interface{}, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue // skip non-signature keys
		}
		pub, err := parseJWK(k)
		if err != nil {
			// Log and skip unrecognized key types rather than aborting.
			continue
		}
		newKeys[k.Kid] = pub
	}

	c.mu.Lock()
	c.keys = newKeys
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	return nil
}

// parseJWK converts a JWK into a Go crypto public key.
func parseJWK(k jwkKey) (interface{}, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAKey(k)
	case "EC":
		return parseECKey(k)
	default:
		return nil, fmt.Errorf("auth: unsupported kty %q", k.Kty)
	}
}

func parseRSAKey(k jwkKey) (*rsa.PublicKey, error) {
	if k.N == "" || k.E == "" {
		return nil, fmt.Errorf("auth: RSA key missing n or e")
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("auth: decode RSA n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("auth: decode RSA e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	if e <= 0 {
		return nil, fmt.Errorf("auth: invalid RSA exponent")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

func parseECKey(k jwkKey) (*ecdsa.PublicKey, error) {
	if k.X == "" || k.Y == "" {
		return nil, fmt.Errorf("auth: EC key missing x or y")
	}

	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("auth: unsupported EC curve %q", k.Crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("auth: decode EC x: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("auth: decode EC y: %w", err)
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}
