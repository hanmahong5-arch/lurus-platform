package slogctx

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Middleware generates a request_id and attaches it to context.Context so that
// the slog Handler can include it in every log entry for the request lifecycle.
// account_id is injected separately by the auth middleware after JWT validation.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Header("X-Request-ID", requestID)

		ctx := WithRequestID(c.Request.Context(), requestID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
