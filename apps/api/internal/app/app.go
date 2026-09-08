// Package app wires every dependency together once, so the server, worker and
// CLI binaries all boot from the same graph instead of each assembling their
// own slightly different one.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/meterrail/api/internal/agora"
	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/config"
	"github.com/meterrail/api/internal/database"
	"github.com/meterrail/api/internal/envio"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/logging"
	"github.com/meterrail/api/internal/repository"
	"github.com/meterrail/api/internal/service"
	"github.com/meterrail/api/internal/storage"
)

// App holds every long-lived dependency. The optional integrations are nil when
// unconfigured; each consumer degrades rather than crashing, so a developer can
// run the API with nothing but Postgres and Redis.
type App struct {
	Config   *config.Config
	Logger   *slog.Logger
	DB       *gorm.DB
	Cache    *cache.Cache
	Jobs     *jobs.Client
	Repos    *repository.Repositories
	Services *service.Services

	Storage  *storage.Client
	Agora    *agora.Service
	Envio    *envio.Client
	Verifier *auth.Verifier
}

// Options tunes what a given binary needs at boot.
type Options struct {
	// ServiceName appears in every log line.
	ServiceName string
	// SkipAuth avoids the JWKS fetch for binaries that serve no requests.
	SkipAuth bool
}

// New boots the dependency graph. Postgres and Redis are hard requirements;
// everything else logs a warning and is left nil.
func New(ctx context.Context, cfg *config.Config, opts Options) (*App, error) {
	if opts.ServiceName == "" {
		opts.ServiceName = cfg.App.Name
	}
	logger := logging.New(cfg.App.LogLevel, cfg.App.LogFormat, opts.ServiceName, cfg.App.Environment)
	slog.SetDefault(logger)

	db, err := database.Open(ctx, cfg.Database, logger)
	if err != nil {
		return nil, err
	}

	// AutoMigrate on boot is convenient locally and dangerous in production,
	// so it is opt-in and defaults off.
	if cfg.Database.AutoMigrate {
		if err := database.Migrate(ctx, db, logger); err != nil {
			return nil, fmt.Errorf("boot migration: %w", err)
		}
	}

	redisCache, err := cache.Connect(ctx, cfg.Redis, logger)
	if err != nil {
		return nil, err
	}

	app := &App{
		Config: cfg,
		Logger: logger,
		DB:     db,
		Cache:  redisCache,
		Jobs:   jobs.NewClient(redisCache.Client(), logger),
		Repos:  repository.New(db),
	}

	// --- optional integrations ---

	if storageClient, err := storage.New(ctx, cfg.Storage, logger); err != nil {
		if errors.Is(err, storage.ErrNotConfigured) {
			logger.Warn("object storage disabled: R2 credentials are not set")
		} else {
			return nil, err
		}
	} else {
		app.Storage = storageClient
	}

	if agoraService, err := agora.New(cfg.Agora); err != nil {
		logger.Warn("realtime video disabled: Agora credentials are not set")
	} else {
		app.Agora = agoraService
	}

	if cfg.Envio.GraphQLURL != "" {
		app.Envio = envio.New(cfg.Envio, logger)
	} else {
		logger.Warn("onchain indexing disabled: ENVIO_GRAPHQL_URL is not set")
	}

	if !opts.SkipAuth {
		verifier, err := auth.NewVerifier(ctx, cfg.Auth, logger)
		switch {
		case errors.Is(err, auth.ErrNotConfigured):
			logger.Warn("authentication disabled: DYNAMIC_ENVIRONMENT_ID is not set")
		case err != nil:
			// A configured-but-unreachable JWKS is a genuine boot failure: the
			// API would reject every authenticated request.
			return nil, err
		default:
			app.Verifier = verifier
		}
	}

	app.Services = service.New(service.Deps{
		Config:  cfg,
		Logger:  logger,
		Repos:   app.Repos,
		Cache:   app.Cache,
		Jobs:    app.Jobs,
		Storage: app.Storage,
		Agora:   app.Agora,
		Envio:   app.Envio,
	})

	return app, nil
}

// Close releases every resource in reverse dependency order. It collects all
// errors rather than stopping at the first, so one bad close cannot leak the
// rest.
func (a *App) Close() error {
	var errs []error

	if a.Jobs != nil {
		if err := a.Jobs.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close job client: %w", err))
		}
	}
	if a.Cache != nil {
		if err := a.Cache.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close redis: %w", err))
		}
	}
	if a.DB != nil {
		if err := database.Close(a.DB); err != nil {
			errs = append(errs, fmt.Errorf("close postgres: %w", err))
		}
	}

	return errors.Join(errs...)
}
