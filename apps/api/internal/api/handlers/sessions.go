package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
	"github.com/meterrail/api/internal/service"
)

type SessionHandler struct {
	Sessions *service.SessionService
}

func (h *SessionHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	var in service.CreateSessionInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.Sessions.Create(r.Context(), identity.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, session)
}

func (h *SessionHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)

	filter := repository.SessionFilter{
		Status: models.SessionStatus(r.URL.Query().Get("status")),
		Page:   repository.Page{Limit: limit, Offset: offset},
	}

	if raw := r.URL.Query().Get("organizationId"); raw != "" {
		orgID, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, r, httpx.BadRequest("organizationId must be a valid UUID"))
			return
		}
		filter.OrganizationID = &orgID
	}

	// `mine=true` scopes the list to sessions the caller hosts.
	if httpx.QueryBool(r, "mine") {
		identity, ok := auth.FromContext(r.Context())
		if !ok {
			httpx.Error(w, r, httpx.Unauthorized(""))
			return
		}
		filter.HostID = &identity.UserID
	}

	sessions, total, err := h.Sessions.List(r.Context(), filter)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Paginated(w, sessions, total, limit, offset)
}

func (h *SessionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.Sessions.Get(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

// Join authorises the caller and returns their Agora RTC/RTM tokens. The app
// certificate stays server-side; the client only ever sees a scoped token.
func (h *SessionHandler) Join(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	result, err := h.Sessions.Join(r.Context(), identity.UserID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// RenewToken re-issues an RTC token before the current one expires, which the
// client schedules from the token's expiresAt.
func (h *SessionHandler) RenewToken(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	token, err := h.Sessions.RenewToken(r.Context(), identity.UserID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, token)
}

func (h *SessionHandler) Leave(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.Sessions.Leave(r.Context(), identity.UserID, id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *SessionHandler) Start(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, h.Sessions.Start)
}

func (h *SessionHandler) End(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, h.Sessions.End)
}

// transition factors out the shared shape of the host-only lifecycle actions.
func (h *SessionHandler) transition(
	w http.ResponseWriter,
	r *http.Request,
	action func(ctx context.Context, userID, sessionID uuid.UUID) (*models.Session, error),
) {
	identity, _ := auth.FromContext(r.Context())

	id, err := pathUUID(r, "sessionID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := action(r.Context(), identity.UserID, id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}
