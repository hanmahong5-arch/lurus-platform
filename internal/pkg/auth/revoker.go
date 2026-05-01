package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionRevoker tracks revoked session tokens by their SHA-256 hash so
// that a server-side logout can invalidate a token before its natural
// expiry (P1-5 in docs/平台硬化清单.md). Without this, a stolen 30-day
// token stays valid for the full window even after the user "logged out"
// in the browser.
//
// Why hash-and-store instead of adding a `jti` claim:
//   - retroactive: every existing token is revocable on day one without a
//     reissue migration
//   - cheap: SHA-256 over the token string is fixed-size and irreversible
//   - Redis-only state: no DB schema change, TTL aligns with token expiry
//
// Failure mode: nil receiver, nil rdb, or Redis error → IsRevoked returns
// false (fail-open). Revocation is a defence-in-depth layer over the
// natural JWT expiry; locking every user out during a Redis blip would
// do strictly more harm than letting a tiny window of revoked-but-still-
// accepted tokens slip through.
type SessionRevoker struct {
	rdb *redis.Client
}

// NewSessionRevoker constructs a revoker. rdb=nil disables both Revoke
// and IsRevoked (no-op + always false) so callers can wire it
// unconditionally.
func NewSessionRevoker(rdb *redis.Client) *SessionRevoker {
	return &SessionRevoker{rdb: rdb}
}

// Revoke records the token hash with the supplied TTL. Idempotent —
// re-revoking the same token is harmless. ttl<=0 → no-op (the token is
// already past its natural expiry; nothing to defend against).
func (r *SessionRevoker) Revoke(ctx context.Context, token string, ttl time.Duration) error {
	if r == nil || r.rdb == nil {
		return nil
	}
	if ttl <= 0 {
		return nil
	}
	return r.rdb.Set(ctx, revokeKey(token), "1", ttl).Err()
}

// IsRevoked reports whether the token's hash is on the revoke list.
// Fails open on Redis error — see type-level comment.
func (r *SessionRevoker) IsRevoked(ctx context.Context, token string) bool {
	if r == nil || r.rdb == nil {
		return false
	}
	n, err := r.rdb.Exists(ctx, revokeKey(token)).Result()
	if err != nil {
		// One slog.Warn per failure is enough; spamming on every request
		// during an outage would drown out the actual diagnostic.
		slog.WarnContext(ctx, "session_revoker: redis error, fail-open", "err", err)
		return false
	}
	return n > 0
}

// revokeKey hashes the raw token so we never persist the secret material
// itself. The "auth:revoked:" prefix isolates this from other Redis users
// (rate limit, idempotency, ...) that share the same DB.
func revokeKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return "auth:revoked:" + hex.EncodeToString(h[:])
}
