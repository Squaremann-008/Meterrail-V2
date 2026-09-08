package handlers

import (
	"context"
	"net/http"
	"runtime"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/database"
	"github.com/meterrail/api/internal/envio"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/jobs"
	"github.com/meterrail/api/internal/storage"
)

// startedAt anchors the uptime figure reported by the health endpoint.
var startedAt = time.Now()

type HealthHandler struct {
	DB      *gorm.DB
	Cache   *cache.Cache
	Jobs    *jobs.Client
	Storage *storage.Client
	Envio   *envio.Client
	Version string
	Commit  string
}

type checkResult struct {
	Status  string `json:"status"`
	Latency string `json:"latencyMs,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Live is the liveness probe: it answers as long as the process can serve, and
// deliberately touches no dependency. nginx and the container runtime use it.
func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": h.Version,
		"commit":  h.Commit,
		"uptime":  time.Since(startedAt).Round(time.Second).String(),
	})
}

// Ready is the readiness probe: it verifies every hard dependency and returns
// 503 if any is down, so a rolling deploy will not send traffic too early.
// Optional integrations are reported but never fail the check.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type namedCheck struct {
		name     string
		required bool
		probe    func(context.Context) error
	}

	checks := []namedCheck{
		{"postgres", true, func(ctx context.Context) error { return database.Health(ctx, h.DB) }},
		{"redis", true, h.Cache.Health},
	}
	if h.Storage != nil {
		checks = append(checks, namedCheck{"r2", false, h.Storage.Health})
	}
	if h.Envio != nil {
		checks = append(checks, namedCheck{"envio", false, h.Envio.Health})
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[string]checkResult, len(checks))
		healthy = true
	)

	// Probes run concurrently so a slow dependency does not serialise the rest.
	for _, check := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()

			start := time.Now()
			err := check.probe(ctx)
			latency := time.Since(start).Round(time.Millisecond).String()

			result := checkResult{Status: "ok", Latency: latency}
			if err != nil {
				result.Status = "error"
				result.Error = err.Error()
			}

			mu.Lock()
			defer mu.Unlock()
			results[check.name] = result
			if err != nil && check.required {
				healthy = false
			}
		}()
	}
	wg.Wait()

	status := http.StatusOK
	overall := "ok"
	if !healthy {
		status = http.StatusServiceUnavailable
		overall = "degraded"
	}

	httpx.JSON(w, status, map[string]any{
		"status":     overall,
		"version":    h.Version,
		"commit":     h.Commit,
		"uptime":     time.Since(startedAt).Round(time.Second).String(),
		"goroutines": runtime.NumGoroutine(),
		"checks":     results,
	})
}

// Queues exposes asynq depth for the operator dashboard, alongside Asynqmon.
func (h *HealthHandler) Queues(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Jobs.QueueStats(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, stats)
}
