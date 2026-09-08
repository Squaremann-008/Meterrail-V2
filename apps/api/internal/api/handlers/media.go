package handlers

import (
	"net/http"
	"time"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/service"
)

type MediaHandler struct {
	Media *service.MediaService
}

// RequestUpload issues a presigned R2 PUT. The browser uploads directly to
// Cloudflare, so file bytes never pass through this server.
func (h *MediaHandler) RequestUpload(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	var in service.RequestUploadInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ticket, err := h.Media.RequestUpload(r.Context(), identity.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, ticket)
}

// Confirm marks a presigned upload complete. It verifies against R2 rather than
// trusting the client, then queues post-processing.
func (h *MediaHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "assetID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	asset, err := h.Media.ConfirmUpload(r.Context(), identity.UserID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, asset)
}

func (h *MediaHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())
	limit, offset := httpx.Pagination(r)

	assets, total, err := h.Media.List(r.Context(), identity.UserID, limit, offset)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Paginated(w, assets, total, limit, offset)
}

func (h *MediaHandler) Get(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "assetID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	asset, err := h.Media.Get(r.Context(), identity.UserID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, asset)
}

// DownloadURL hands back a public CDN URL for public buckets, or a short-lived
// presigned GET for private ones.
func (h *MediaHandler) DownloadURL(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "assetID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	ttl := time.Duration(httpx.QueryInt(r, "ttlSeconds", 0)) * time.Second
	url, err := h.Media.DownloadURL(r.Context(), identity.UserID, id, ttl)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"url": url})
}

func (h *MediaHandler) Delete(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "assetID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.Media.Delete(r.Context(), identity.UserID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
