package models

import (
	"time"

	"github.com/google/uuid"
)

type NotificationChannel string

const (
	NotifyInApp   NotificationChannel = "in_app"
	NotifyEmail   NotificationChannel = "email"
	NotifyWebhook NotificationChannel = "webhook"
)

// Notification is queued by the worker and drained by the client.
type Notification struct {
	Base
	UserID      uuid.UUID           `gorm:"type:uuid;not null;index:idx_notifications_user_read,priority:1" json:"userId"`
	Channel     NotificationChannel `gorm:"size:32;not null;default:in_app" json:"channel"`
	Kind        string              `gorm:"size:64;not null;index" json:"kind"`
	Title       string              `gorm:"size:255;not null" json:"title"`
	Body        string              `gorm:"type:text" json:"body,omitempty"`
	Data        JSONMap             `gorm:"type:jsonb" json:"data,omitempty"`
	ReadAt      *time.Time          `gorm:"index:idx_notifications_user_read,priority:2" json:"readAt,omitempty"`
	DeliveredAt *time.Time          `json:"deliveredAt,omitempty"`

	User *User `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

func (Notification) TableName() string { return "notifications" }

// AuditLog is an append-only trail of privileged actions.
type AuditLog struct {
	Base
	ActorID        *uuid.UUID `gorm:"type:uuid;index" json:"actorId,omitempty"`
	OrganizationID *uuid.UUID `gorm:"type:uuid;index" json:"organizationId,omitempty"`
	Action         string     `gorm:"size:128;not null;index" json:"action"`
	ResourceType   string     `gorm:"size:64;not null" json:"resourceType"`
	ResourceID     string     `gorm:"size:128" json:"resourceId,omitempty"`
	IPAddress      string     `gorm:"size:64" json:"ipAddress,omitempty"`
	UserAgent      string     `gorm:"size:512" json:"userAgent,omitempty"`
	Metadata       JSONMap    `gorm:"type:jsonb" json:"metadata,omitempty"`
}

func (AuditLog) TableName() string { return "audit_logs" }
