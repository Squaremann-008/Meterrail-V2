// Package agora mints RTC and RTM tokens. The app certificate never leaves the
// server; clients call the API for a short-lived token and renew before expiry.
package agora

import (
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	rtc "github.com/AgoraIO-Community/go-tokenbuilder/rtctokenbuilder2"
	rtm "github.com/AgoraIO-Community/go-tokenbuilder/rtmtokenbuilder2"

	"github.com/meterrail/api/internal/config"
)

var ErrNotConfigured = errors.New("agora: AGORA_APP_ID and AGORA_APP_CERTIFICATE are required")

type Role string

const (
	RolePublisher  Role = "publisher"
	RoleSubscriber Role = "subscriber"
)

func (r Role) agora() rtc.Role {
	if r == RolePublisher {
		return rtc.RolePublisher
	}
	return rtc.RoleSubscriber
}

func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RolePublisher:
		return RolePublisher, nil
	case RoleSubscriber, "":
		return RoleSubscriber, nil
	}
	return "", fmt.Errorf("agora: unknown role %q", s)
}

type Service struct {
	cfg config.Agora
}

func New(cfg config.Agora) (*Service, error) {
	if !cfg.Configured() {
		return nil, ErrNotConfigured
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) AppID() string { return s.cfg.AppID }

// Token is what the client needs to join a channel.
type Token struct {
	AppID       string    `json:"appId"`
	Channel     string    `json:"channel"`
	Token       string    `json:"token"`
	UID         uint32    `json:"uid"`
	Role        Role      `json:"role"`
	ExpiresAt   time.Time `json:"expiresAt"`
	ExpiresInMs int64     `json:"expiresInMs"`
}

// RTCToken signs a join credential for one channel and uid.
func (s *Service) RTCToken(channel string, uid uint32, role Role) (*Token, error) {
	if channel == "" {
		return nil, errors.New("agora: channel is required")
	}
	ttl := uint32(s.cfg.TokenTTL.Seconds())

	token, err := rtc.BuildTokenWithUid(
		s.cfg.AppID,
		s.cfg.AppCertificate,
		channel,
		uid,
		role.agora(),
		ttl, // token lifetime
		ttl, // privilege lifetime
	)
	if err != nil {
		return nil, fmt.Errorf("agora: build rtc token: %w", err)
	}

	expires := time.Now().UTC().Add(s.cfg.TokenTTL)
	return &Token{
		AppID:       s.cfg.AppID,
		Channel:     channel,
		Token:       token,
		UID:         uid,
		Role:        role,
		ExpiresAt:   expires,
		ExpiresInMs: s.cfg.TokenTTL.Milliseconds(),
	}, nil
}

// RTMToken signs a credential for Agora's signaling/messaging product, used for
// in-session chat and presence alongside the RTC stream.
func (s *Service) RTMToken(userID string) (*Token, error) {
	if userID == "" {
		return nil, errors.New("agora: userID is required")
	}
	ttl := uint32(s.cfg.TokenTTL.Seconds())

	token, err := rtm.BuildToken(s.cfg.AppID, s.cfg.AppCertificate, userID, ttl)
	if err != nil {
		return nil, fmt.Errorf("agora: build rtm token: %w", err)
	}

	return &Token{
		AppID:       s.cfg.AppID,
		Token:       token,
		ExpiresAt:   time.Now().UTC().Add(s.cfg.TokenTTL),
		ExpiresInMs: s.cfg.TokenTTL.Milliseconds(),
	}, nil
}

// DeriveUID maps a stable string (our user UUID) onto the uint32 Agora wants.
// Collisions inside one channel would evict a participant, so callers persist
// the result per session and re-probe on conflict.
func DeriveUID(seed string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	uid := h.Sum32()
	if uid == 0 {
		// 0 tells the SDK to assign a uid itself, which breaks token binding.
		return 1
	}
	return uid
}
