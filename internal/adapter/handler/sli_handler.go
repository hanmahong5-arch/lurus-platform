package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/repo"
)

// SLIHandler exposes a public read-only SLO snapshot at GET /sli.
//
// The endpoint is intentionally unauthenticated: it is consumed by the
// status page, by ops dashboards, and by external uptime probes that
// have no shared secret with the platform. Nothing on the response
// reveals tenant data or internal topology — only aggregate health
// signals and the version the binary was built from.
//
// SLO targets are aspirational while in dev mode and become contractual
// once we promote to prod. Updates to any of the constants below MUST
// land in lockstep with a corresponding entry in docs/runbooks/.
type SLIHandler struct {
	hookDLQ *repo.HookFailureRepo
	// version captured at startup so /sli reflects what's actually
	// running, not whatever the latest deploy pushed to git.
	version string
}

// NewSLIHandler wires the handler. version may be the empty string;
// callers typically pass buildinfo.Get().SHA so the field always names
// a concrete image tag.
func NewSLIHandler(hookDLQ *repo.HookFailureRepo, version string) *SLIHandler {
	return &SLIHandler{hookDLQ: hookDLQ, version: version}
}

// SLO targets — change these only with a corresponding doc entry. They
// are aspirational while in dev mode and become contractual at prod.
const (
	sloWhoamiP99MS  = 500.0
	sloUptime30d    = 0.995
	sloHookDLQDepth = 0
)

// sliMode reports whether /sli is acting as a dev-mode aspirational
// snapshot or a prod-mode contractual one. The string is informational —
// alerting decides based on the numeric `current` fields, not the mode.
const (
	sliModeDev  = "dev"
	sliModeProd = "prod"
)

// Get serves GET /sli.
//
// The response shape is documented in docs/runbooks/ — clients (status
// page, dashboards) depend on the field layout, so any rename must be
// coordinated.
//
// Errors are intentionally swallowed into log + null (or -1) fields:
// /sli is a status endpoint, not a critical path, and 500-ing it would
// erase the very signal external probes look for.
func (h *SLIHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()

	// hook_dlq_pending — only "current" we can compute in-process
	// today. PendingDepth is a single COUNT against an indexed column,
	// so it's cheap enough to run on every /sli probe.
	var hookDLQCurrent int64
	hookDLQErr := ""
	if h.hookDLQ != nil {
		depth, err := h.hookDLQ.PendingDepth(ctx)
		if err != nil {
			slog.WarnContext(ctx, "sli: hook_dlq pending depth lookup failed",
				"err", err, "request_id", c.GetString("request_id"))
			hookDLQCurrent = -1
			hookDLQErr = err.Error()
		} else {
			hookDLQCurrent = depth
		}
	} else {
		// No DLQ wired (legacy/standalone) — surface as -1 with a
		// reason so operators don't read "0" as healthy.
		hookDLQCurrent = -1
		hookDLQErr = "hook_dlq_repo_not_wired"
	}

	hookDLQField := gin.H{
		"current": hookDLQCurrent,
		"target":  sloHookDLQDepth,
		"unit":    "count",
	}
	if hookDLQErr != "" {
		hookDLQField["_lookup_error"] = hookDLQErr
	}

	c.JSON(http.StatusOK, gin.H{
		"version": h.version,
		"mode":    sliModeDev,
		"slos": gin.H{
			// TODO wire to local prometheus scrape or push values
			// from middleware histograms.
			"whoami_p99_ms": gin.H{
				"current": nil,
				"target":  sloWhoamiP99MS,
				"unit":    "ms",
			},
			// TODO wire to local prometheus scrape or push values
			// from middleware histograms.
			"uptime_30d": gin.H{
				"current": nil,
				"target":  sloUptime30d,
				"unit":    "ratio",
			},
			"hook_dlq_pending": hookDLQField,
		},
		"metrics_endpoint": "/metrics",
		"note": "Targets aspirational while in dev mode; non-DLQ currents will be wired " +
			"when Prometheus is queryable from process. See docs/runbooks/ for incident response.",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
