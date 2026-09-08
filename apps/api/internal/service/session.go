package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/meterrail/api/internal/agora"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
)

type SessionService struct{ deps Deps }

type CreateSessionInput struct {
	Title           string     `json:"title"`
	Description     string     `json:"description,omitempty"`
	OrganizationID  *uuid.UUID `json:"organizationId,omitempty"`
	ScheduledFor    *time.Time `json:"scheduledFor,omitempty"`
	MaxParticipants int        `json:"maxParticipants,omitempty"`
	IsRecorded      bool       `json:"isRecorded,omitempty"`
}

// Create opens a room. The channel name is random rather than derived from the
// title so it cannot be guessed from public metadata.
func (s *SessionService) Create(ctx context.Context, hostID uuid.UUID, in CreateSessionInput) (*models.Session, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 255 {
		return nil, httpx.UnprocessableEntity("invalid session",
			map[string]any{"title": "must be between 1 and 255 characters"})
	}
	if in.MaxParticipants <= 0 {
		in.MaxParticipants = 16
	}
	if in.MaxParticipants > 128 {
		return nil, httpx.UnprocessableEntity("invalid session",
			map[string]any{"maxParticipants": "must be 128 or fewer"})
	}
	if in.ScheduledFor != nil && in.ScheduledFor.Before(time.Now().UTC().Add(-time.Minute)) {
		return nil, httpx.UnprocessableEntity("invalid session",
			map[string]any{"scheduledFor": "must not be in the past"})
	}
	if in.OrganizationID != nil {
		if _, err := s.deps.Repos.Organizations.RoleOf(ctx, hostID, *in.OrganizationID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, httpx.Forbidden("you are not a member of this organization")
			}
			return nil, err
		}
	}

	channel, err := randomChannelName()
	if err != nil {
		return nil, err
	}

	session := &models.Session{
		HostID:          hostID,
		OrganizationID:  in.OrganizationID,
		ChannelName:     channel,
		Title:           title,
		Description:     in.Description,
		Status:          models.SessionScheduled,
		IsRecorded:      in.IsRecorded,
		MaxParticipants: in.MaxParticipants,
		ScheduledFor:    in.ScheduledFor,
	}
	if err := s.deps.Repos.Sessions.Create(ctx, session); err != nil {
		return nil, err
	}

	// A scheduled session gets a delayed job that flips it live on time.
	if in.ScheduledFor != nil {
		task, err := jobs.NewSessionStartTask(jobs.SessionPayload{SessionID: session.ID}, *in.ScheduledFor)
		if err != nil {
			return nil, err
		}
		if err := s.deps.Jobs.Enqueue(ctx, task); err != nil {
			s.deps.Logger.Error("enqueue session start", "error", err, "session_id", session.ID)
		}
	}

	return session, nil
}

// randomChannelName produces a 16-hex-character channel, within Agora's 64-byte
// channel name limit and drawn from its permitted character set.
func randomChannelName() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate channel name: %w", err)
	}
	return "mr-" + hex.EncodeToString(buf), nil
}

func (s *SessionService) Get(ctx context.Context, id uuid.UUID) (*models.Session, error) {
	session, err := s.deps.Repos.Sessions.ByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, httpx.NotFound("session")
	}
	return session, err
}

func (s *SessionService) List(ctx context.Context, f repository.SessionFilter) ([]models.Session, int64, error) {
	return s.deps.Repos.Sessions.List(ctx, f)
}

// JoinResult is everything the client needs to connect to Agora.
type JoinResult struct {
	Session     *models.Session            `json:"session"`
	Participant *models.SessionParticipant `json:"participant"`
	RTC         *agora.Token               `json:"rtc"`
	RTM         *agora.Token               `json:"rtm,omitempty"`
}

// Join authorises a user for a session and mints their Agora tokens. The host
// is always a publisher; everyone else joins as a subscriber unless the session
// is open and has room.
func (s *SessionService) Join(ctx context.Context, userID, sessionID uuid.UUID) (*JoinResult, error) {
	if s.deps.Agora == nil {
		return nil, httpx.ServiceUnavailable("agora")
	}

	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	switch session.Status {
	case models.SessionEnded, models.SessionCancelled:
		return nil, httpx.Conflict("this session has already finished")
	}

	// Org-scoped sessions are members-only.
	if session.OrganizationID != nil {
		if _, err := s.deps.Repos.Organizations.RoleOf(ctx, userID, *session.OrganizationID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, httpx.Forbidden("this session is limited to organization members")
			}
			return nil, err
		}
	}

	role := models.ParticipantSubscriber
	if session.HostID == userID {
		role = models.ParticipantPublisher
	}

	// Capacity is only enforced for newcomers; a rejoining participant already
	// holds a slot.
	existing, err := s.deps.Repos.Sessions.ActiveParticipantCount(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if int(existing) >= session.MaxParticipants && session.HostID != userID {
		return nil, httpx.Conflict("this session is full")
	}

	agoraUID := agora.DeriveUID(userID.String())
	participant, err := s.deps.Repos.Sessions.JoinParticipant(ctx, sessionID, userID, agoraUID, role)
	if err != nil {
		return nil, err
	}

	agoraRole := agora.RoleSubscriber
	if role == models.ParticipantPublisher {
		agoraRole = agora.RolePublisher
	}

	rtc, err := s.deps.Agora.RTCToken(session.ChannelName, participant.AgoraUID, agoraRole)
	if err != nil {
		return nil, err
	}
	rtm, err := s.deps.Agora.RTMToken(userID.String())
	if err != nil {
		// Signalling is optional; video should still work without it.
		s.deps.Logger.Warn("rtm token failed", "error", err)
		rtm = nil
	}

	return &JoinResult{Session: session, Participant: participant, RTC: rtc, RTM: rtm}, nil
}

// RenewToken re-issues an RTC token for a participant already in the room,
// which the client calls a minute before expiry.
func (s *SessionService) RenewToken(ctx context.Context, userID, sessionID uuid.UUID) (*agora.Token, error) {
	if s.deps.Agora == nil {
		return nil, httpx.ServiceUnavailable("agora")
	}

	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	role := agora.RoleSubscriber
	if session.HostID == userID {
		role = agora.RolePublisher
	}
	return s.deps.Agora.RTCToken(session.ChannelName, agora.DeriveUID(userID.String()), role)
}

func (s *SessionService) Leave(ctx context.Context, userID, sessionID uuid.UUID) error {
	return s.deps.Repos.Sessions.LeaveParticipant(ctx, sessionID, userID)
}

// Start and End are host-only transitions.
func (s *SessionService) Start(ctx context.Context, userID, sessionID uuid.UUID) (*models.Session, error) {
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.HostID != userID {
		return nil, httpx.Forbidden("only the host can start this session")
	}
	changed, err := s.deps.Repos.Sessions.Start(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if !changed && session.Status != models.SessionLive {
		return nil, httpx.Conflict("this session cannot be started from its current state")
	}
	return s.Get(ctx, sessionID)
}

func (s *SessionService) End(ctx context.Context, userID, sessionID uuid.UUID) (*models.Session, error) {
	session, err := s.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.HostID != userID {
		return nil, httpx.Forbidden("only the host can end this session")
	}
	if _, err := s.deps.Repos.Sessions.End(ctx, sessionID); err != nil {
		return nil, err
	}

	task, err := jobs.NewSessionReconcileTask(jobs.SessionPayload{SessionID: sessionID})
	if err == nil {
		if err := s.deps.Jobs.Enqueue(ctx, task); err != nil {
			s.deps.Logger.Warn("enqueue session reconcile", "error", err)
		}
	}
	return s.Get(ctx, sessionID)
}
