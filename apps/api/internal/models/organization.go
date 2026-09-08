package models

import "github.com/google/uuid"

// Organization is the tenancy boundary: media, sessions and audit entries all
// hang off one.
type Organization struct {
	Base
	Slug        string    `gorm:"size:64;uniqueIndex;not null" json:"slug"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description,omitempty"`
	LogoURL     string    `gorm:"size:1024" json:"logoUrl,omitempty"`
	OwnerID     uuid.UUID `gorm:"type:uuid;not null;index" json:"ownerId"`

	Members []Membership `gorm:"constraint:OnDelete:CASCADE" json:"members,omitempty"`
}

func (Organization) TableName() string { return "organizations" }

// Membership joins a user to an organization with a role.
type Membership struct {
	Base
	UserID         uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_memberships_user_org,priority:1" json:"userId"`
	OrganizationID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_memberships_user_org,priority:2" json:"organizationId"`
	Role           Role      `gorm:"size:32;not null;default:member" json:"role"`

	User         *User         `gorm:"constraint:OnDelete:CASCADE" json:"user,omitempty"`
	Organization *Organization `gorm:"constraint:OnDelete:CASCADE" json:"organization,omitempty"`
}

func (Membership) TableName() string { return "memberships" }
