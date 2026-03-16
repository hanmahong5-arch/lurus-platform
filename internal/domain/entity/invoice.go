package entity

import (
	"encoding/json"
	"time"
)

// InvoiceStatus represents invoice lifecycle states.
type InvoiceStatus string

const (
	InvoiceStatusDraft  InvoiceStatus = "draft"
	InvoiceStatusIssued InvoiceStatus = "issued"
)

// InvoiceLineItem is a single line in an invoice.
type InvoiceLineItem struct {
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Amount      float64 `json:"amount"`
}

// Invoice represents a billing invoice linked to a payment order.
// One invoice is generated per paid payment order (enforced by unique index on order_no).
type Invoice struct {
	ID          int64           `json:"-"            gorm:"primaryKey;autoIncrement"`
	InvoiceNo   string          `json:"invoice_no"   gorm:"uniqueIndex;not null"`
	AccountID   int64           `json:"account_id"   gorm:"index;not null"`
	OrderNo     string          `json:"order_no"     gorm:"uniqueIndex;not null"` // one invoice per order
	IssueDate   time.Time       `json:"issue_date"`
	LineItems   json.RawMessage `json:"line_items"   gorm:"type:jsonb;default:'[]'"`
	SubtotalCNY float64         `json:"subtotal_cny"`
	TotalCNY    float64         `json:"total_cny"`
	Currency    string          `json:"currency"     gorm:"default:'CNY'"`
	Status      InvoiceStatus   `json:"status"       gorm:"default:'issued'"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// TableName specifies the database table for Invoice.
func (Invoice) TableName() string { return "billing.invoices" }
