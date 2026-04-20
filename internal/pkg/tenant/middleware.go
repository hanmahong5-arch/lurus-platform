package tenant

import "github.com/gin-gonic/gin"

// GinContextKeyAccountID matches auth.ContextKeyAccountID — duplicated here to
// avoid a tenant → auth import cycle (auth already depends on logging/pkg
// utilities that depend on tenant in the wiring graph).
const GinContextKeyAccountID = "account_id"

// Middleware propagates the account id set by the JWT auth middleware into
// the request context so downstream code (repos using WithTenant) can pick it
// up via AccountIDFromContext.
//
// Must run AFTER auth middleware. A noop when account_id is absent or invalid.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get(GinContextKeyAccountID)
		if !ok {
			c.Next()
			return
		}
		accountID, ok := v.(int64)
		if !ok || accountID <= 0 {
			c.Next()
			return
		}
		c.Request = c.Request.WithContext(WithAccountID(c.Request.Context(), accountID))
		c.Next()
	}
}
