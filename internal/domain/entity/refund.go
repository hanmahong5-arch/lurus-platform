package entity

import "time"

// RefundStatus represents refund lifecycle states.
type RefundStatus string

const (
	RefundStatusPending   RefundStatus = "pending"
	RefundStatusApproved  RefundStatus = "approved"
	RefundStatusRejected  RefundStatus = "rejected"
	RefundStatusCompleted RefundStatus = "completed"
)

// Refund represents a customer refund request against a paid payment order.
type Refund struct {
	ID          int64        `json:"-"                      gorm:"primaryKey;autoIncrement"`
	RefundNo    string       `json:"refund_no"              gorm:"uniqueIndex;not null"`
	AccountID   int64        `json:"account_id"             gorm:"index;not null"`
	OrderNo     string       `json:"order_no"               gorm:"index;not null"`
	AmountCNY   float64      `json:"amount_cny"`
	Reason      string       `json:"reason"`
	Status      RefundStatus `json:"status"                 gorm:"default:'pending'"`
	ReviewNote  string       `json:"review_note"`
	ReviewedBy  string       `json:"reviewed_by"`
	ReviewedAt  *time.Time   `json:"reviewed_at,omitempty"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// TableName specifies the database table for Refund.
func (Refund) TableName() string { return "billing.refunds" }
