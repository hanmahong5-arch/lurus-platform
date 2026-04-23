package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// apiPathPrefixes are the path prefixes that MUST receive JSON 404 responses
// when no handler matches. Anything else falls through to the SPA shell so
// client-side routing (/wallet, /callback, ...) works.
var apiPathPrefixes = []string{
	"/api/",
	"/internal/",
	"/admin/",
	"/webhooks/",
	"/oauth/",
	"/.well-known/",
}

// NoRouteHandler returns a gin.HandlerFunc suitable for engine.NoRoute.
// - API paths → 404 JSON (route_not_found) — never HTML.
// - Static asset paths present in webFS → serve the file.
// - Anything else → index.html (SPA client-side routing).
//
// This fixes the class of bug where a disabled or misrouted API endpoint
// would fall through to the SPA and cause "Unexpected token '<'" JSON
// parse errors at the client.
func NoRouteHandler(webFS fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, p := range apiPathPrefixes {
			if strings.HasPrefix(path, p) {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "route_not_found",
					"path":    path,
					"method":  c.Request.Method,
					"message": "No handler registered for this path. If this is a new feature, the responsible service may be disabled (missing required config).",
				})
				return
			}
		}

		// Non-API — serve static asset or SPA shell.
		reqPath := strings.TrimPrefix(path, "/")
		if reqPath != "" {
			if _, err := webFS.Open(reqPath); err == nil {
				http.FileServerFS(webFS).ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		data, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	}
}
