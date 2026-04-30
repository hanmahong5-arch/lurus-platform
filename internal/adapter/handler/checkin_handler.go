package handler

import (
	"errors"
	"net/http"

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
		// Branch on the typed sentinel rather than substring-matching on
		// err.Error() — the previous strings.Contains check broke as soon
		// as the message wording was tweaked. errors.Is is stable across
		// wrapping (fmt.Errorf %w) too.
		if errors.Is(err, app.ErrCheckinAlreadyToday) {
			c.JSON(http.StatusConflict, gin.H{
				"error":      "already_checked_in_today",
				"message":    "今天已经签到过了，明天再来吧",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "checkin failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}
