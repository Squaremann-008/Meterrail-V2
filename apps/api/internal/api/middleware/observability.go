package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/logging"
)

const requestIDHeader = "X-Request-Id"

// RequestID assigns or propagates a correlation id and puts a logger carrying
// it on the context, so every log line for one request is joinable.
func RequestID(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(requestIDHeader, id)

			ctx := logging.Into(r.Context(), logger.With(slog.String("request_id", id)))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// statusRecorder captures the status and byte count for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, which keeps
// flushing and hijacking working through the wrapper.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// AccessLog emits one structured line per request. Health checks are logged at
// debug so they do not drown the log in production.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		attrs := []any{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int("bytes", recorder.bytes),
			slog.Duration("duration", time.Since(start)),
		}

		logger := logging.From(r.Context())
		switch {
		case r.URL.Path == "/health" || r.URL.Path == "/health/live":
			logger.Debug("request", attrs...)
		case recorder.status >= 500:
			logger.Error("request", attrs...)
		case recorder.status >= 400:
			logger.Warn("request", attrs...)
		default:
			logger.Info("request", attrs...)
		}
	})
}

// Recover turns a panic into a 500 instead of killing the connection, and logs
// the stack once.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// A client disconnect surfaces as this sentinel; it is not a bug,
			// and the net/http server expects it to keep propagating.
			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}
			logging.From(r.Context()).Error("panic recovered",
				slog.Any("panic", recovered),
				slog.String("stack", string(debug.Stack())),
			)
			httpx.Error(w, r, httpx.Internal(nil))
		}()
		next.ServeHTTP(w, r)
	})
}

// RateLimit applies a fixed-window limit keyed by user id when authenticated
// and by client IP otherwise, so one noisy user cannot exhaust a shared IP's
// allowance.
func RateLimit(c *cache.Cache, perMinute int, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := "ip:" + clientIP(r, trustProxy)
			if identity, ok := auth.FromContext(r.Context()); ok && !identity.Service {
				key = "user:" + identity.UserID.String()
			}

			allowed, remaining, reset, err := c.RateLimit(r.Context(), key, perMinute, time.Minute)
			if err != nil {
				// Redis being down must not take the API down with it.
				logging.From(r.Context()).Warn("rate limiter unavailable", slog.String("error", err.Error()))
				next.ServeHTTP(w, r)
				return
			}

			resetSeconds := int(reset.Seconds())
			w.Header().Set("X-RateLimit-Limit", itoa(perMinute))
			w.Header().Set("X-RateLimit-Remaining", itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", itoa(resetSeconds))

			if !allowed {
				w.Header().Set("Retry-After", itoa(resetSeconds))
				httpx.Error(w, r, httpx.TooManyRequests(resetSeconds))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// SecurityHeaders sets the defensive headers that are safe for a JSON API. The
// browser-facing CSP lives in nginx, in front of the Next.js app.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		next.ServeHTTP(w, r)
	})
}

// Timeout bounds handler execution so a stuck dependency cannot pin a request
// goroutine forever.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":{"code":"timeout","message":"the request took too long"}}`)
	}
}
