// Command server runs the Meterrail HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/meterrail/api/internal/api"
	"github.com/meterrail/api/internal/app"
	"github.com/meterrail/api/internal/config"
)

// Build metadata, injected at link time by the Makefile.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet if config failed, so use the default.
		slog.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	// Cancel on SIGINT/SIGTERM so shutdown starts the moment the orchestrator
	// asks, not when the current request finishes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	application, err := app.New(ctx, cfg, app.Options{ServiceName: cfg.App.Name + "-server"})
	if err != nil {
		return fmt.Errorf("boot application: %w", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			application.Logger.Error("shutdown cleanup failed", slog.String("error", err.Error()))
		}
	}()

	handler := api.NewRouter(api.Deps{
		Config:   cfg,
		Logger:   application.Logger,
		DB:       application.DB,
		Cache:    application.Cache,
		Jobs:     application.Jobs,
		Storage:  application.Storage,
		Envio:    application.Envio,
		Services: application.Services,
		Verifier: application.Verifier,
		Version:  version,
		Commit:   commit,
	})

	server := &http.Server{
		Addr:              cfg.HTTP.Addr(),
		Handler:           handler,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	// ListenAndServe blocks, so it runs on its own goroutine and reports back
	// through a channel; the main goroutine waits on either it or a signal.
	serverErr := make(chan error, 1)
	go func() {
		application.Logger.Info("http server listening",
			slog.String("addr", cfg.HTTP.Addr()),
			slog.String("version", version),
			slog.String("commit", commit),
			slog.String("env", cfg.App.Environment),
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("listen on %s: %w", cfg.HTTP.Addr(), err)
		}
		return nil
	case <-ctx.Done():
		application.Logger.Info("shutdown signal received, draining connections",
			slog.Duration("grace_period", cfg.HTTP.ShutdownTimeout))
	}

	// A fresh context: the signal already cancelled the original one.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		// In-flight requests outlived the grace period; drop them rather than
		// hanging the deploy.
		application.Logger.Error("graceful shutdown timed out, forcing close",
			slog.String("error", err.Error()))
		return server.Close()
	}

	application.Logger.Info("server stopped cleanly")
	return nil
}
