package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
	"github.com/meterrail/api/internal/storage"
)

// allowedContentTypes is an allowlist, not a denylist: anything not named here
// is refused, so a new file type is an explicit decision.
var allowedContentTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/gif":       true,
	"image/avif":      true,
	"video/mp4":       true,
	"video/webm":      true,
	"application/pdf": true,
}

type MediaService struct{ deps Deps }

// RequestUploadInput is what the browser sends before it uploads anything.
type RequestUploadInput struct {
	Filename       string     `json:"filename"`
	ContentType    string     `json:"contentType"`
	SizeBytes      int64      `json:"sizeBytes"`
	OrganizationID *uuid.UUID `json:"organizationId,omitempty"`
}

// UploadTicket pairs the presigned URL with the row that tracks it.
type UploadTicket struct {
	Asset  *models.MediaAsset       `json:"asset"`
	Upload *storage.PresignedUpload `json:"upload"`
}

// RequestUpload validates the request, reserves a key, writes a pending row and
// hands back a presigned PUT. The row exists before the object so an abandoned
// upload is discoverable by the reaper.
func (s *MediaService) RequestUpload(ctx context.Context, ownerID uuid.UUID, in RequestUploadInput) (*UploadTicket, error) {
	if s.deps.Storage == nil {
		return nil, httpx.ServiceUnavailable("object storage")
	}

	filename := strings.TrimSpace(in.Filename)
	if filename == "" || len(filename) > 512 {
		return nil, httpx.UnprocessableEntity("invalid upload",
			map[string]any{"filename": "must be between 1 and 512 characters"})
	}
	// Reject traversal in the name we persist; the storage key is generated
	// anyway, but the display name should not carry path separators.
	filename = filepath.Base(filename)

	contentType := strings.ToLower(strings.TrimSpace(in.ContentType))
	if !allowedContentTypes[contentType] {
		return nil, httpx.UnprocessableEntity("unsupported file type",
			map[string]any{"contentType": fmt.Sprintf("%q is not an accepted type", in.ContentType)})
	}

	if in.SizeBytes <= 0 || in.SizeBytes > s.deps.Storage.MaxUploadBytes() {
		return nil, httpx.UnprocessableEntity("invalid upload",
			map[string]any{"sizeBytes": fmt.Sprintf("must be between 1 and %d bytes", s.deps.Storage.MaxUploadBytes())})
	}

	if in.OrganizationID != nil {
		if err := s.assertOrgMember(ctx, ownerID, *in.OrganizationID); err != nil {
			return nil, err
		}
	}

	key := storage.BuildKey("uploads/"+ownerID.String(), filename)

	asset := &models.MediaAsset{
		OwnerID:        ownerID,
		OrganizationID: in.OrganizationID,
		Key:            key,
		Bucket:         s.deps.Storage.Bucket(),
		Filename:       filename,
		ContentType:    contentType,
		SizeBytes:      in.SizeBytes,
		Status:         models.MediaPending,
	}
	if err := s.deps.Repos.Media.Create(ctx, asset); err != nil {
		return nil, fmt.Errorf("create media row: %w", err)
	}

	upload, err := s.deps.Storage.PresignUpload(ctx, key, contentType, in.SizeBytes)
	if err != nil {
		// The row is left pending; the reaper will clear it.
		return nil, fmt.Errorf("presign upload: %w", err)
	}

	return &UploadTicket{Asset: asset, Upload: upload}, nil
}

// ConfirmUpload is called after the browser's PUT succeeds. It verifies the
// object really landed by asking R2 for its size, then queues processing.
func (s *MediaService) ConfirmUpload(ctx context.Context, ownerID, assetID uuid.UUID) (*models.MediaAsset, error) {
	if s.deps.Storage == nil {
		return nil, httpx.ServiceUnavailable("object storage")
	}

	asset, err := s.deps.Repos.Media.ByID(ctx, assetID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, httpx.NotFound("media asset")
	}
	if err != nil {
		return nil, err
	}
	if asset.OwnerID != ownerID {
		return nil, httpx.Forbidden("this asset belongs to another user")
	}
	if asset.Status != models.MediaPending {
		// Confirming twice is not an error; return what we already have.
		return asset, nil
	}

	info, err := s.deps.Storage.Head(ctx, asset.Key)
	if err != nil {
		return nil, httpx.BadRequest("the object was not found in storage; upload it before confirming")
	}

	publicURL := s.deps.Storage.PublicURL(asset.Key)
	if err := s.deps.Repos.Media.MarkUploaded(ctx, asset.ID, info.SizeBytes, info.ContentType, info.ETag, publicURL); err != nil {
		return nil, err
	}

	task, err := jobs.NewMediaProcessTask(jobs.MediaProcessPayload{
		MediaAssetID:       asset.ID,
		Key:                asset.Key,
		GenerateThumbnails: strings.HasPrefix(asset.ContentType, "image/"),
	})
	if err != nil {
		return nil, err
	}
	if err := s.deps.Jobs.Enqueue(ctx, task); err != nil {
		// The upload succeeded; failing to queue post-processing should not
		// fail the request, so log and let the reconcile cron pick it up.
		s.deps.Logger.Error("enqueue media processing",
			slog.String("asset_id", asset.ID.String()), slog.String("error", err.Error()))
	}

	return s.deps.Repos.Media.ByID(ctx, asset.ID)
}

func (s *MediaService) Get(ctx context.Context, ownerID, assetID uuid.UUID) (*models.MediaAsset, error) {
	asset, err := s.deps.Repos.Media.ByID(ctx, assetID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, httpx.NotFound("media asset")
	}
	if err != nil {
		return nil, err
	}
	if asset.OwnerID != ownerID {
		return nil, httpx.Forbidden("this asset belongs to another user")
	}
	return asset, nil
}

// DownloadURL returns a public URL when the bucket is public, and a short-lived
// presigned URL otherwise.
func (s *MediaService) DownloadURL(ctx context.Context, ownerID, assetID uuid.UUID, ttl time.Duration) (string, error) {
	asset, err := s.Get(ctx, ownerID, assetID)
	if err != nil {
		return "", err
	}
	if asset.PublicURL != "" {
		return asset.PublicURL, nil
	}
	if s.deps.Storage == nil {
		return "", httpx.ServiceUnavailable("object storage")
	}
	return s.deps.Storage.PresignDownload(ctx, asset.Key, ttl)
}

func (s *MediaService) List(ctx context.Context, ownerID uuid.UUID, limit, offset int) ([]models.MediaAsset, int64, error) {
	return s.deps.Repos.Media.ListByOwner(ctx, ownerID, repository.Page{Limit: limit, Offset: offset})
}

// Delete removes the object then the row. Storage first: a failure there leaves
// a row we can retry, whereas the reverse would orphan the object silently.
func (s *MediaService) Delete(ctx context.Context, ownerID, assetID uuid.UUID) error {
	asset, err := s.Get(ctx, ownerID, assetID)
	if err != nil {
		return err
	}
	if s.deps.Storage != nil {
		if err := s.deps.Storage.Delete(ctx, asset.Key); err != nil {
			return fmt.Errorf("delete object: %w", err)
		}
	}
	return s.deps.Repos.Media.Delete(ctx, asset.ID)
}

// assertOrgMember is the shared org-scoping guard.
func (s *MediaService) assertOrgMember(ctx context.Context, userID, orgID uuid.UUID) error {
	_, err := s.deps.Repos.Organizations.RoleOf(ctx, userID, orgID)
	if errors.Is(err, repository.ErrNotFound) {
		return httpx.Forbidden("you are not a member of this organization")
	}
	return err
}
