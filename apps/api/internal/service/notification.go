package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
)

type NotificationService struct{ deps Deps }

func (s *NotificationService) List(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit, offset int) ([]models.Notification, int64, error) {
	return s.deps.Repos.Notifications.ListForUser(ctx, userID, unreadOnly,
		repository.Page{Limit: limit, Offset: offset})
}

func (s *NotificationService) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.deps.Repos.Notifications.UnreadCount(ctx, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, userID, notificationID uuid.UUID) error {
	changed, err := s.deps.Repos.Notifications.MarkRead(ctx, userID, notificationID)
	if err != nil {
		return err
	}
	if !changed {
		// Either it does not exist, belongs to someone else, or was already
		// read. All three are indistinguishable to the caller by design.
		return httpx.NotFound("notification")
	}
	return nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.deps.Repos.Notifications.MarkAllRead(ctx, userID)
}

// Dispatch queues a notification for delivery. Callers do not write the row
// themselves so every notification goes through the same retry policy.
func (s *NotificationService) Dispatch(ctx context.Context, p jobs.NotificationSendPayload) error {
	if p.UserID == uuid.Nil {
		return httpx.BadRequest("userId is required")
	}
	if strings.TrimSpace(p.Title) == "" {
		return httpx.BadRequest("title is required")
	}
	if _, err := s.deps.Repos.Users.ByID(ctx, p.UserID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return httpx.NotFound("user")
		}
		return err
	}

	task, err := jobs.NewNotificationTask(p)
	if err != nil {
		return err
	}
	if err := s.deps.Jobs.Enqueue(ctx, task); err != nil {
		return fmt.Errorf("enqueue notification: %w", err)
	}
	return nil
}
