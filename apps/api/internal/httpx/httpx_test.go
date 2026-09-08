package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMetaHasMore(t *testing.T) {
	tests := []struct {
		name    string
		total   int64
		limit   int
		offset  int
		hasMore bool
	}{
		{"first page of many", 100, 25, 0, true},
		{"last full page", 100, 25, 75, false},
		{"exactly one page", 10, 25, 0, false},
		{"empty result set", 0, 25, 0, false},
		{"offset past the end", 10, 25, 50, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := NewMeta(tt.total, tt.limit, tt.offset)
			if meta.HasMore != tt.hasMore {
				t.Errorf("HasMore = %v, want %v", meta.HasMore, tt.hasMore)
			}
		})
	}
}

func TestPaginationClamping(t *testing.T) {
	tests := []struct {
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"", 25, 0},
		{"?limit=50&offset=100", 50, 100},
		{"?limit=0", 25, 0},    // zero falls back to the default
		{"?limit=5000", 25, 0}, // above the cap falls back
		{"?limit=-5", 25, 0},   // negative falls back
		{"?offset=-10", 25, 0}, // negative offset is clamped to zero
		{"?limit=abc", 25, 0},  // unparsable falls back
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/things"+tt.query, http.NoBody)

			limit, offset := Pagination(r)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Errorf("Pagination() = (%d, %d), want (%d, %d)",
					limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"a","typo":1}`))
	err := DecodeJSON(httptest.NewRecorder(), r, &dst)

	if err == nil {
		t.Fatal("expected an unknown field to be rejected")
	}
}

func TestDecodeJSONRejectsTrailingContent(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	// Two concatenated objects: accepting this would silently drop the second.
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"a"}{"name":"b"}`))
	err := DecodeJSON(httptest.NewRecorder(), r, &dst)

	if err == nil {
		t.Fatal("expected trailing JSON content to be rejected")
	}
}

func TestDecodeJSONRejectsEmptyBody(t *testing.T) {
	var dst struct{}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(""))
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst); err == nil {
		t.Fatal("expected an empty body to be rejected")
	}
}

func TestDecodeJSONAcceptsValidBody(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(`{"name":"meterrail"}`))
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst); err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if dst.Name != "meterrail" {
		t.Errorf("Name = %q, want meterrail", dst.Name)
	}
}

// An internal error must reach the client as a generic message; only the log
// gets the underlying cause.
func TestErrorDoesNotLeakInternalCause(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	Error(w, r, Internal(errNamedSecret))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "connection string") {
		t.Errorf("response leaked the internal cause: %s", w.Body.String())
	}
}

var errNamedSecret = &stringError{"failed to dial postgres connection string secret"}

type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

func TestJSONEnvelopeShape(t *testing.T) {
	w := httptest.NewRecorder()

	JSON(w, http.StatusOK, map[string]string{"hello": "world"})

	var decoded struct {
		Data map[string]string `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Data["hello"] != "world" {
		t.Errorf("data = %v, want {hello: world}", decoded.Data)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}
