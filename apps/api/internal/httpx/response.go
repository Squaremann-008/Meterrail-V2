// Package httpx holds the JSON envelope, the error taxonomy and the small
// helpers every handler shares.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/meterrail/api/internal/logging"
)

// Envelope is the success shape for every endpoint: {"data": ...} plus an
// optional pagination block.
type Envelope struct {
	Data any   `json:"data"`
	Meta *Meta `json:"meta,omitempty"`
}

type Meta struct {
	Total   int64 `json:"total"`
	Limit   int   `json:"limit"`
	Offset  int   `json:"offset"`
	HasMore bool  `json:"hasMore"`
}

// NewMeta computes HasMore so clients do not have to.
func NewMeta(total int64, limit, offset int) *Meta {
	return &Meta{
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: int64(offset+limit) < total,
	}
}

// JSON writes a success response.
func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, Envelope{Data: data})
}

// Paginated writes a list response with its pagination meta.
func Paginated(w http.ResponseWriter, data any, total int64, limit, offset int) {
	write(w, http.StatusOK, Envelope{Data: data, Meta: NewMeta(total, limit, offset)})
}

// NoContent ends a request with 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already on the wire, so there is nothing to do
		// but record it.
		slog.Default().Error("encode response", slog.String("error", err.Error()))
	}
}

// Error renders an APIError, mapping unknown errors onto a 500 that never leaks
// internal detail to the client.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}

	logger := logging.From(r.Context())
	if apiErr.Status >= http.StatusInternalServerError {
		logger.Error("request failed",
			slog.String("code", apiErr.Code),
			slog.String("error", apiErr.internal()),
			slog.String("path", r.URL.Path),
		)
	} else {
		logger.Debug("request rejected",
			slog.String("code", apiErr.Code),
			slog.Int("status", apiErr.Status),
			slog.String("path", r.URL.Path),
		)
	}

	write(w, apiErr.Status, map[string]any{"error": apiErr})
}

// DecodeJSON reads and validates a request body, capping it so a large upload
// cannot exhaust memory, and rejecting unknown fields to catch client typos.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	const maxBody = 1 << 20 // 1 MiB
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return BadRequest("request body exceeds 1MB")
		case errors.Is(err, io.EOF):
			return BadRequest("request body is empty")
		default:
			return BadRequest(fmt.Sprintf("malformed JSON: %s", err))
		}
	}

	// A second value in the stream means the client sent concatenated objects.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BadRequest("request body must contain a single JSON object")
	}
	return nil
}

// QueryInt reads an integer query parameter, falling back on absent or
// unparsable input rather than erroring.
func QueryInt(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// QueryBool reads a boolean query parameter.
func QueryBool(r *http.Request, name string) bool {
	value, err := strconv.ParseBool(r.URL.Query().Get(name))
	return err == nil && value
}

// Pagination extracts limit/offset with sane clamping.
func Pagination(r *http.Request) (limit, offset int) {
	limit = QueryInt(r, "limit", 25)
	offset = QueryInt(r, "offset", 0)
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
