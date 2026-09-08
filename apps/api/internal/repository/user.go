package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/meterrail/api/internal/models"
)

type UserRepository struct{ db *gorm.DB }

func (r *UserRepository) ByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Preload("Wallets").First(&user, "id = ?", id).Error
	return &user, wrap(err)
}

func (r *UserRepository) ByDynamicID(ctx context.Context, dynamicID string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Preload("Wallets").First(&user, "dynamic_user_id = ?", dynamicID).Error
	return &user, wrap(err)
}

// UpsertFromClaims is the provisioning path: the first time a Dynamic identity
// reaches the API we create the local row, and on every later request we keep
// the mutable profile fields in sync. Runs in one transaction with the wallet
// upsert so a half-provisioned user is never visible.
func (r *UserRepository) UpsertFromClaims(
	ctx context.Context,
	dynamicID, email, displayName string,
	wallet *models.Wallet,
) (*models.User, error) {
	var user models.User

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.First(&user, "dynamic_user_id = ?", dynamicID).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			user = models.User{
				DynamicUserID: dynamicID,
				DisplayName:   displayName,
				IsActive:      true,
			}
			if email != "" {
				user.Email = &email
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			updates := map[string]any{"last_seen_at": time.Now().UTC()}
			if email != "" && (user.Email == nil || *user.Email != email) {
				updates["email"] = email
			}
			if displayName != "" && user.DisplayName != displayName {
				updates["display_name"] = displayName
			}
			if err := tx.Model(&user).Updates(updates).Error; err != nil {
				return err
			}
		}

		if wallet == nil || wallet.Address == "" {
			return nil
		}

		wallet.UserID = user.ID
		wallet.Address = models.NormalizeAddress(wallet.Chain, wallet.Address)
		now := time.Now().UTC()
		wallet.VerifiedAt = &now

		// A wallet can be re-linked to a different account in Dynamic, so the
		// conflict target is (chain, address) and the owner is what updates.
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "chain"}, {Name: "address"}},
			DoUpdates: clause.AssignmentColumns([]string{"user_id", "provider", "chain_id", "verified_at", "updated_at"}),
		}).Create(wallet).Error
	})
	if err != nil {
		return nil, wrap(err)
	}

	return r.ByID(ctx, user.ID)
}

func (r *UserRepository) Update(ctx context.Context, id uuid.UUID, updates map[string]any) (*models.User, error) {
	if len(updates) == 0 {
		return r.ByID(ctx, id)
	}
	err := r.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", id).Updates(updates).Error
	if err != nil {
		return nil, wrap(err)
	}
	return r.ByID(ctx, id)
}

func (r *UserRepository) TouchLastSeen(ctx context.Context, id uuid.UUID) error {
	return wrap(r.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", id).
		UpdateColumn("last_seen_at", time.Now().UTC()).Error)
}

// ByWallet resolves an address back to a user, which is how onchain events get
// attributed to accounts.
func (r *UserRepository) ByWallet(ctx context.Context, chain models.WalletChain, address string) (*models.User, error) {
	var wallet models.Wallet
	err := r.db.WithContext(ctx).
		First(&wallet, "chain = ? AND address = ?", chain, models.NormalizeAddress(chain, address)).Error
	if err != nil {
		return nil, wrap(err)
	}
	return r.ByID(ctx, wallet.UserID)
}

func (r *UserRepository) List(ctx context.Context, page Page, search string) ([]models.User, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.User{})
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("display_name ILIKE ? OR email ILIKE ? OR username ILIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var users []models.User
	err := page.apply(q).Order("created_at DESC").Find(&users).Error
	return users, total, wrap(err)
}
