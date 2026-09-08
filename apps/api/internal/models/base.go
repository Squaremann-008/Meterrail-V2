// Package models holds the GORM entities and the canonical schema for the API.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base carries the identity and audit columns shared by every table.
type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	CreatedAt time.Time      `gorm:"not null;index" json:"createdAt"`
	UpdatedAt time.Time      `gorm:"not null" json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate assigns a UUIDv7 so primary keys stay roughly time-ordered,
// which keeps the btree index from fragmenting the way v4 does.
func (b *Base) BeforeCreate(*gorm.DB) error {
	if b.ID != uuid.Nil {
		return nil
	}
	// NewV7 only fails if the entropy source does, and v4 is an equally valid
	// key when it happens, so the failure is absorbed rather than propagated.
	if id, err := uuid.NewV7(); err == nil {
		b.ID = id
	} else {
		b.ID = uuid.New()
	}
	return nil
}

// All returns every model in dependency order for AutoMigrate.
func All() []any {
	return []any{
		&User{},
		&Wallet{},
		&Organization{},
		&Membership{},
		&MediaAsset{},
		&Session{},
		&SessionParticipant{},
		&OnchainEvent{},
		&IndexerCheckpoint{},
		&Notification{},
		&AuditLog{},
	}
}
