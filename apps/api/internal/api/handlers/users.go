// Package handlers contains the HTTP layer: decode, delegate to a service,
// encode. No business logic lives here.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/service"
)

type UserHandler struct {
	Users *service.UserService
}

// Me returns the authenticated caller's profile. This is the endpoint the
// frontend hits right after Dynamic finishes its wallet handshake, and it is
// what provisions the local user row on first login.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.FromContext(r.Context())
	if !ok {
		httpx.Error(w, r, httpx.Unauthorized(""))
		return
	}

	user, err := h.Users.Get(r.Context(), identity.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// UpdateMe patches the caller's own profile.
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.FromContext(r.Context())
	if !ok {
		httpx.Error(w, r, httpx.Unauthorized(""))
		return
	}

	var in service.UpdateProfileInput
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}

	user, err := h.Users.UpdateProfile(r.Context(), identity.UserID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// MyOrganizations lists the orgs the caller belongs to.
func (h *UserHandler) MyOrganizations(w http.ResponseWriter, r *http.Request) {
	identity, _ := auth.FromContext(r.Context())

	orgs, err := h.Users.Organizations(r.Context(), identity.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, orgs)
}

// Get returns a single user by id.
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "userID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	user, err := h.Users.Get(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// List is the paginated directory, with an optional search term.
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)

	users, total, err := h.Users.List(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Paginated(w, users, total, limit, offset)
}

// pathUUID parses a UUID path parameter into a typed 400 on malformed input.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.BadRequest(name + " must be a valid UUID")
	}
	return id, nil
}
