package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/meterrail/api/internal/models"
)

type NotificationRepository struct{ db *gorm.DB }

func (r *NotificationRepository) Create(ctx context.Context, n *models.Notification) error {
	return wrap(r.db.WithContext(ctx).Create(n).Error)
}

func (r *NotificationRepository) ListForUser(ctx context.Context, userID uuid.UUID, unreadOnly bool, page Page) ([]models.Notification, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.Notification{}).Where("user_id = ?", userID)
	if unreadOnly {
		q = q.Where("read_at IS NULL")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var items []models.Notification
	err := page.apply(q).Order("created_at DESC").Find(&items).Error
	return items, total, wrap(err)
}

func (r *NotificationRepository) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Count(&count).Error
	return count, wrap(err)
}

// MarkRead scopes the update by user id as well as notification id so one user
// cannot mark another's notification read by guessing an id.
func (r *NotificationRepository) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", notificationID, userID).
		Update("read_at", time.Now().UTC())
	return res.RowsAffected > 0, wrap(res.Error)
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	res := r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", time.Now().UTC())
	return res.RowsAffected, wrap(res.Error)
}

func (r *NotificationRepository) MarkDelivered(ctx context.Context, id uuid.UUID) error {
	return wrap(r.db.WithContext(ctx).Model(&models.Notification{}).
		Where("id = ?", id).
		Update("delivered_at", time.Now().UTC()).Error)
}

func (r *NotificationRepository) DB() *gorm.DB { return r.db }
