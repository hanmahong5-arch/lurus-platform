package handler

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

const (
	adminOpsMaxCount = 1000
	adminOpsMinCount = 1
)

// batchCodeRequest is the request body for POST /admin/v1/redemption-codes/batch.
type batchCodeRequest struct {
	Count        int        `json:"count"`
	ProductID    string     `json:"product_id"`
	PlanCode     string     `json:"plan_code"`
	DurationDays int        `json:"duration_days"`
	Notes        string     `json:"notes"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

// AdminOpsHandler handles administrative operation endpoints.
type AdminOpsHandler struct {
	referrals *app.ReferralService
}

// NewAdminOpsHandler creates a new AdminOpsHandler.
func NewAdminOpsHandler(referrals *app.ReferralService) *AdminOpsHandler {
	return &AdminOpsHandler{referrals: referrals}
}

// BatchGenerateCodes handles POST /admin/v1/redemption-codes/batch.
// Generates up to 1000 unique redemption codes.
// Responds with JSON by default; sends CSV when Accept: text/csv.
func (h *AdminOpsHandler) BatchGenerateCodes(c *gin.Context) {
	var req batchCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		handleBindError(c, err)
		return
	}

	if req.Count < adminOpsMinCount || req.Count > adminOpsMaxCount {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"count must be between 1 and 1000")
		return
	}
	if req.ProductID == "" {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"product_id is required")
		return
	}
	if req.PlanCode == "" {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"plan_code is required")
		return
	}
	if req.DurationDays <= 0 {
		respondError(c, http.StatusBadRequest, ErrCodeInvalidParameter,
			"duration_days must be positive")
		return
	}

	codes, err := h.referrals.BulkGenerateCodes(
		c.Request.Context(),
		req.ProductID,
		req.PlanCode,
		req.DurationDays,
		req.ExpiresAt,
		req.Notes,
		req.Count,
	)
	if err != nil {
		respondInternalError(c, "admin.generate_codes", err)
		return
	}

	// Content negotiation: CSV export when Accept header is text/csv.
	if c.GetHeader("Accept") == "text/csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", `attachment; filename="codes.csv"`)
		w := csv.NewWriter(c.Writer)
		_ = w.Write([]string{"code", "product_id", "plan_code", "duration_days", "expires_at", "notes"})
		for _, code := range codes {
			exp := ""
			if code.ExpiresAt != nil {
				exp = code.ExpiresAt.Format(time.RFC3339)
			}
			_ = w.Write([]string{
				code.Code,
				code.ProductID,
				req.PlanCode,
				strconv.Itoa(req.DurationDays),
				exp,
				req.Notes,
			})
		}
		w.Flush()
		return
	}

	c.JSON(http.StatusOK, codes)
}
