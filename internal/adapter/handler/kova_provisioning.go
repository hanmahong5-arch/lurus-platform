package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// KovaProvisioningHandler exposes the provisioning + usage endpoints that
// glue platform-core to the kova-rest tester pool on R6.
//
// Three endpoints, three audiences:
//
//	GET  /api/v1/orgs/:id/services/kova        — tenant view (Zitadel JWT)
//	POST /internal/v1/orgs/:id/services/kova-tester — service-to-service
//	POST /internal/v1/usage/report/kova        — kova worker callback
type KovaProvisioningHandler struct {
	svc *app.KovaProvisioningService
}

// NewKovaProvisioningHandler wires the handler against an app service.
func NewKovaProvisioningHandler(svc *app.KovaProvisioningService) *KovaProvisioningHandler {
	return &KovaProvisioningHandler{svc: svc}
}

// GetKova returns the org's provisioned kova service (status, base_url, key
// prefix). The full admin key is never returned by this endpoint — it is
// emitted only at provision time. Customers that lost the key must rotate
// (rotation endpoint is out of scope for this slice).
//
// Auth: Zitadel JWT; caller must be a member of the org. Non-members get 403.
//
// GET /api/v1/orgs/:id/services/kova
func (h *KovaProvisioningHandler) GetKova(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	row, err := h.svc.GetKovaService(c.Request.Context(), orgID, accountID)
	switch {
	case errors.Is(err, app.ErrPermissionDenied):
		respondError(c, http.StatusForbidden, ErrCodeForbidden,
			"You are not a member of this organization")
		return
	case errors.Is(err, app.ErrOrgServiceNotProvisioned):
		respondError(c, http.StatusNotFound, "service_not_provisioned",
			"Kova workspace has not been provisioned for this organization yet")
		return
	case err != nil:
		respondInternalError(c, "kova_provisioning.get", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"org_id":         row.OrgID,
		"service":        row.Service,
		"status":         row.Status,
		"base_url":       row.BaseURL,
		"key_prefix":     row.KeyPrefix,
		"tester_name":    row.TesterName,
		"port":           row.Port,
		"provisioned_at": row.ProvisionedAt,
	})
}

// ProvisionKovaTester triggers (or refreshes) provisioning for an org. The
// raw admin key is returned in the response — exactly once. Subsequent GETs
// only see the SHA-256 hash.
//
// Auth: internal bearer with scope `org:provision`.
//
// POST /internal/v1/orgs/:id/services/kova-tester
func (h *KovaProvisioningHandler) ProvisionKovaTester(c *gin.Context) {
	if !requireScope(c, entity.ScopeOrgProvision) {
		return
	}
	orgID, ok := parsePathInt64(c, "id", "Organization ID")
	if !ok {
		return
	}
	result, err := h.svc.ProvisionKovaTester(c.Request.Context(), orgID)
	switch {
	case errors.Is(err, app.ErrOrgNotFound):
		respondNotFound(c, "Organization")
		return
	case err != nil:
		// Distinguish caller-fixable errors (mostly from R6 4xx) from "we
		// tried 3 times and it never worked". Both surface as 502 today —
		// the metadata.error column carries the wire-level detail.
		respondError(c, http.StatusBadGateway, ErrCodeUpstreamFailed,
			"Kova provisioning failed: "+err.Error())
		return
	}

	resp := gin.H{
		"org_id":         result.Service.OrgID,
		"service":        result.Service.Service,
		"status":         result.Service.Status,
		"base_url":       result.Service.BaseURL,
		"key_prefix":     result.Service.KeyPrefix,
		"tester_name":    result.Service.TesterName,
		"port":           result.Service.Port,
		"provisioned_at": result.Service.ProvisionedAt,
		"mock_mode":      result.IsMock,
	}
	if result.AdminKey != "" {
		// Returned ONCE. Document this in the OpenAPI spec; clients that
		// drop the response are out of luck and must trigger rotation.
		resp["admin_key"] = result.AdminKey
		resp["admin_key_warning"] = "This is the only time the admin key is returned. Store it now."
	}
	c.JSON(http.StatusOK, resp)
}

// usageReportRequest is the body shape for kova worker callbacks. Strict
// JSON tags so a typo on the worker side (e.g. token_in vs tokens_in) fails
// loudly rather than silently zeroing the metric.
type usageReportRequest struct {
	OrgID      int64     `json:"org_id"      binding:"required,gt=0"`
	Service    string    `json:"service"`
	TesterName string    `json:"tester_name"`
	AgentID    string    `json:"agent_id"`
	TokensIn   int64     `json:"tokens_in"   binding:"gte=0"`
	TokensOut  int64     `json:"tokens_out"  binding:"gte=0"`
	CostMicros int64     `json:"cost_micros" binding:"gte=0"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ReportKovaUsage ingests a single completed-run report from a kova worker.
//
// Auth: internal bearer with scope `usage:report`.
//
// Why a kova-specific path rather than reusing /internal/v1/usage/report?
// The legacy endpoint takes (account_id, amount_cny) — fine for lurus-api's
// post-LLM CNY accumulation, but lossy for kova where we want token-level
// granularity. Splitting the path keeps the legacy contract untouched while
// the new endpoint can evolve schema without negotiation.
//
// POST /internal/v1/usage/report/kova
func (h *KovaProvisioningHandler) ReportKovaUsage(c *gin.Context) {
	if !requireScope(c, entity.ScopeUsageReport) {
		return
	}
	var req usageReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}
	if req.Service == "" {
		req.Service = entity.OrgServiceKova
	}
	ev := &entity.UsageEvent{
		OrgID:      req.OrgID,
		Service:    req.Service,
		TesterName: req.TesterName,
		AgentID:    req.AgentID,
		TokensIn:   req.TokensIn,
		TokensOut:  req.TokensOut,
		CostMicros: req.CostMicros,
		OccurredAt: req.OccurredAt,
	}
	if err := h.svc.RecordUsage(c.Request.Context(), ev); err != nil {
		respondInternalError(c, "kova_provisioning.report_usage", err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"accepted": true,
		"id":       ev.ID,
	})
}
