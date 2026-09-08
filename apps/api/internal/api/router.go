// Package api assembles the HTTP router: middleware stack, route table and the
// handler wiring.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"gorm.io/gorm"

	"github.com/meterrail/api/internal/api/handlers"
	"github.com/meterrail/api/internal/api/middleware"
	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/config"
	"github.com/meterrail/api/internal/envio"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/service"
	"github.com/meterrail/api/internal/storage"
)

// Deps is everything the router needs to build the handler graph.
type Deps struct {
	Config   *config.Config
	Logger   *slog.Logger
	DB       *gorm.DB
	Cache    *cache.Cache
	Jobs     *jobs.Client
	Storage  *storage.Client
	Envio    *envio.Client
	Services *service.Services
	Verifier *auth.Verifier
	Version  string
	Commit   string
}

// NewRouter builds the full route table.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	authenticator := &middleware.Authenticator{
		Verifier:     deps.Verifier,
		Users:        deps.Services.Users,
		ServiceToken: deps.Config.Auth.ServiceToken,
		Logger:       deps.Logger,
	}

	// Order matters: request id first so every later log line carries it,
	// recover before anything that can panic, and the access log outermost of
	// the handler-facing middleware so it records the final status.
	r.Use(middleware.RequestID(deps.Logger))
	r.Use(middleware.Recover)
	r.Use(middleware.AccessLog)
	r.Use(middleware.SecurityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.HTTP.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-Id", "X-Service-Token"},
		ExposedHeaders:   []string{"X-Request-Id", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.Timeout(deps.Config.HTTP.WriteTimeout - time.Second))

	health := &handlers.HealthHandler{
		DB:      deps.DB,
		Cache:   deps.Cache,
		Jobs:    deps.Jobs,
		Storage: deps.Storage,
		Envio:   deps.Envio,
		Version: deps.Version,
		Commit:  deps.Commit,
	}

	userHandler := &handlers.UserHandler{Users: deps.Services.Users}
	mediaHandler := &handlers.MediaHandler{Media: deps.Services.Media}
	sessionHandler := &handlers.SessionHandler{Sessions: deps.Services.Sessions}
	onchainHandler := &handlers.OnchainHandler{Onchain: deps.Services.Onchain, Jobs: deps.Jobs}
	notificationHandler := &handlers.NotificationHandler{Notifications: deps.Services.Notifications}

	// Probes sit outside /v1 and outside the rate limiter.
	r.Get("/health", health.Live)
	r.Get("/health/live", health.Live)
	r.Get("/health/ready", health.Ready)

	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.RateLimit(deps.Cache, deps.Config.HTTP.RateLimitPerMin, deps.Config.HTTP.TrustedProxies))

		// Attach an identity whenever a token is present, so public reads can
		// still be personalised. Routes that require a caller add
		// authenticator.Required on top, which reuses the identity this
		// already resolved rather than verifying the token twice.
		r.Use(authenticator.Optional)

		// Note on structure: every path prefix is declared exactly once.
		// Splitting a prefix like /sessions across two chi.Group blocks would
		// mount a second subrouter at the same pattern and silently shadow the
		// first, so public and authenticated routes are nested instead.

		r.Route("/me", func(r chi.Router) {
			r.Use(authenticator.Required)

			r.Get("/", userHandler.Me)
			r.Patch("/", userHandler.UpdateMe)
			r.Get("/organizations", userHandler.MyOrganizations)
		})

		r.Route("/users", func(r chi.Router) {
			r.Use(authenticator.Required)

			r.Get("/", userHandler.List)
			r.Get("/{userID}", userHandler.Get)
		})

		r.Route("/media", func(r chi.Router) {
			r.Use(authenticator.Required)

			r.Get("/", mediaHandler.List)
			r.Post("/uploads", mediaHandler.RequestUpload)
			r.Post("/{assetID}/confirm", mediaHandler.Confirm)
			r.Get("/{assetID}", mediaHandler.Get)
			r.Get("/{assetID}/download-url", mediaHandler.DownloadURL)
			r.Delete("/{assetID}", mediaHandler.Delete)
		})

		r.Route("/sessions", func(r chi.Router) {
			// Public: browsing the schedule needs no wallet.
			r.Get("/", sessionHandler.List)
			r.Get("/{sessionID}", sessionHandler.Get)

			// Everything that mints a token or mutates state does.
			r.Group(func(r chi.Router) {
				r.Use(authenticator.Required)

				r.Post("/", sessionHandler.Create)
				r.Post("/{sessionID}/join", sessionHandler.Join)
				r.Post("/{sessionID}/token", sessionHandler.RenewToken)
				r.Post("/{sessionID}/leave", sessionHandler.Leave)
				r.Post("/{sessionID}/start", sessionHandler.Start)
				r.Post("/{sessionID}/end", sessionHandler.End)
			})
		})

		// Onchain data is public: it is already public on the chain.
		r.Route("/onchain", func(r chi.Router) {
			r.Get("/events", onchainHandler.List)
			r.Get("/status", onchainHandler.Status)
		})

		r.Route("/notifications", func(r chi.Router) {
			r.Use(authenticator.Required)

			r.Get("/", notificationHandler.List)
			r.Get("/unread-count", notificationHandler.UnreadCount)
			r.Post("/read-all", notificationHandler.MarkAllRead)
			r.Post("/{notificationID}/read", notificationHandler.MarkRead)
		})

		// Machine-to-machine routes, gated by the shared service token.
		r.Route("/internal", func(r chi.Router) {
			r.Use(authenticator.ServiceOnly)

			r.Get("/queues", health.Queues)
			r.Post("/onchain/sync", onchainHandler.TriggerSync)
			r.Post("/notifications/dispatch", notificationHandler.Dispatch)
		})
	})

	// Typed 404/405 so clients get the same envelope everywhere.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, httpx.NotFound("endpoint"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, &httpx.APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "that method is not allowed on this endpoint",
		})
	})

	return r
}
