package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
)

// CheckinHandler handles daily check-in endpoints.
type CheckinHandler struct {
	checkin *app.CheckinService
}

// NewCheckinHandler creates the handler.
func NewCheckinHandler(checkin *app.CheckinService) *CheckinHandler {
	return &CheckinHandler{checkin: checkin}
}

// GetStatus returns the current check-in status for the authenticated user.
// GET /api/v1/checkin/status
func (h *CheckinHandler) GetStatus(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	status, err := h.checkin.GetStatus(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get checkin status"})
		return
	}
	c.JSON(http.StatusOK, status)
}

// DoCheckin performs a daily check-in for the authenticated user.
// POST /api/v1/checkin
func (h *CheckinHandler) DoCheckin(c *gin.Context) {
	accountID, ok := requireAccountID(c)
	if !ok {
		return
	}
	result, err := h.checkin.DoCheckin(c.Request.Context(), accountID)
	if err != nil {
		if strings.Contains(err.Error(), "already checked in") {
			c.JSON(http.StatusConflict, gin.H{"error": "already checked in today"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "checkin failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}
