package models

import (
	"time"

	"github.com/google/uuid"
)

type MediaStatus string

const (
	// MediaPending means a presigned PUT was issued but the client has not
	// confirmed the upload yet.
	MediaPending    MediaStatus = "pending"
	MediaUploaded   MediaStatus = "uploaded"
	MediaProcessing MediaStatus = "processing"
	MediaReady      MediaStatus = "ready"
	MediaFailed     MediaStatus = "failed"
)

// MediaAsset tracks an object in the Cloudflare R2 bucket. The row is created
// when the presigned URL is handed out so orphaned objects can be reaped.
type MediaAsset struct {
	Base
	OrganizationID *uuid.UUID  `gorm:"type:uuid;index" json:"organizationId,omitempty"`
	OwnerID        uuid.UUID   `gorm:"type:uuid;not null;index" json:"ownerId"`
	Key            string      `gorm:"size:1024;uniqueIndex;not null" json:"key"`
	Bucket         string      `gorm:"size:128;not null" json:"bucket"`
	Filename       string      `gorm:"size:512;not null" json:"filename"`
	ContentType    string      `gorm:"size:255;not null" json:"contentType"`
	SizeBytes      int64       `gorm:"not null;default:0" json:"sizeBytes"`
	Checksum       string      `gorm:"size:128" json:"checksum,omitempty"`
	Width          int         `json:"width,omitempty"`
	Height         int         `json:"height,omitempty"`
	Status         MediaStatus `gorm:"size:32;not null;default:pending;index" json:"status"`
	PublicURL      string      `gorm:"size:2048" json:"publicUrl,omitempty"`
	Variants       JSONMap     `gorm:"type:jsonb" json:"variants,omitempty"`
	Metadata       JSONMap     `gorm:"type:jsonb" json:"metadata,omitempty"`
	UploadedAt     *time.Time  `json:"uploadedAt,omitempty"`

	Owner *User `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

func (MediaAsset) TableName() string { return "media_assets" }
