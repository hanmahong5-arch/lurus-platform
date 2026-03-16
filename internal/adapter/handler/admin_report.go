package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// FinancialReportRow represents one aggregated row in the financial reconciliation report.
type FinancialReportRow struct {
	Date          string  `json:"date"`
	ProductID     string  `json:"product_id"`
	PaymentMethod string  `json:"payment_method"`
	Count         int64   `json:"count"`
	GrossCNY      float64 `json:"gross_cny"`
	RefundsCNY    float64 `json:"refunds_cny"`
	NetCNY        float64 `json:"net_cny"`
}

// ReportHandler handles administrative report endpoints.
type ReportHandler struct {
	db *gorm.DB
}

// NewReportHandler creates a new ReportHandler.
func NewReportHandler(db *gorm.DB) *ReportHandler {
	return &ReportHandler{db: db}
}

// FinancialReport handles GET /admin/v1/reports/financial.
// Query params:
//   - from: YYYY-MM-DD (default: first day of current month)
//   - to:   YYYY-MM-DD inclusive (default: today)
//   - group_by: day|month (default: day)
//
// Accept: text/csv returns a downloadable CSV file.
func (h *ReportHandler) FinancialReport(c *gin.Context) {
	now := time.Now().UTC()

	// Default range: current month.
	defaultFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	defaultTo := now

	from, err := parseDateParam(c.Query("from"), defaultFrom)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date: " + err.Error()})
		return
	}
	to, err := parseDateParam(c.Query("to"), defaultTo)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date: " + err.Error()})
		return
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to must not be before from"})
		return
	}

	// Whitelist group_by to prevent SQL injection.
	groupBy := c.DefaultQuery("group_by", "day")
	truncate := "day"
	if groupBy == "month" {
		truncate = "month"
	}

	// to is inclusive: extend to end-of-day.
	toExclusive := to.AddDate(0, 0, 1)

	// Build the aggregation query.
	// DATE_TRUNC is used with a trusted constant (truncate), not user input.
	rawSQL := fmt.Sprintf(`
		SELECT
			DATE_TRUNC('%s', created_at AT TIME ZONE 'UTC')::DATE::TEXT AS date,
			COALESCE(product_id, '')                                     AS product_id,
			COALESCE(payment_method, '')                                 AS payment_method,
			COUNT(*) FILTER (WHERE status = 'paid')                      AS count,
			COALESCE(SUM(amount_cny) FILTER (WHERE status = 'paid'),     0) AS gross_cny,
			COALESCE(SUM(amount_cny) FILTER (WHERE status = 'refunded'), 0) AS refunds_cny,
			COALESCE(SUM(amount_cny) FILTER (WHERE status = 'paid'),     0)
			  - COALESCE(SUM(amount_cny) FILTER (WHERE status = 'refunded'), 0) AS net_cny
		FROM billing.payment_orders
		WHERE created_at >= ? AND created_at < ?
		  AND status IN ('paid', 'refunded')
		GROUP BY 1, 2, 3
		ORDER BY 1, 2, 3
	`, truncate)

	var rows []FinancialReportRow
	if err := h.db.WithContext(c.Request.Context()).
		Raw(rawSQL, from, toExclusive).
		Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed: " + err.Error()})
		return
	}
	if rows == nil {
		rows = []FinancialReportRow{}
	}

	// Content negotiation: CSV download.
	if c.GetHeader("Accept") == "text/csv" {
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", `attachment; filename="financial_report.csv"`)
		w := csv.NewWriter(c.Writer)
		_ = w.Write([]string{"date", "product_id", "payment_method", "count", "gross_cny", "refunds_cny", "net_cny"})
		for _, row := range rows {
			_ = w.Write([]string{
				row.Date,
				row.ProductID,
				row.PaymentMethod,
				strconv.FormatInt(row.Count, 10),
				strconv.FormatFloat(row.GrossCNY, 'f', 2, 64),
				strconv.FormatFloat(row.RefundsCNY, 'f', 2, 64),
				strconv.FormatFloat(row.NetCNY, 'f', 2, 64),
			})
		}
		w.Flush()
		return
	}

	c.JSON(http.StatusOK, rows)
}

// parseDateParam parses a YYYY-MM-DD string; returns defaultVal if s is empty.
func parseDateParam(s string, defaultVal time.Time) (time.Time, error) {
	if s == "" {
		return defaultVal, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD, got %q", s)
	}
	return t.UTC(), nil
}
