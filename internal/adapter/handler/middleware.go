package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// DefaultMaxRequestBodyBytes is the global request body size limit (2 MB).
	// Individual endpoints may use io.LimitReader for smaller limits (e.g. webhooks: 1 MB).
	DefaultMaxRequestBodyBytes = 2 << 20

	// DefaultRequestTimeout is the per-request context deadline.
	// Protects against slow clients and stuck DB queries leaking goroutines.
	DefaultRequestTimeout = 30 * time.Second
)

// MaxBodySize returns a Gin middleware that limits request body size.
// Uses http.MaxBytesReader which returns 413 if the limit is exceeded.
//
// Borrowed from OpenClaw's readJsonBodyOrError pattern — explicit size limits
// prevent large-payload DoS attacks.
func MaxBodySize(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

// RequestTimeout sets a context deadline on each request.
// When the deadline expires, the context is cancelled, causing any in-flight
// DB queries (via context.Context) or HTTP calls to abort cleanly.
//
// This does NOT use goroutines — it relies on Go's context cancellation
// propagation. Handlers that pass ctx to DB/HTTP calls benefit automatically.
func RequestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
