package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/meterrail/api/internal/models"
)

type OrganizationRepository struct{ db *gorm.DB }

// Create writes the org and its owner membership atomically; an org without an
// owner row would be unreachable through the permission checks.
func (r *OrganizationRepository) Create(ctx context.Context, org *models.Organization) error {
	return wrap(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(org).Error; err != nil {
			return err
		}
		return tx.Create(&models.Membership{
			UserID:         org.OwnerID,
			OrganizationID: org.ID,
			Role:           models.RoleOwner,
		}).Error
	}))
}

func (r *OrganizationRepository) ByID(ctx context.Context, id uuid.UUID) (*models.Organization, error) {
	var org models.Organization
	err := r.db.WithContext(ctx).First(&org, "id = ?", id).Error
	return &org, wrap(err)
}

func (r *OrganizationRepository) BySlug(ctx context.Context, slug string) (*models.Organization, error) {
	var org models.Organization
	err := r.db.WithContext(ctx).First(&org, "slug = ?", slug).Error
	return &org, wrap(err)
}

// ForUser lists the organizations a user belongs to, in one join rather than
// N+1 membership lookups.
func (r *OrganizationRepository) ForUser(ctx context.Context, userID uuid.UUID) ([]models.Organization, error) {
	var orgs []models.Organization
	err := r.db.WithContext(ctx).
		Joins("JOIN memberships ON memberships.organization_id = organizations.id").
		Where("memberships.user_id = ? AND memberships.deleted_at IS NULL", userID).
		Order("organizations.created_at DESC").
		Find(&orgs).Error
	return orgs, wrap(err)
}

// RoleOf returns the caller's role in an org, or ErrNotFound when they are not
// a member. This is the primitive every org-scoped authorization check uses.
func (r *OrganizationRepository) RoleOf(ctx context.Context, userID, orgID uuid.UUID) (models.Role, error) {
	var membership models.Membership
	err := r.db.WithContext(ctx).
		First(&membership, "user_id = ? AND organization_id = ?", userID, orgID).Error
	if err != nil {
		return "", wrap(err)
	}
	return membership.Role, nil
}

func (r *OrganizationRepository) AddMember(ctx context.Context, orgID, userID uuid.UUID, role models.Role) error {
	return wrap(r.db.WithContext(ctx).Create(&models.Membership{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
	}).Error)
}

func (r *OrganizationRepository) Members(ctx context.Context, orgID uuid.UUID, page Page) ([]models.Membership, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.Membership{}).Where("organization_id = ?", orgID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var members []models.Membership
	err := page.apply(q).Preload("User").Order("created_at ASC").Find(&members).Error
	return members, total, wrap(err)
}

func (r *OrganizationRepository) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	return wrap(r.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		Delete(&models.Membership{}).Error)
}
