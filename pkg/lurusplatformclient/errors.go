// Package lurusplatformclient — error types.
//
// Every non-2xx response from lurus-platform is decoded into a *PlatformError.
// Callers branch on the typed sentinel checks (IsNotFound, IsInsufficient,
// ...) rather than substring-matching the message.
package lurusplatformclient

import "fmt"

// Canonical error codes emitted by lurus-platform's unified envelope.
// Mirrors the constants in internal/adapter/handler/respond.go but
// duplicated here so this public package never imports internal/*.
const (
	codeInvalidRequest      = "invalid_request"
	codeInvalidParameter    = "invalid_parameter"
	codeUnauthorized        = "unauthorized"
	codeForbidden           = "forbidden"
	codeNotFound            = "not_found"
	codeConflict            = "conflict"
	codeInsufficientBalance = "insufficient_balance"
	codeRateLimited         = "rate_limited"
	codeInternal            = "internal_error"
	codeUpstreamFailed      = "upstream_failed"
)

// rawBodyTruncateBytes caps how much of an error response body we retain
// for diagnostics. 4 KiB is enough to capture a structured envelope plus
// a few stack-frame hints without bloating logs.
const rawBodyTruncateBytes = 4096

// PlatformError is returned for any non-2xx response from lurus-platform.
//
// The Code is the machine-readable error code from the unified envelope
// `{error: "<code>", message: "<text>"}`; Message is the human-readable
// text. Status is the HTTP status code.
//
// RawBody captures the response body verbatim for diagnostics, truncated
// to rawBodyTruncateBytes. Use it for log lines and bug reports — not
// for branching logic (use the IsXxx sentinel checks instead).
type PlatformError struct {
	Status  int    // 400, 401, 403, 404, 409, 429, 500, ...
	Code    string // "invalid_request", "unauthorized", "not_found", ...
	Message string // "Account not found"
	RawBody string // up to 4 KiB of the raw response body
}

// Error implements the error interface.
func (e *PlatformError) Error() string {
	if e == nil {
		return "lurus-platform: <nil error>"
	}
	return fmt.Sprintf("lurus-platform: %s (code=%s, status=%d)",
		e.Message, e.Code, e.Status)
}

// IsNotFound reports whether the error was a 404 / not_found envelope.
func (e *PlatformError) IsNotFound() bool {
	return e != nil && e.Code == codeNotFound
}

// IsUnauthorized reports whether the error was a 401 / unauthorized
// envelope. Typically caused by an invalid or expired bearer / cookie.
func (e *PlatformError) IsUnauthorized() bool {
	return e != nil && e.Code == codeUnauthorized
}

// IsForbidden reports whether the error was a 403 / forbidden envelope.
// Typically caused by a service key missing the required scope.
func (e *PlatformError) IsForbidden() bool {
	return e != nil && e.Code == codeForbidden
}

// IsInsufficient reports whether a wallet debit failed with an
// insufficient_balance envelope. Callers should prompt the user to
// top up rather than retry.
func (e *PlatformError) IsInsufficient() bool {
	return e != nil && e.Code == codeInsufficientBalance
}

// IsRateLimited reports whether the request was throttled. Callers
// should back off (exponential) or honour a Retry-After hint.
func (e *PlatformError) IsRateLimited() bool {
	return e != nil && e.Code == codeRateLimited
}

// IsUpstreamFailed reports whether a third-party dependency (NewAPI,
// payment gateway, ...) failed mid-request after platform mutated state.
// Typically a paired rollback already happened on the platform side; the
// caller can retry the original operation safely.
func (e *PlatformError) IsUpstreamFailed() bool {
	return e != nil && e.Code == codeUpstreamFailed
}

// IsRetriable reports whether the error class is worth retrying with
// exponential backoff. True for: 5xx, 429 rate-limited, upstream_failed.
// False for: 4xx client errors that won't change on retry (400/401/403/404/409).
//
// The client itself NEVER retries — caller-controlled. This helper is
// the canonical predicate to feed into a retry loop.
func (e *PlatformError) IsRetriable() bool {
	if e == nil {
		return false
	}
	if e.Status >= 500 {
		return true
	}
	if e.Status == 429 {
		return true
	}
	return e.Code == codeUpstreamFailed
}
