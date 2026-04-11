package entity

import "time"

// ReconciliationIssue tracks a discrepancy found between payment orders
// and wallet transactions (or provider-side records).
type ReconciliationIssue struct {
	ID             int64      `json:"id"              gorm:"primaryKey;autoIncrement"`
	IssueType      string     `json:"issue_type"      gorm:"type:varchar(32);not null"`
	Severity       string     `json:"severity"        gorm:"type:varchar(16);default:'warning'"`
	OrderNo        string     `json:"order_no"        gorm:"type:varchar(64)"`
	AccountID      *int64     `json:"account_id"`
	Provider       string     `json:"provider"        gorm:"type:varchar(32)"`
	ExpectedAmount *float64   `json:"expected_amount" gorm:"type:decimal(10,2)"`
	ActualAmount   *float64   `json:"actual_amount"   gorm:"type:decimal(10,2)"`
	Description    string     `json:"description"     gorm:"type:text;not null"`
	Status         string     `json:"status"          gorm:"type:varchar(16);default:'open'"`
	Resolution     string     `json:"resolution"      gorm:"type:text"`
	DetectedAt     time.Time  `json:"detected_at"     gorm:"autoCreateTime"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	CreatedAt      time.Time  `json:"created_at"      gorm:"autoCreateTime"`
}

func (ReconciliationIssue) TableName() string { return "billing.reconciliation_issues" }

// Issue type constants.
const (
	ReconIssueMissingCredit  = "missing_credit"  // order paid but no wallet credit
	ReconIssueOrphanPayment  = "orphan_payment"  // wallet credit without matching paid order
	ReconIssueAmountMismatch = "amount_mismatch"  // amounts don't match
)

// Issue status constants.
const (
	ReconStatusOpen     = "open"
	ReconStatusResolved = "resolved"
	ReconStatusIgnored  = "ignored"
)

// PaidOrderWithoutCredit is a projection returned by the integrity check query.
type PaidOrderWithoutCredit struct {
	OrderNo       string  `json:"order_no"`
	AccountID     int64   `json:"account_id"`
	AmountCNY     float64 `json:"amount_cny"`
	PaymentMethod string  `json:"payment_method"`
	PaidAt        *time.Time
}
