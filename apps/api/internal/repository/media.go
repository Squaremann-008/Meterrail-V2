package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/meterrail/api/internal/models"
)

type MediaRepository struct{ db *gorm.DB }

func (r *MediaRepository) Create(ctx context.Context, asset *models.MediaAsset) error {
	return wrap(r.db.WithContext(ctx).Create(asset).Error)
}

func (r *MediaRepository) ByID(ctx context.Context, id uuid.UUID) (*models.MediaAsset, error) {
	var asset models.MediaAsset
	err := r.db.WithContext(ctx).First(&asset, "id = ?", id).Error
	return &asset, wrap(err)
}

func (r *MediaRepository) ByKey(ctx context.Context, key string) (*models.MediaAsset, error) {
	var asset models.MediaAsset
	err := r.db.WithContext(ctx).First(&asset, "key = ?", key).Error
	return &asset, wrap(err)
}

// MarkUploaded records the object's real size and type as reported by R2, not
// as claimed by the client.
func (r *MediaRepository) MarkUploaded(ctx context.Context, id uuid.UUID, sizeBytes int64, contentType, checksum, publicURL string) error {
	now := time.Now().UTC()
	return wrap(r.db.WithContext(ctx).Model(&models.MediaAsset{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":       models.MediaUploaded,
			"size_bytes":   sizeBytes,
			"content_type": contentType,
			"checksum":     checksum,
			"public_url":   publicURL,
			"uploaded_at":  now,
		}).Error)
}

func (r *MediaRepository) SetStatus(ctx context.Context, id uuid.UUID, status models.MediaStatus) error {
	return wrap(r.db.WithContext(ctx).Model(&models.MediaAsset{}).
		Where("id = ?", id).
		Update("status", status).Error)
}

// Finalize is what the processing worker calls once derivatives exist.
func (r *MediaRepository) Finalize(ctx context.Context, id uuid.UUID, width, height int, variants models.JSONMap) error {
	return wrap(r.db.WithContext(ctx).Model(&models.MediaAsset{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":   models.MediaReady,
			"width":    width,
			"height":   height,
			"variants": variants,
		}).Error)
}

func (r *MediaRepository) ListByOwner(ctx context.Context, ownerID uuid.UUID, page Page) ([]models.MediaAsset, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.MediaAsset{}).Where("owner_id = ?", ownerID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var assets []models.MediaAsset
	err := page.apply(q).Order("created_at DESC").Find(&assets).Error
	return assets, total, wrap(err)
}

// PendingOlderThan finds presigned uploads the client never completed, so the
// reaper can delete the row and any stray object.
func (r *MediaRepository) PendingOlderThan(ctx context.Context, age time.Duration, limit int) ([]models.MediaAsset, error) {
	cutoff := time.Now().UTC().Add(-age)
	var assets []models.MediaAsset
	err := r.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", models.MediaPending, cutoff).
		Limit(limit).
		Find(&assets).Error
	return assets, wrap(err)
}

func (r *MediaRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return wrap(r.db.WithContext(ctx).Delete(&models.MediaAsset{}, "id = ?", id).Error)
}

// HardDelete removes the row entirely; used by the reaper where a soft-deleted
// tombstone would keep the unique key on `key` occupied.
func (r *MediaRepository) HardDelete(ctx context.Context, id uuid.UUID) error {
	return wrap(r.db.WithContext(ctx).Unscoped().Delete(&models.MediaAsset{}, "id = ?", id).Error)
}

func (r *MediaRepository) DB() *gorm.DB { return r.db }
