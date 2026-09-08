// Package service holds the business logic that sits between HTTP handlers and
// the repositories. Anything that touches more than one dependency — a write
// plus a cache eviction plus an enqueued job — belongs here.
package service

import (
	"log/slog"

	"github.com/meterrail/api/internal/agora"
	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/config"
	"github.com/meterrail/api/internal/envio"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/repository"
	"github.com/meterrail/api/internal/storage"
)

// Deps is everything the services may reach for. The optional members (storage,
// agora, envio) are nil when the corresponding integration is unconfigured, and
// each service degrades to a 503 rather than panicking.
type Deps struct {
	Config  *config.Config
	Logger  *slog.Logger
	Repos   *repository.Repositories
	Cache   *cache.Cache
	Jobs    *jobs.Client
	Storage *storage.Client
	Agora   *agora.Service
	Envio   *envio.Client
}

// Services is the handler-facing bundle.
type Services struct {
	Users         *UserService
	Media         *MediaService
	Sessions      *SessionService
	Onchain       *OnchainService
	Notifications *NotificationService
}

func New(deps Deps) *Services {
	return &Services{
		Users:         &UserService{deps: deps},
		Media:         &MediaService{deps: deps},
		Sessions:      &SessionService{deps: deps},
		Onchain:       &OnchainService{deps: deps},
		Notifications: &NotificationService{deps: deps},
	}
}
