package handlers

import (
	"net/http"
	"strconv"

	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/repository"
	"github.com/meterrail/api/internal/service"
)

type OnchainHandler struct {
	Onchain *service.OnchainService
	Jobs    *jobs.Client
}

// List serves the mirrored indexer data. Reads hit our Postgres copy rather
// than Envio directly, so the API stays up when the indexer is resyncing.
func (h *OnchainHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := httpx.Pagination(r)

	filter := repository.OnchainFilter{
		EventName: r.URL.Query().Get("eventName"),
		Contract:  r.URL.Query().Get("contract"),
		Address:   r.URL.Query().Get("address"),
		Page:      repository.Page{Limit: limit, Offset: offset},
	}

	if raw := r.URL.Query().Get("chainId"); raw != "" {
		chainID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			httpx.Error(w, r, httpx.BadRequest("chainId must be an integer"))
			return
		}
		filter.ChainID = &chainID
	}

	events, total, err := h.Onchain.List(r.Context(), filter)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Paginated(w, events, total, limit, offset)
}

// Status reports indexer head versus our mirrored checkpoint, so a stale
// dashboard is diagnosable without shell access.
func (h *OnchainHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.Onchain.IndexerStatus(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, status)
}

// TriggerSync queues an out-of-band sync. Mounted behind the service token
// because it is an operator action, not a user one.
func (h *OnchainHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ChainID   int64 `json:"chainId"`
		BatchSize int   `json:"batchSize,omitempty"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if in.ChainID == 0 {
		httpx.Error(w, r, httpx.BadRequest("chainId is required"))
		return
	}

	task, err := jobs.NewIndexerSyncTask(jobs.IndexerSyncPayload{
		ChainID:   in.ChainID,
		BatchSize: in.BatchSize,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.Jobs.Enqueue(r.Context(), task); err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"queued":  true,
		"chainId": in.ChainID,
	})
}
