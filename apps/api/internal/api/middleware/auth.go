// Package middleware holds the cross-cutting HTTP concerns: authentication,
// request logging, rate limiting, panic recovery and request identity.
package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/logging"
	"github.com/meterrail/api/internal/service"
)

// Authenticator turns a Dynamic bearer token into an Identity on the context.
type Authenticator struct {
	Verifier     *auth.Verifier
	Users        *service.UserService
	ServiceToken string
	Logger       *slog.Logger
}

// Required rejects anything that is not a valid caller. Use it on every route
// that touches user data.
func (a *Authenticator) Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Optional usually runs first on the same chain; reuse what it
		// resolved rather than verifying the same token a second time.
		if _, ok := auth.FromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		identity, err := a.identify(r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if identity == nil {
			httpx.Error(w, r, httpx.Unauthorized(""))
			return
		}
		next.ServeHTTP(w, a.decorate(r, identity))
	})
}

// Optional attaches an identity when one is present but lets anonymous
// requests through, for endpoints with a public and a personalised shape.
func (a *Authenticator) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := a.identify(r)
		if err != nil || identity == nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, a.decorate(r, identity))
	})
}

// ServiceOnly gates internal endpoints (queue triggers, admin sync) behind the
// shared service token. It refuses outright when no token is configured so a
// misconfigured deploy fails closed.
func (a *Authenticator) ServiceOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.ServiceToken == "" {
			httpx.Error(w, r, httpx.Forbidden("internal endpoints are disabled"))
			return
		}
		presented := auth.BearerToken(r.Header.Get("Authorization"))
		if presented == "" {
			presented = r.Header.Get("X-Service-Token")
		}
		if !secureEqual(presented, a.ServiceToken) {
			httpx.Error(w, r, httpx.Unauthorized("invalid service token"))
			return
		}
		next.ServeHTTP(w, a.decorate(r, &auth.Identity{Service: true}))
	})
}

// identify returns (nil, nil) for an anonymous request and an error only when a
// credential was presented and failed.
func (a *Authenticator) identify(r *http.Request) (*auth.Identity, error) {
	raw := auth.BearerToken(r.Header.Get("Authorization"))
	if raw == "" {
		return nil, nil
	}
	if a.Verifier == nil {
		return nil, httpx.ServiceUnavailable("authentication")
	}

	claims, err := a.Verifier.Verify(r.Context(), raw)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) || errors.Is(err, auth.ErrNoToken) {
			return nil, httpx.Unauthorized("the access token is invalid or has expired")
		}
		return nil, httpx.Internal(err)
	}

	identity, err := a.Users.ResolveIdentity(r.Context(), claims)
	if err != nil {
		return nil, err
	}
	return identity, nil
}

// decorate puts the identity and its log fields on the request context.
func (a *Authenticator) decorate(r *http.Request, identity *auth.Identity) *http.Request {
	ctx := auth.WithIdentity(r.Context(), identity)

	logger := logging.From(ctx)
	if identity.Service {
		logger = logger.With(slog.String("caller", "service"))
	} else {
		logger = logger.With(slog.String("user_id", identity.UserID.String()))
	}
	return r.WithContext(logging.Into(ctx, logger))
}

// secureEqual is a constant-time comparison that does not leak length through
// early return.
func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range len(a) {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// RequireScope enforces a token scope on a route group.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := auth.FromContext(r.Context())
			if !ok {
				httpx.Error(w, r, httpx.Unauthorized(""))
				return
			}
			if !identity.HasScope(scope) {
				httpx.Error(w, r, httpx.Forbidden("missing required scope: "+scope))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP resolves the caller's address, honouring X-Forwarded-For only when
// the deployment says a trusted proxy sits in front.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first, _, found := strings.Cut(forwarded, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(forwarded)
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return strings.TrimSpace(realIP)
		}
	}
	host, _, found := strings.Cut(r.RemoteAddr, ":")
	if !found {
		return r.RemoteAddr
	}
	return host
}
