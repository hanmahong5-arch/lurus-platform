package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

// OpsCatalogHandler exposes the platform's privileged-op catalogue
// — every op the deployment knows about, with risk metadata
// suitable for rendering admin UIs and the Lutu APP confirm screen
// without hardcoding op-specific knowledge per client.
//
// The catalogue is the published contract that Phase 4 Sprint 1B
// (the APP biometric confirm screen) and Phase 4 Sprint 3 (the
// audit dashboard) consume. Publishing it before either consumer
// ships locks in the schema and lets multiple clients evolve
// independently.
type OpsCatalogHandler struct {
	registry *ops.Registry
}

// NewOpsCatalogHandler wires the handler. registry must be the same
// instance populated at boot in cmd/core/main.go; passing nil
// causes List to return 503 so a half-wired deployment is loud
// rather than silently empty.
func NewOpsCatalogHandler(registry *ops.Registry) *OpsCatalogHandler {
	return &OpsCatalogHandler{registry: registry}
}

// opCatalogEntry is the JSON shape per op. Flat + stable so admin
// UIs can render directly without further massage.
type opCatalogEntry struct {
	// Type is the canonical op identifier — the same string that
	// appears in QR delegate session params and audit log lines.
	Type string `json:"type"`
	// Description is a one-line English summary.
	Description string `json:"description"`
	// RiskLevel ∈ {info, warn, destructive} — drives UI colour and
	// whether the APP requires biometric step-up.
	RiskLevel string `json:"risk_level"`
	// Destructive is true iff RiskLevel == "destructive". Surfaced
	// separately so clients can short-circuit a single boolean
	// check without parsing the level string.
	Destructive bool `json:"destructive"`
	// Delegate is true iff this op runs on the QR-delegate APP
	// confirm path (i.e. the registered Op also satisfies
	// ops.DelegateOp). Future declarative ops would have
	// Delegate=false.
	Delegate bool `json:"delegate"`
}

// opsCatalogResponse wraps the entry list in an object so future
// fields (cursor, version, supported_levels) don't break clients
// expecting a top-level array.
type opsCatalogResponse struct {
	Ops []opCatalogEntry `json:"ops"`
}

// List — GET /admin/v1/ops
//
// Returns the full catalogue sorted by Type ascending. Read-only;
// no rate-limit since admin auth + small payload makes abuse
// uninteresting. Empty registry returns {"ops":[]} with 200 — an
// empty deployment is a valid state, not an error.
func (h *OpsCatalogHandler) List(c *gin.Context) {
	if h.registry == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "ops_registry_unavailable",
			"message": "Ops registry not wired on this deployment",
		})
		return
	}
	all := h.registry.List()
	resp := opsCatalogResponse{Ops: make([]opCatalogEntry, 0, len(all))}
	for _, op := range all {
		_, isDelegate := op.(ops.DelegateOp)
		resp.Ops = append(resp.Ops, opCatalogEntry{
			Type:        op.Type(),
			Description: op.Description(),
			RiskLevel:   string(op.RiskLevel()),
			Destructive: op.IsDestructive(),
			Delegate:    isDelegate,
		})
	}
	c.JSON(http.StatusOK, resp)
}
