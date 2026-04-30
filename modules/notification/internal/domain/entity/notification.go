// Package entity defines domain entities for the notification service.
package entity

import "time"

// Channel represents a notification delivery channel.
type Channel string

const (
	ChannelInApp Channel = "in_app"
	ChannelEmail Channel = "email"
	ChannelFCM   Channel = "fcm"
)

// Priority defines notification urgency levels.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// Status tracks notification delivery state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusSent      Status = "sent"
	StatusDelivered Status = "delivered"
	StatusRetrying  Status = "retrying"
	StatusFailed    Status = "failed"
	StatusRead      Status = "read"
)

// Notification is the core domain entity.
type Notification struct {
	ID         int64      `json:"id" gorm:"primaryKey;autoIncrement"`
	AccountID  int64      `json:"account_id" gorm:"index;not null"`
	Channel    Channel    `json:"channel" gorm:"type:varchar(20);not null"`
	Source     string     `json:"source" gorm:"type:varchar(20);not null;default:'identity';index"`
	Category   string     `json:"category" gorm:"type:varchar(50);not null"`
	Title      string     `json:"title" gorm:"type:varchar(200);not null"`
	Body       string     `json:"body" gorm:"type:text;not null"`
	Priority   Priority   `json:"priority" gorm:"type:varchar(20);default:'normal'"`
	Status     Status     `json:"status" gorm:"type:varchar(20);default:'pending'"`
	EventType  string     `json:"event_type" gorm:"type:varchar(100)"`
	EventID    string     `json:"event_id" gorm:"type:varchar(50);index"`
	Metadata   string     `json:"metadata" gorm:"type:jsonb;default:'{}'"`
	Payload    string     `json:"payload" gorm:"type:jsonb;not null;default:'{}'"`
	ReadAt     *time.Time `json:"read_at"`
	SentAt     *time.Time `json:"sent_at"`
	CreatedAt  time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName sets the PostgreSQL table name.
func (Notification) TableName() string { return "notification.notifications" }

// Template defines a notification template for a given event type and channel.
type Template struct {
	ID        int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	EventType string  `json:"event_type" gorm:"type:varchar(100);uniqueIndex:idx_template_event_channel;not null"`
	Channel   Channel `json:"channel" gorm:"type:varchar(20);uniqueIndex:idx_template_event_channel;not null"`
	Title     string  `json:"title" gorm:"type:varchar(200);not null"`
	Body      string  `json:"body" gorm:"type:text;not null"`
	Priority  Priority `json:"priority" gorm:"type:varchar(20);default:'normal'"`
	Enabled   bool    `json:"enabled" gorm:"default:true"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName sets the PostgreSQL table name.
func (Template) TableName() string { return "notification.templates" }

// Preference stores per-account notification channel preferences.
type Preference struct {
	ID        int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	AccountID int64   `json:"account_id" gorm:"uniqueIndex:idx_pref_account_channel;not null"`
	Channel   Channel `json:"channel" gorm:"type:varchar(20);uniqueIndex:idx_pref_account_channel;not null"`
	Enabled   bool    `json:"enabled" gorm:"default:true"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName sets the PostgreSQL table name.
func (Preference) TableName() string { return "notification.preferences" }

// DeviceToken stores FCM/APNs device tokens for push notifications.
type DeviceToken struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	AccountID int64     `json:"account_id" gorm:"index;not null"`
	Platform  string    `json:"platform" gorm:"type:varchar(20);not null"` // "ios", "android"
	Token     string    `json:"token" gorm:"type:varchar(500);uniqueIndex;not null"`
	Active    bool      `json:"active" gorm:"default:true"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName sets the PostgreSQL table name.
func (DeviceToken) TableName() string { return "notification.device_tokens" }
