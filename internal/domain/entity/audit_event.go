package entity

import (
	"encoding/json"
	"time"
)

// AuditEvent is one row in module.audit_events — a persistent record of
// a destructive admin operation. Mirrors the schema in
// migrations/031_audit_events.sql.
//
// Result is "success" or "failed"; Error is populated only when
// Result=="failed". The repo trims Error to 1024 chars before insert
// so runaway stack traces from misbehaving downstreams do not bloat
// the table.
type AuditEvent struct {
	ID         int64           `json:"id"          gorm:"primaryKey"`
	Op         string          `json:"op"          gorm:"column:op"`
	ActorID    *int64          `json:"actor_id,omitempty" gorm:"column:actor_id"`
	TargetID   *int64          `json:"target_id,omitempty" gorm:"column:target_id"`
	TargetKind string          `json:"target_kind,omitempty" gorm:"column:target_kind"`
	Params     json.RawMessage `json:"params"      gorm:"column:params;type:jsonb"`
	Result     string          `json:"result"      gorm:"column:result"`
	Error      string          `json:"error,omitempty" gorm:"column:error"`
	IP         string          `json:"ip,omitempty" gorm:"column:ip"`
	UserAgent  string          `json:"user_agent,omitempty" gorm:"column:user_agent"`
	OccurredAt time.Time       `json:"occurred_at" gorm:"column:occurred_at"`
	RequestID  string          `json:"request_id,omitempty" gorm:"column:request_id"`
}

// TableName binds the entity to the schema-qualified table name.
func (AuditEvent) TableName() string { return "module.audit_events" }
