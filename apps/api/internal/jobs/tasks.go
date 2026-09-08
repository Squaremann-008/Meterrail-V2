// Package jobs defines the asynq task types, the enqueue client, the worker
// handlers and the periodic (cron) schedule. Asynqmon is pointed at the same
// Redis instance for the web dashboard.
package jobs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// Queue names, highest priority first. The weights live in WORKER_QUEUES.
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

// Task type identifiers. These strings are the wire contract: renaming one
// strands the payloads already sitting in Redis.
const (
	TypeMediaProcess     = "media:process"
	TypeMediaReapOrphans = "media:reap_orphans"
	TypeNotificationSend = "notification:send"
	TypeSessionStart     = "session:start"
	TypeSessionReconcile = "session:reconcile"
	TypeIndexerSync      = "indexer:sync"
	TypeCacheWarm        = "cache:warm"
	TypeAuditPrune       = "audit:prune"
)

// MediaProcessPayload asks the worker to fetch a freshly uploaded object from
// R2, derive thumbnails and flip the row to ready.
type MediaProcessPayload struct {
	MediaAssetID       uuid.UUID `json:"mediaAssetId"`
	Key                string    `json:"key"`
	GenerateThumbnails bool      `json:"generateThumbnails"`
}

type NotificationSendPayload struct {
	UserID  uuid.UUID      `json:"userId"`
	Kind    string         `json:"kind"`
	Title   string         `json:"title"`
	Body    string         `json:"body,omitempty"`
	Channel string         `json:"channel,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type SessionPayload struct {
	SessionID uuid.UUID `json:"sessionId"`
}

type IndexerSyncPayload struct {
	ChainID   int64 `json:"chainId"`
	BatchSize int   `json:"batchSize,omitempty"`
}

type ReapOrphansPayload struct {
	OlderThan time.Duration `json:"olderThan"`
}

type CacheWarmPayload struct {
	Scope string `json:"scope"`
}

type AuditPrunePayload struct {
	RetainDays int `json:"retainDays"`
}

// newTask marshals a payload and applies per-type options.
func newTask(typeName string, payload any, opts ...asynq.Option) (*asynq.Task, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jobs: encode %s payload: %w", typeName, err)
	}
	return asynq.NewTask(typeName, raw, opts...), nil
}

// NewMediaProcessTask processes an uploaded asset. Retried aggressively because
// a failure leaves the user staring at a spinner.
func NewMediaProcessTask(p MediaProcessPayload) (*asynq.Task, error) {
	return newTask(TypeMediaProcess, p,
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(5),
		asynq.Timeout(5*time.Minute),
		// Deduplicate: re-confirming the same upload must not re-process it.
		asynq.TaskID("media-process-"+p.MediaAssetID.String()),
		asynq.Retention(24*time.Hour),
	)
}

func NewNotificationTask(p NotificationSendPayload) (*asynq.Task, error) {
	return newTask(TypeNotificationSend, p,
		asynq.Queue(QueueCritical),
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
	)
}

func NewSessionStartTask(p SessionPayload, at time.Time) (*asynq.Task, error) {
	return newTask(TypeSessionStart, p,
		asynq.Queue(QueueCritical),
		asynq.MaxRetry(3),
		asynq.ProcessAt(at),
		asynq.TaskID("session-start-"+p.SessionID.String()),
	)
}

func NewSessionReconcileTask(p SessionPayload) (*asynq.Task, error) {
	return newTask(TypeSessionReconcile, p, asynq.Queue(QueueLow), asynq.MaxRetry(2))
}

func NewIndexerSyncTask(p IndexerSyncPayload) (*asynq.Task, error) {
	return newTask(TypeIndexerSync, p,
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(3),
		asynq.Timeout(2*time.Minute),
	)
}

func NewReapOrphansTask(p ReapOrphansPayload) (*asynq.Task, error) {
	return newTask(TypeMediaReapOrphans, p, asynq.Queue(QueueLow), asynq.MaxRetry(1))
}

func NewCacheWarmTask(p CacheWarmPayload) (*asynq.Task, error) {
	return newTask(TypeCacheWarm, p, asynq.Queue(QueueLow), asynq.MaxRetry(1))
}

func NewAuditPruneTask(p AuditPrunePayload) (*asynq.Task, error) {
	return newTask(TypeAuditPrune, p, asynq.Queue(QueueLow), asynq.MaxRetry(1))
}
