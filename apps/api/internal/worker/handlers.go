// Package worker holds the asynq task handlers: the code that actually runs
// each background job. It lives apart from the jobs package so that services
// can enqueue work without importing the handlers that consume it.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registers the GIF decoder
	"image/jpeg"
	_ "image/png" // registers the PNG decoder
	"io"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"golang.org/x/image/draw"

	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
	"github.com/meterrail/api/internal/service"
	"github.com/meterrail/api/internal/storage"
)

// thumbnailWidths are the derivative sizes generated for every image upload.
var thumbnailWidths = []int{320, 640, 1280}

// Handlers owns the worker-side dependencies. It is deliberately separate from
// the HTTP services: a worker process runs this and never builds a router.
type Handlers struct {
	Repos    *repository.Repositories
	Services *service.Services
	Storage  *storage.Client
	Logger   *slog.Logger
}

// Register maps every task type onto its handler.
func (h *Handlers) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(jobs.TypeMediaProcess, h.handleMediaProcess)
	mux.HandleFunc(jobs.TypeMediaReapOrphans, h.handleReapOrphans)
	mux.HandleFunc(jobs.TypeNotificationSend, h.handleNotificationSend)
	mux.HandleFunc(jobs.TypeSessionStart, h.handleSessionStart)
	mux.HandleFunc(jobs.TypeSessionReconcile, h.handleSessionReconcile)
	mux.HandleFunc(jobs.TypeIndexerSync, h.handleIndexerSync)
	mux.HandleFunc(jobs.TypeCacheWarm, h.handleCacheWarm)
	mux.HandleFunc(jobs.TypeAuditPrune, h.handleAuditPrune)
}

// decode unmarshals a task payload, wrapping failures in asynq.SkipRetry: a
// payload that will not parse now will not parse on retry either.
func decode(task *asynq.Task, dst any) error {
	if err := json.Unmarshal(task.Payload(), dst); err != nil {
		return fmt.Errorf("%s: decode payload: %w: %w", task.Type(), err, asynq.SkipRetry)
	}
	return nil
}

// handleMediaProcess downloads an uploaded image from R2, derives thumbnails,
// writes them back and flips the asset to ready.
func (h *Handlers) handleMediaProcess(ctx context.Context, task *asynq.Task) error {
	var p jobs.MediaProcessPayload
	if err := decode(task, &p); err != nil {
		return err
	}

	asset, err := h.Repos.Media.ByID(ctx, p.MediaAssetID)
	if errors.Is(err, repository.ErrNotFound) {
		// The asset was deleted between enqueue and execution. Nothing to do.
		h.Logger.Info("media asset gone, skipping", slog.String("asset_id", p.MediaAssetID.String()))
		return nil
	}
	if err != nil {
		return fmt.Errorf("load media asset: %w", err)
	}
	if asset.Status == models.MediaReady {
		return nil // already processed
	}

	if err := h.Repos.Media.SetStatus(ctx, asset.ID, models.MediaProcessing); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	// Non-images need no derivatives; mark them ready as-is.
	if !p.GenerateThumbnails || h.Storage == nil {
		return h.Repos.Media.Finalize(ctx, asset.ID, 0, 0, nil)
	}

	width, height, variants, err := h.generateThumbnails(ctx, asset)
	if err != nil {
		if statusErr := h.Repos.Media.SetStatus(ctx, asset.ID, models.MediaFailed); statusErr != nil {
			h.Logger.Error("mark failed", slog.String("error", statusErr.Error()))
		}
		return fmt.Errorf("generate thumbnails for %s: %w", asset.Key, err)
	}

	if err := h.Repos.Media.Finalize(ctx, asset.ID, width, height, variants); err != nil {
		return fmt.Errorf("finalize asset: %w", err)
	}

	h.Logger.Info("media processed",
		slog.String("asset_id", asset.ID.String()),
		slog.Int("variants", len(variants)),
	)
	return nil
}

// generateThumbnails decodes the original once and rescales it per width.
func (h *Handlers) generateThumbnails(
	ctx context.Context,
	asset *models.MediaAsset,
) (width, height int, variants models.JSONMap, err error) {
	body, _, err := h.Storage.Get(ctx, asset.Key)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("fetch original: %w", err)
	}
	defer func() { _ = body.Close() }()

	// Cap the read so a hostile object cannot exhaust worker memory.
	raw, err := io.ReadAll(io.LimitReader(body, h.Storage.MaxUploadBytes()+1))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read original: %w", err)
	}

	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := source.Bounds()
	originalWidth, originalHeight := bounds.Dx(), bounds.Dy()
	variants = models.JSONMap{}

	for _, targetWidth := range thumbnailWidths {
		// Never upscale: a 200px avatar has no business becoming 1280px.
		if targetWidth >= originalWidth {
			continue
		}
		targetHeight := originalHeight * targetWidth / originalWidth

		canvas := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.CatmullRom.Scale(canvas, canvas.Bounds(), source, bounds, draw.Over, nil)

		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 82}); err != nil {
			return 0, 0, nil, fmt.Errorf("encode %dpx variant: %w", targetWidth, err)
		}

		variantKey := fmt.Sprintf("%s.w%d.jpg", asset.Key, targetWidth)
		if err := h.Storage.Put(ctx, variantKey, "image/jpeg", bytes.NewReader(encoded.Bytes())); err != nil {
			return 0, 0, nil, fmt.Errorf("upload %dpx variant: %w", targetWidth, err)
		}

		variants[fmt.Sprintf("w%d", targetWidth)] = map[string]any{
			"key":       variantKey,
			"width":     targetWidth,
			"height":    targetHeight,
			"url":       h.Storage.PublicURL(variantKey),
			"sizeBytes": encoded.Len(),
		}
	}

	return originalWidth, originalHeight, variants, nil
}

// handleReapOrphans deletes rows for presigned uploads the client abandoned,
// plus any object that did land but was never confirmed.
func (h *Handlers) handleReapOrphans(ctx context.Context, task *asynq.Task) error {
	var p jobs.ReapOrphansPayload
	if err := decode(task, &p); err != nil {
		return err
	}
	if p.OlderThan <= 0 {
		p.OlderThan = 24 * time.Hour
	}

	stale, err := h.Repos.Media.PendingOlderThan(ctx, p.OlderThan, 500)
	if err != nil {
		return fmt.Errorf("list stale uploads: %w", err)
	}

	reaped := 0
	for i := range stale {
		asset := &stale[i]
		if h.Storage != nil {
			// The object usually does not exist; deleting a missing key is a
			// no-op in S3, so this needs no existence check.
			if err := h.Storage.Delete(ctx, asset.Key); err != nil {
				h.Logger.Warn("reap object failed",
					slog.String("key", asset.Key), slog.String("error", err.Error()))
				continue
			}
		}
		if err := h.Repos.Media.HardDelete(ctx, asset.ID); err != nil {
			h.Logger.Warn("reap row failed", slog.String("asset_id", asset.ID.String()))
			continue
		}
		reaped++
	}

	if reaped > 0 {
		h.Logger.Info("orphaned uploads reaped", slog.Int("count", reaped))
	}
	return nil
}

// handleNotificationSend persists the notification and marks it delivered. The
// email/webhook transports plug in here.
func (h *Handlers) handleNotificationSend(ctx context.Context, task *asynq.Task) error {
	var p jobs.NotificationSendPayload
	if err := decode(task, &p); err != nil {
		return err
	}

	channel := models.NotificationChannel(p.Channel)
	if channel == "" {
		channel = models.NotifyInApp
	}

	notification := &models.Notification{
		UserID:  p.UserID,
		Channel: channel,
		Kind:    p.Kind,
		Title:   p.Title,
		Body:    p.Body,
		Data:    models.JSONMap(p.Data),
	}
	if err := h.Repos.Notifications.Create(ctx, notification); err != nil {
		return fmt.Errorf("persist notification: %w", err)
	}

	// In-app notifications are delivered the moment the row exists. Email and
	// webhook transports would dispatch here before marking delivered.
	if channel == models.NotifyInApp {
		if err := h.Repos.Notifications.MarkDelivered(ctx, notification.ID); err != nil {
			return fmt.Errorf("mark delivered: %w", err)
		}
	}

	h.Logger.Info("notification queued",
		slog.String("user_id", p.UserID.String()), slog.String("kind", p.Kind))
	return nil
}

// handleSessionStart flips a scheduled session live at its appointed time and
// tells the host it has begun.
func (h *Handlers) handleSessionStart(ctx context.Context, task *asynq.Task) error {
	var p jobs.SessionPayload
	if err := decode(task, &p); err != nil {
		return err
	}

	session, err := h.Repos.Sessions.ByID(ctx, p.SessionID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}

	started, err := h.Repos.Sessions.Start(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	if !started {
		// Host started it manually, or it was cancelled. Either way, done.
		return nil
	}

	return h.Services.Notifications.Dispatch(ctx, jobs.NotificationSendPayload{
		UserID: session.HostID,
		Kind:   "session.started",
		Title:  "Your session is live",
		Body:   fmt.Sprintf("%q has started.", session.Title),
		Data:   map[string]any{"sessionId": session.ID.String()},
	})
}

// handleSessionReconcile closes out participants left dangling when a session
// ended, so the active-participant count does not drift.
func (h *Handlers) handleSessionReconcile(ctx context.Context, task *asynq.Task) error {
	var p jobs.SessionPayload
	if err := decode(task, &p); err != nil {
		return err
	}

	session, err := h.Repos.Sessions.ByID(ctx, p.SessionID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	for i := range session.Participants {
		participant := &session.Participants[i]
		if participant.LeftAt != nil {
			continue
		}
		if err := h.Repos.Sessions.LeaveParticipant(ctx, session.ID, participant.UserID); err != nil {
			h.Logger.Warn("reconcile participant failed",
				slog.String("participant_id", participant.ID.String()))
		}
	}
	return nil
}

// handleIndexerSync mirrors new Envio events into Postgres.
func (h *Handlers) handleIndexerSync(ctx context.Context, task *asynq.Task) error {
	var p jobs.IndexerSyncPayload
	if err := decode(task, &p); err != nil {
		return err
	}

	synced, err := h.Services.Onchain.Sync(ctx, p.ChainID, p.BatchSize)
	if err != nil {
		return fmt.Errorf("sync chain %d: %w", p.ChainID, err)
	}
	if synced > 0 {
		h.Logger.Info("indexer sync",
			slog.Int64("chain_id", p.ChainID), slog.Int("events", synced))
	}
	return nil
}

// handleCacheWarm repopulates hot keys after a deploy so the first user does
// not pay the cold-cache cost.
func (h *Handlers) handleCacheWarm(ctx context.Context, task *asynq.Task) error {
	var p jobs.CacheWarmPayload
	if err := decode(task, &p); err != nil {
		return err
	}

	if p.Scope == "" || p.Scope == "onchain" {
		if _, err := h.Services.Onchain.IndexerStatus(ctx); err != nil {
			h.Logger.Warn("cache warm: indexer status", slog.String("error", err.Error()))
		}
	}
	return nil
}

// handleAuditPrune enforces the audit-log retention window.
func (h *Handlers) handleAuditPrune(ctx context.Context, task *asynq.Task) error {
	var p jobs.AuditPrunePayload
	if err := decode(task, &p); err != nil {
		return err
	}
	if p.RetainDays <= 0 {
		p.RetainDays = 90
	}

	deleted, err := h.Repos.Audit.Prune(ctx, time.Duration(p.RetainDays)*24*time.Hour)
	if err != nil {
		return fmt.Errorf("prune audit log: %w", err)
	}
	if deleted > 0 {
		h.Logger.Info("audit log pruned", slog.Int64("deleted", deleted))
	}
	return nil
}
