package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/meterrail/api/internal/models"
)

type AuditRepository struct{ db *gorm.DB }

func (r *AuditRepository) Record(ctx context.Context, entry *models.AuditLog) error {
	return wrap(r.db.WithContext(ctx).Create(entry).Error)
}

func (r *AuditRepository) List(ctx context.Context, page Page) ([]models.AuditLog, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.AuditLog{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var entries []models.AuditLog
	err := page.apply(q).Order("created_at DESC").Find(&entries).Error
	return entries, total, wrap(err)
}

// Prune drops entries past the retention window. Unscoped because audit rows
// are hard-deleted, not tombstoned.
func (r *AuditRepository) Prune(ctx context.Context, retain time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-retain)
	res := r.db.WithContext(ctx).Unscoped().
		Where("created_at < ?", cutoff).
		Delete(&models.AuditLog{})
	return res.RowsAffected, wrap(res.Error)
}

func (r *AuditRepository) DB() *gorm.DB { return r.db }
