package handlers

import (
	"net/http"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/service"
)

type NotificationHandler struct {
	Notifications *service.NotificationService
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())
	limit, offset := httpx.Pagination(r)

	items, total, err := h.Notifications.List(
		r.Context(), identity.UserID, httpx.QueryBool(r, "unread"), limit, offset)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Paginated(w, items, total, limit, offset)
}

// UnreadCount backs the notification badge; it is polled, so it stays cheap.
func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	count, err := h.Notifications.UnreadCount(r.Context(), identity.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int64{"count": count})
}

func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "notificationID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.Notifications.MarkRead(r.Context(), identity.UserID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	updated, err := h.Notifications.MarkAllRead(r.Context(), identity.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int64{"updated": updated})
}

// Dispatch queues a notification. Service-token only: this is how other
// internal systems fan out to users.
func (h *NotificationHandler) Dispatch(w http.ResponseWriter, r *http.Request) {
	var in jobs.NotificationSendPayload
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.Notifications.Dispatch(r.Context(), in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]bool{"queued": true})
}
