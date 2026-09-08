package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/meterrail/api/internal/models"
)

type SessionRepository struct{ db *gorm.DB }

func (r *SessionRepository) Create(ctx context.Context, session *models.Session) error {
	return wrap(r.db.WithContext(ctx).Create(session).Error)
}

func (r *SessionRepository) ByID(ctx context.Context, id uuid.UUID) (*models.Session, error) {
	var session models.Session
	err := r.db.WithContext(ctx).
		Preload("Host").
		Preload("Participants").
		Preload("Participants.User").
		First(&session, "id = ?", id).Error
	return &session, wrap(err)
}

func (r *SessionRepository) ByChannel(ctx context.Context, channel string) (*models.Session, error) {
	var session models.Session
	err := r.db.WithContext(ctx).First(&session, "channel_name = ?", channel).Error
	return &session, wrap(err)
}

type SessionFilter struct {
	OrganizationID *uuid.UUID
	HostID         *uuid.UUID
	Status         models.SessionStatus
	Page           Page
}

func (r *SessionRepository) List(ctx context.Context, f SessionFilter) ([]models.Session, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.Session{})
	if f.OrganizationID != nil {
		q = q.Where("organization_id = ?", *f.OrganizationID)
	}
	if f.HostID != nil {
		q = q.Where("host_id = ?", *f.HostID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, wrap(err)
	}

	var sessions []models.Session
	err := f.Page.apply(q).Preload("Host").Order("created_at DESC").Find(&sessions).Error
	return sessions, total, wrap(err)
}

// Start flips a session live. The guard on status makes concurrent start calls
// idempotent: only the first UPDATE matches a row.
func (r *SessionRepository) Start(ctx context.Context, id uuid.UUID) (bool, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&models.Session{}).
		Where("id = ? AND status = ?", id, models.SessionScheduled).
		Updates(map[string]any{"status": models.SessionLive, "started_at": now})
	if res.Error != nil {
		return false, wrap(res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *SessionRepository) End(ctx context.Context, id uuid.UUID) (bool, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&models.Session{}).
		Where("id = ? AND status = ?", id, models.SessionLive).
		Updates(map[string]any{"status": models.SessionEnded, "ended_at": now})
	if res.Error != nil {
		return false, wrap(res.Error)
	}
	return res.RowsAffected > 0, nil
}

// JoinParticipant registers a user in a session, reusing the existing row (and
// therefore the existing Agora uid) if they rejoin.
func (r *SessionRepository) JoinParticipant(ctx context.Context, sessionID, userID uuid.UUID, agoraUID uint32, role models.ParticipantRole) (*models.SessionParticipant, error) {
	var participant models.SessionParticipant
	now := time.Now().UTC()

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.First(&participant, "session_id = ? AND user_id = ?", sessionID, userID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			participant = models.SessionParticipant{
				SessionID: sessionID,
				UserID:    userID,
				AgoraUID:  agoraUID,
				Role:      role,
				JoinedAt:  &now,
			}
			return tx.Create(&participant).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&participant).Updates(map[string]any{
			"role":      role,
			"joined_at": now,
			"left_at":   nil,
		}).Error
	})
	if err != nil {
		return nil, wrap(err)
	}
	return &participant, nil
}

func (r *SessionRepository) LeaveParticipant(ctx context.Context, sessionID, userID uuid.UUID) error {
	return wrap(r.db.WithContext(ctx).Model(&models.SessionParticipant{}).
		Where("session_id = ? AND user_id = ?", sessionID, userID).
		Update("left_at", time.Now().UTC()).Error)
}

// ActiveParticipantCount counts who is currently in the room, used to enforce
// MaxParticipants before minting another token.
func (r *SessionRepository) ActiveParticipantCount(ctx context.Context, sessionID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.SessionParticipant{}).
		Where("session_id = ? AND joined_at IS NOT NULL AND left_at IS NULL", sessionID).
		Count(&count).Error
	return count, wrap(err)
}

// StaleLive finds sessions that were left live past their expected window, for
// the reconciliation cron to close out.
func (r *SessionRepository) StaleLive(ctx context.Context, olderThan time.Duration, limit int) ([]models.Session, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	var sessions []models.Session
	err := r.db.WithContext(ctx).
		Where("status = ? AND started_at < ?", models.SessionLive, cutoff).
		Limit(limit).
		Find(&sessions).Error
	return sessions, wrap(err)
}

// DueToStart returns scheduled sessions whose start time has arrived.
func (r *SessionRepository) DueToStart(ctx context.Context, limit int) ([]models.Session, error) {
	var sessions []models.Session
	err := r.db.WithContext(ctx).
		Where("status = ? AND scheduled_for IS NOT NULL AND scheduled_for <= ?",
			models.SessionScheduled, time.Now().UTC()).
		Limit(limit).
		Find(&sessions).Error
	return sessions, wrap(err)
}
