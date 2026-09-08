// Command worker runs the asynq background processor and the cron scheduler.
// Both live in one process so a single deploy unit owns all async work; set
// -scheduler=false to run extra pure-worker replicas.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/hibiken/asynq"

	"github.com/meterrail/api/internal/app"
	"github.com/meterrail/api/internal/config"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/worker"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	withScheduler := flag.Bool("scheduler", true, "run the periodic task scheduler in this process")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// The worker serves no HTTP requests, so it never verifies user JWTs.
	application, err := app.New(ctx, cfg, app.Options{
		ServiceName: cfg.App.Name + "-worker",
		SkipAuth:    true,
	})
	if err != nil {
		return fmt.Errorf("boot application: %w", err)
	}
	defer func() {
		if err := application.Close(); err != nil {
			application.Logger.Error("shutdown cleanup failed", slog.String("error", err.Error()))
		}
	}()

	handlers := &worker.Handlers{
		Repos:    application.Repos,
		Services: application.Services,
		Storage:  application.Storage,
		Logger:   application.Logger,
	}

	mux := asynq.NewServeMux()
	// Recover inside the worker too: a panic in one task must not take down
	// the whole processor and abandon the other in-flight jobs.
	mux.Use(recoverMiddleware(application.Logger))
	handlers.Register(mux)

	server := jobs.NewServer(
		application.Cache.Client(),
		cfg.Worker.Concurrency,
		cfg.Worker.Queues,
		cfg.Worker.ShutdownTimeout,
		application.Logger,
	)

	if *withScheduler {
		chainIDs := parseChainIDs(os.Getenv("ENVIO_CHAIN_IDS"))
		schedule := jobs.PeriodicSchedule(chainIDs, cfg.Envio.SyncInterval)

		scheduler, err := jobs.NewScheduler(application.Cache.Client(), schedule, application.Logger)
		if err != nil {
			return fmt.Errorf("build scheduler: %w", err)
		}
		if err := scheduler.Start(); err != nil {
			return fmt.Errorf("start scheduler: %w", err)
		}
		defer scheduler.Shutdown()

		application.Logger.Info("scheduler running", slog.Int("entries", len(schedule)))
	}

	if err := server.Start(mux); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}

	application.Logger.Info("worker running",
		slog.Int("concurrency", cfg.Worker.Concurrency),
		slog.Any("queues", cfg.Worker.Queues),
		slog.String("version", version),
		slog.String("commit", commit),
	)

	<-ctx.Done()
	application.Logger.Info("shutdown signal received, finishing in-flight tasks",
		slog.Duration("grace_period", cfg.Worker.ShutdownTimeout))

	// Stop pulls the worker off the queues but lets active tasks finish;
	// Shutdown then waits for them, up to ShutdownTimeout.
	server.Stop()
	server.Shutdown()

	application.Logger.Info("worker stopped cleanly")
	return nil
}

// recoverMiddleware converts a panicking handler into a failed task so asynq
// can retry it under the normal policy.
func recoverMiddleware(logger *slog.Logger) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("task panicked",
						slog.String("type", task.Type()),
						slog.Any("panic", recovered),
					)
					err = fmt.Errorf("task %s panicked: %v", task.Type(), recovered)
				}
			}()
			return next.ProcessTask(ctx, task)
		})
	}
}

// parseChainIDs reads the comma-separated chain list the indexer covers,
// defaulting to Ethereum mainnet so a fresh checkout has a working cron entry.
func parseChainIDs(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return []int64{1}
	}

	var ids []int64
	for _, part := range strings.Split(raw, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []int64{1}
	}
	return ids
}
