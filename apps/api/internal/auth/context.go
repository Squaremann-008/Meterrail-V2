package auth

import (
	"context"

	"github.com/google/uuid"
)

// Identity is the authenticated caller as the rest of the app sees it: the
// local user row joined with the claims that produced it.
type Identity struct {
	UserID        uuid.UUID `json:"userId"`
	DynamicUserID string    `json:"dynamicUserId"`
	Email         string    `json:"email,omitempty"`
	WalletAddress string    `json:"walletAddress,omitempty"`
	Chain         string    `json:"chain,omitempty"`
	SessionID     string    `json:"sessionId,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	// Service marks a machine-to-machine caller authenticated with the
	// internal service token rather than a user JWT.
	Service bool `json:"service,omitempty"`
}

// HasScope reports whether the token carries a scope. Service callers bypass
// scope checks because they are trusted by deployment, not by token contents.
func (i *Identity) HasScope(scope string) bool {
	if i == nil {
		return false
	}
	if i.Service {
		return true
	}
	for _, s := range i.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

type identityKey struct{}

// WithIdentity attaches the caller to the request context.
func WithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

// FromContext returns the caller, or false when the request is anonymous.
func FromContext(ctx context.Context) (*Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(*Identity)
	return identity, ok && identity != nil
}

// MustUserID returns the caller's user id, or uuid.Nil when anonymous.
func MustUserID(ctx context.Context) uuid.UUID {
	if identity, ok := FromContext(ctx); ok {
		return identity.UserID
	}
	return uuid.Nil
}
