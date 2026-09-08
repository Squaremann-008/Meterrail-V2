package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// PeriodicTask is one cron entry. Keeping the schedule in code rather than in
// Redis means a deploy is the only way it changes, and it is reviewable.
type PeriodicTask struct {
	// Cronspec accepts standard cron syntax plus the @every shorthand.
	Cronspec string
	Type     string
	Payload  any
	Queue    string
	Timeout  time.Duration
}

// PeriodicSchedule is the full cron table for the worker.
func PeriodicSchedule(chainIDs []int64, envioInterval time.Duration) []PeriodicTask {
	tasks := make([]PeriodicTask, 0, 3+len(chainIDs))
	tasks = append(tasks, []PeriodicTask{
		{
			// Clear presigned uploads the client never completed.
			Cronspec: "@every 1h",
			Type:     TypeMediaReapOrphans,
			Payload:  ReapOrphansPayload{OlderThan: 24 * time.Hour},
			Queue:    QueueLow,
			Timeout:  10 * time.Minute,
		},
		{
			// Keep the operator dashboard's cached figures fresh.
			Cronspec: "@every 5m",
			Type:     TypeCacheWarm,
			Payload:  CacheWarmPayload{Scope: "onchain"},
			Queue:    QueueLow,
			Timeout:  time.Minute,
		},
		{
			// Retention sweep, off-peak.
			Cronspec: "0 4 * * *",
			Type:     TypeAuditPrune,
			Payload:  AuditPrunePayload{RetainDays: 90},
			Queue:    QueueLow,
			Timeout:  15 * time.Minute,
		},
	}...)

	// One sync entry per configured chain, so a slow chain cannot stall the
	// others behind it in a single task.
	for _, chainID := range chainIDs {
		tasks = append(tasks, PeriodicTask{
			Cronspec: fmt.Sprintf("@every %s", envioInterval),
			Type:     TypeIndexerSync,
			Payload:  IndexerSyncPayload{ChainID: chainID},
			Queue:    QueueDefault,
			Timeout:  2 * time.Minute,
		})
	}

	return tasks
}

// NewScheduler builds the asynq scheduler and registers the cron table. Asynq's
// scheduler elects a single active instance across replicas, so running the
// worker with --replicas > 1 does not double-fire.
func NewScheduler(rdb *redis.Client, tasks []PeriodicTask, logger *slog.Logger) (*asynq.Scheduler, error) {
	scheduler := asynq.NewScheduler(redisOptions(rdb), &asynq.SchedulerOpts{
		Location: time.UTC,
		PostEnqueueFunc: func(info *asynq.TaskInfo, err error) {
			if err != nil {
				logger.Error("periodic enqueue failed",
					slog.String("type", info.Type), slog.String("error", err.Error()))
				return
			}
			logger.Debug("periodic task enqueued",
				slog.String("type", info.Type), slog.String("id", info.ID))
		},
	})

	for _, periodic := range tasks {
		payload, err := json.Marshal(periodic.Payload)
		if err != nil {
			return nil, fmt.Errorf("scheduler: encode %s payload: %w", periodic.Type, err)
		}

		entryID, err := scheduler.Register(
			periodic.Cronspec,
			asynq.NewTask(periodic.Type, payload),
			asynq.Queue(periodic.Queue),
			asynq.Timeout(periodic.Timeout),
		)
		if err != nil {
			return nil, fmt.Errorf("scheduler: register %s (%s): %w", periodic.Type, periodic.Cronspec, err)
		}

		logger.Info("periodic task registered",
			slog.String("entry_id", entryID),
			slog.String("type", periodic.Type),
			slog.String("cron", periodic.Cronspec),
		)
	}

	return scheduler, nil
}

// NewServer builds the worker server with the configured queue weights.
func NewServer(rdb *redis.Client, concurrency int, queues map[string]int, shutdownTimeout time.Duration, logger *slog.Logger) *asynq.Server {
	if len(queues) == 0 {
		queues = map[string]int{QueueCritical: 6, QueueDefault: 3, QueueLow: 1}
	}

	return asynq.NewServer(redisOptions(rdb), asynq.Config{
		Concurrency:     concurrency,
		Queues:          queues,
		ShutdownTimeout: shutdownTimeout,
		StrictPriority:  false,
		Logger:          &asynqLogger{logger: logger},
		// Exponential backoff with a ceiling, so a dependency outage does not
		// hammer the failing service.
		RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration {
			delay := time.Duration(1<<uint(n)) * time.Second
			if delay > 10*time.Minute {
				return 10 * time.Minute
			}
			return delay
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			retried, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)

			attrs := []any{
				slog.String("type", task.Type()),
				slog.Int("retry", retried),
				slog.Int("max_retry", maxRetry),
				slog.String("error", err.Error()),
			}
			if retried >= maxRetry {
				logger.Error("task exhausted retries, archiving", attrs...)
			} else {
				logger.Warn("task failed, will retry", attrs...)
			}
		}),
	})
}

// asynqLogger adapts slog to asynq's logger interface.
type asynqLogger struct{ logger *slog.Logger }

func (l *asynqLogger) Debug(args ...any) { l.logger.Debug(fmt.Sprint(args...)) }
func (l *asynqLogger) Info(args ...any)  { l.logger.Info(fmt.Sprint(args...)) }
func (l *asynqLogger) Warn(args ...any)  { l.logger.Warn(fmt.Sprint(args...)) }
func (l *asynqLogger) Error(args ...any) { l.logger.Error(fmt.Sprint(args...)) }
func (l *asynqLogger) Fatal(args ...any) { l.logger.Error(fmt.Sprint(args...)) }
