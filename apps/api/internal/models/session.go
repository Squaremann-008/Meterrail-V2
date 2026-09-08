package models

import (
	"time"

	"github.com/google/uuid"
)

type SessionStatus string

const (
	SessionScheduled SessionStatus = "scheduled"
	SessionLive      SessionStatus = "live"
	SessionEnded     SessionStatus = "ended"
	SessionCancelled SessionStatus = "cancelled"
)

// Session is a real-time Agora room. ChannelName is what clients join and is
// the value signed into the RTC token.
type Session struct {
	Base
	OrganizationID  *uuid.UUID    `gorm:"type:uuid;index" json:"organizationId,omitempty"`
	HostID          uuid.UUID     `gorm:"type:uuid;not null;index" json:"hostId"`
	ChannelName     string        `gorm:"size:64;uniqueIndex;not null" json:"channelName"`
	Title           string        `gorm:"size:255;not null" json:"title"`
	Description     string        `gorm:"type:text" json:"description,omitempty"`
	Status          SessionStatus `gorm:"size:32;not null;default:scheduled;index" json:"status"`
	IsRecorded      bool          `gorm:"not null;default:false" json:"isRecorded"`
	MaxParticipants int           `gorm:"not null;default:16" json:"maxParticipants"`
	ScheduledFor    *time.Time    `gorm:"index" json:"scheduledFor,omitempty"`
	StartedAt       *time.Time    `json:"startedAt,omitempty"`
	EndedAt         *time.Time    `json:"endedAt,omitempty"`
	RecordingID     *uuid.UUID    `gorm:"type:uuid" json:"recordingId,omitempty"`
	Metadata        JSONMap       `gorm:"type:jsonb" json:"metadata,omitempty"`

	Host         *User                `gorm:"constraint:OnDelete:CASCADE" json:"host,omitempty"`
	Participants []SessionParticipant `gorm:"constraint:OnDelete:CASCADE" json:"participants,omitempty"`
}

func (Session) TableName() string { return "sessions" }

type ParticipantRole string

const (
	ParticipantPublisher  ParticipantRole = "publisher"
	ParticipantSubscriber ParticipantRole = "subscriber"
)

// SessionParticipant records who joined a session and with which Agora UID.
type SessionParticipant struct {
	Base
	SessionID uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_participants_session_user,priority:1" json:"sessionId"`
	UserID    uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_participants_session_user,priority:2" json:"userId"`
	AgoraUID  uint32          `gorm:"not null" json:"agoraUid"`
	Role      ParticipantRole `gorm:"size:32;not null;default:subscriber" json:"role"`
	JoinedAt  *time.Time      `json:"joinedAt,omitempty"`
	LeftAt    *time.Time      `json:"leftAt,omitempty"`

	Session *Session `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	User    *User    `gorm:"constraint:OnDelete:CASCADE" json:"user,omitempty"`
}

func (SessionParticipant) TableName() string { return "session_participants" }
