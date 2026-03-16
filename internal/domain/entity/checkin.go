package entity

import "time"

// Checkin records a daily check-in event for an account.
type Checkin struct {
	ID          int64     `json:"id"           gorm:"primaryKey;autoIncrement"`
	AccountID   int64     `json:"account_id"   gorm:"not null;uniqueIndex:uq_checkin_daily"`
	CheckinDate string    `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:uq_checkin_daily"` // yyyy-MM-dd
	RewardType  string    `json:"reward_type"  gorm:"type:varchar(32);not null;default:'credits'"`
	RewardValue float64   `json:"reward_value" gorm:"type:decimal(14,4);not null"`
	CreatedAt   time.Time `json:"created_at"   gorm:"autoCreateTime"`
}

func (Checkin) TableName() string { return "identity.checkins" }
