package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// Client enqueues work. Handlers and HTTP handlers share one instance.
type Client struct {
	asynq     *asynq.Client
	inspector *asynq.Inspector
	logger    *slog.Logger
}

// NewClient reuses the parsed options from the existing Redis client so the
// queue and the cache always agree on which instance they are talking to.
func NewClient(rdb *redis.Client, logger *slog.Logger) *Client {
	opt := redisOptions(rdb)
	return &Client{
		asynq:     asynq.NewClient(opt),
		inspector: asynq.NewInspector(opt),
		logger:    logger,
	}
}

// redisOptions projects go-redis options onto asynq's connection type.
func redisOptions(rdb *redis.Client) asynq.RedisClientOpt {
	o := rdb.Options()
	return asynq.RedisClientOpt{
		Addr:      o.Addr,
		Username:  o.Username,
		Password:  o.Password,
		DB:        o.DB,
		TLSConfig: o.TLSConfig,
	}
}

// Enqueue submits a task, tolerating the duplicate error that TaskID-based
// deduplication produces on purpose.
func (c *Client) Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) error {
	info, err := c.asynq.EnqueueContext(ctx, task, opts...)
	if err != nil {
		if isDuplicate(err) {
			c.logger.Debug("task already queued", slog.String("type", task.Type()))
			return nil
		}
		return fmt.Errorf("jobs: enqueue %s: %w", task.Type(), err)
	}
	c.logger.Debug("task enqueued",
		slog.String("type", task.Type()),
		slog.String("id", info.ID),
		slog.String("queue", info.Queue),
	)
	return nil
}

// isDuplicate recognises the two errors that TaskID-based deduplication
// raises on purpose: an identical task is already queued or still retained.
func isDuplicate(err error) bool {
	return errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask)
}

// Stats summarizes queue depth for the health endpoint.
type Stats struct {
	Queue     string `json:"queue"`
	Size      int    `json:"size"`
	Active    int    `json:"active"`
	Pending   int    `json:"pending"`
	Scheduled int    `json:"scheduled"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Completed int    `json:"completed"`
	Processed int    `json:"processed"`
	Failed    int    `json:"failed"`
}

// QueueStats reads live counters straight out of Redis.
// The asynq Inspector API is synchronous and takes no context of its own.
func (c *Client) QueueStats(_ context.Context) ([]Stats, error) {
	queues, err := c.inspector.Queues()
	if err != nil {
		return nil, fmt.Errorf("jobs: list queues: %w", err)
	}

	out := make([]Stats, 0, len(queues))
	for _, name := range queues {
		info, err := c.inspector.GetQueueInfo(name)
		if err != nil {
			c.logger.Warn("queue info failed", slog.String("queue", name), slog.String("error", err.Error()))
			continue
		}
		out = append(out, Stats{
			Queue:     info.Queue,
			Size:      info.Size,
			Active:    info.Active,
			Pending:   info.Pending,
			Scheduled: info.Scheduled,
			Retry:     info.Retry,
			Archived:  info.Archived,
			Completed: info.Completed,
			Processed: info.Processed,
			Failed:    info.Failed,
		})
	}
	return out, nil
}

func (c *Client) Close() error {
	if err := c.inspector.Close(); err != nil {
		return err
	}
	return c.asynq.Close()
}
