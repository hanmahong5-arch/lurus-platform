package entity

import "time"

// ReferralRewardEvent tracks when referral commissions are earned.
type ReferralRewardEvent struct {
	ID            int64     `json:"id"             gorm:"primaryKey;autoIncrement"`
	ReferrerID    int64     `json:"referrer_id"    gorm:"not null;index"`
	RefereeID     int64     `json:"referee_id"     gorm:"not null;index"`
	EventType     string    `json:"event_type"     gorm:"type:varchar(32)"` // signup/first_topup/first_subscription/renewal
	RewardCredits float64   `json:"reward_credits" gorm:"type:decimal(10,4);not null"`
	Status        string    `json:"status"         gorm:"type:varchar(16)"` // pending/credited/expired
	TriggeredAt   time.Time `json:"triggered_at"   gorm:"autoCreateTime"`
}

func (ReferralRewardEvent) TableName() string { return "billing.referral_reward_events" }

// Referral event type constants.
const (
	ReferralEventSignup            = "signup"
	ReferralEventFirstTopup        = "first_topup"
	ReferralEventFirstSubscription = "first_subscription"
	ReferralEventRenewal           = "renewal"
)
