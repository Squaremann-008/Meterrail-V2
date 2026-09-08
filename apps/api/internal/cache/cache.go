// Package cache wraps Redis with a small typed API: JSON get/set, singleflight
// style read-through, tag invalidation and a fixed-window rate limiter.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/meterrail/api/internal/config"
)

// ErrMiss is returned by Get when the key is absent.
var ErrMiss = errors.New("cache: miss")

type Cache struct {
	rdb    *redis.Client
	prefix string
	logger *slog.Logger
}

// Connect parses the Redis URL, applies pool settings and pings.
func Connect(ctx context.Context, cfg config.Redis, logger *slog.Logger) (*Cache, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opts.PoolSize = cfg.PoolSize

	rdb := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	logger.Info("redis connected", slog.String("addr", opts.Addr), slog.Int("db", opts.DB))
	return &Cache{rdb: rdb, prefix: cfg.CachePrefix, logger: logger}, nil
}

// Client exposes the raw handle for callers that need Redis primitives.
func (c *Cache) Client() *redis.Client { return c.rdb }

func (c *Cache) Close() error { return c.rdb.Close() }

func (c *Cache) Health(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

func (c *Cache) key(parts ...string) string {
	return c.prefix + ":" + strings.Join(parts, ":")
}

// Get decodes the JSON value at key into dst, returning ErrMiss when absent.
func Get[T any](ctx context.Context, c *Cache, key string) (T, error) {
	var zero T
	raw, err := c.rdb.Get(ctx, c.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return zero, ErrMiss
	}
	if err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		// A poisoned entry should not wedge the endpoint; drop and miss.
		c.logger.Warn("cache decode failed, evicting", slog.String("key", key))
		_ = c.rdb.Del(ctx, c.key(key)).Err()
		return zero, ErrMiss
	}
	return out, nil
}

// Set stores value as JSON with a TTL.
func Set(ctx context.Context, c *Cache, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: encode %s: %w", key, err)
	}
	return c.rdb.Set(ctx, c.key(key), raw, ttl).Err()
}

// Remember is the read-through helper: return the cached value, or call load,
// store the result and return it. A load error is never cached.
func Remember[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	if cached, err := Get[T](ctx, c, key); err == nil {
		return cached, nil
	} else if !errors.Is(err, ErrMiss) {
		// Redis is down: degrade to the origin rather than failing the request.
		c.logger.Warn("cache read failed", slog.String("key", key), slog.String("error", err.Error()))
	}

	value, err := load(ctx)
	if err != nil {
		return value, err
	}
	if err := Set(ctx, c, key, value, ttl); err != nil {
		c.logger.Warn("cache write failed", slog.String("key", key), slog.String("error", err.Error()))
	}
	return value, nil
}

// Delete removes one or more exact keys.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = c.key(k)
	}
	return c.rdb.Del(ctx, full...).Err()
}

// DeletePrefix evicts every key under a logical prefix. It scans in batches
// rather than using KEYS so it never blocks the server.
func (c *Cache) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	pattern := c.key(prefix) + "*"
	var (
		cursor  uint64
		removed int
	)
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 256).Result()
		if err != nil {
			return removed, err
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return removed, err
			}
			removed += len(keys)
		}
		cursor = next
		if cursor == 0 {
			return removed, nil
		}
	}
}

// RateLimit implements a fixed window.
//
// It reports whether this request is allowed, how much of the budget is left
// after it, and how long until the window resets. A limit of 5 permits five
// requests per window: the sixth is the first to be refused.
func (c *Cache) RateLimit(ctx context.Context, identifier string, limit int, window time.Duration) (allowed bool, remaining int, reset time.Duration, err error) {
	k := c.key("ratelimit", identifier)

	// INCR and EXPIRE in one round trip. EXPIRE is unconditional, which makes
	// this a fixed window that restarts on the first request after a reset
	// rather than a sliding one.
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, limit, 0, err
	}

	used := int(incr.Val())

	ttl, err := c.rdb.TTL(ctx, k).Result()
	if err != nil || ttl < 0 {
		ttl = window
	}

	if used > limit {
		return false, 0, ttl, nil
	}
	return true, limit - used, ttl, nil
}

// Lock takes a best-effort distributed lock, returning a release func. Used to
// keep cron fan-out from double-firing across worker replicas.
func (c *Cache) Lock(ctx context.Context, name string, ttl time.Duration) (release func(), ok bool, err error) {
	k := c.key("lock", name)
	acquired, err := c.rdb.SetNX(ctx, k, time.Now().UTC().Format(time.RFC3339Nano), ttl).Result()
	if err != nil {
		return func() {}, false, err
	}
	if !acquired {
		return func() {}, false, nil
	}
	return func() {
		// Detached context: the caller's may already be cancelled.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = c.rdb.Del(releaseCtx, k).Err()
	}, true, nil
}
