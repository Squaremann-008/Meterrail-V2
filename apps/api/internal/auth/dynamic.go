// Package auth verifies Dynamic (dynamic.xyz) JWTs. Dynamic runs the wallet
// signature challenge and the embedded-wallet flows; this package's only job is
// to prove a bearer token was signed by the environment's JWKS and to project
// its claims onto a local identity.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"

	"github.com/meterrail/api/internal/config"
)

var (
	ErrNoToken       = errors.New("auth: no bearer token")
	ErrInvalidToken  = errors.New("auth: invalid token")
	ErrNotConfigured = errors.New("auth: Dynamic is not configured")
)

// VerifiedWallet is one wallet Dynamic has proven the user controls.
type VerifiedWallet struct {
	Address    string `json:"address"`
	Chain      string `json:"chain"`
	WalletName string `json:"wallet_name"`
	ID         string `json:"id"`
	PublicKey  string `json:"public_key"`
}

// Claims is the subset of the Dynamic JWT we rely on. Dynamic ships more; the
// rest is ignored deliberately so a provider-side addition cannot break parsing.
type Claims struct {
	jwt.RegisteredClaims

	Email               string           `json:"email"`
	EnvironmentID       string           `json:"environment_id"`
	VerifiedAccount     string           `json:"verified_account"`
	VerifiedCredentials []VerifiedWallet `json:"verified_credentials"`
	Alias               string           `json:"alias"`
	SessionID           string           `json:"sid"`
	Scopes              []string         `json:"scopes"`
	LastVerifiedAt      *jwt.NumericDate `json:"last_verified_credential_id_at"`
}

// PrimaryWallet returns the first verified credential that carries an address.
func (c *Claims) PrimaryWallet() (VerifiedWallet, bool) {
	for _, w := range c.VerifiedCredentials {
		if strings.TrimSpace(w.Address) != "" {
			return w, true
		}
	}
	return VerifiedWallet{}, false
}

// Verifier validates tokens against Dynamic's rotating JWKS. The key set is
// fetched once and refreshed in the background, so verification is local.
type Verifier struct {
	keyfunc keyfunc.Keyfunc
	cfg     config.Auth
	logger  *slog.Logger
	parser  *jwt.Parser
}

// NewVerifier fetches the JWKS and starts the refresh loop. It returns
// ErrNotConfigured when no environment/JWKS URL is set, which lets local
// development run with auth disabled.
func NewVerifier(ctx context.Context, cfg config.Auth, logger *slog.Logger) (*Verifier, error) {
	jwksURL := cfg.JWKS()
	if jwksURL == "" {
		return nil, ErrNotConfigured
	}

	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		return nil, fmt.Errorf("auth: fetch JWKS from %s: %w", jwksURL, err)
	}

	opts := []jwt.ParserOption{
		jwt.WithLeeway(cfg.Leeway),
		jwt.WithExpirationRequired(),
		// Dynamic signs with RS256; pinning it blocks alg-confusion attacks.
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
	}
	if cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(cfg.Issuer))
	}
	if cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(cfg.Audience))
	}

	logger.Info("dynamic auth ready", slog.String("jwks_url", jwksURL))
	return &Verifier{
		keyfunc: kf,
		cfg:     cfg,
		logger:  logger,
		parser:  jwt.NewParser(opts...),
	}, nil
}

// Verify parses and validates a raw bearer token.
// The context parameter is kept for call-site symmetry with the rest of the
// service layer; JWKS verification itself is local and cannot block.
func (v *Verifier) Verify(_ context.Context, raw string) (*Claims, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrNoToken
	}

	claims := &Claims{}
	token, err := v.parser.ParseWithClaims(raw, claims, v.keyfunc.Keyfunc)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: missing sub claim", ErrInvalidToken)
	}
	// Belt and braces: reject a token minted for a different Dynamic project
	// even if it happens to be signed by a key we trust.
	if v.cfg.DynamicEnvironmentID != "" && claims.EnvironmentID != "" &&
		claims.EnvironmentID != v.cfg.DynamicEnvironmentID {
		return nil, fmt.Errorf("%w: environment mismatch", ErrInvalidToken)
	}
	return claims, nil
}

// BearerToken pulls the credential out of an Authorization header value.
func BearerToken(header string) string {
	const prefix = "bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

// TokenTTL reports how long a verified token remains valid, for cache sizing.
func (c *Claims) TokenTTL(now time.Time) time.Duration {
	if c.ExpiresAt == nil {
		return 0
	}
	if d := c.ExpiresAt.Sub(now); d > 0 {
		return d
	}
	return 0
}
