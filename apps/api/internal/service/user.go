package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/meterrail/api/internal/auth"
	"github.com/meterrail/api/internal/cache"
	"github.com/meterrail/api/internal/httpx"
	"github.com/meterrail/api/internal/models"
	"github.com/meterrail/api/internal/repository"
)

// identityCacheTTL is short on purpose: it only needs to absorb the burst of
// requests a single page load produces, not to be a session store.
const identityCacheTTL = 2 * time.Minute

type UserService struct{ deps Deps }

// ResolveIdentity turns verified Dynamic claims into a local Identity,
// provisioning the user row on first sight. The result is cached per Dynamic
// user so the common path is one Redis read instead of a write transaction.
func (s *UserService) ResolveIdentity(ctx context.Context, claims *auth.Claims) (*auth.Identity, error) {
	key := "identity:" + claims.Subject

	identity, err := cache.Remember(ctx, s.deps.Cache, key, identityCacheTTL,
		func(ctx context.Context) (*auth.Identity, error) {
			return s.provision(ctx, claims)
		})
	if err != nil {
		return nil, err
	}

	// The cached copy predates this token, so refresh the per-request fields.
	identity.SessionID = claims.SessionID
	identity.Scopes = claims.Scopes
	return identity, nil
}

func (s *UserService) provision(ctx context.Context, claims *auth.Claims) (*auth.Identity, error) {
	var wallet *models.Wallet
	if verified, ok := claims.PrimaryWallet(); ok {
		wallet = &models.Wallet{
			Address:  verified.Address,
			Chain:    walletChain(verified.Chain),
			Provider: verified.WalletName,
		}
	}

	displayName := displayNameFor(claims)

	user, err := s.deps.Repos.Users.UpsertFromClaims(ctx, claims.Subject, claims.Email, displayName, wallet)
	if err != nil {
		return nil, fmt.Errorf("provision user %s: %w", claims.Subject, err)
	}
	if !user.IsActive {
		return nil, httpx.Forbidden("this account has been deactivated")
	}

	identity := &auth.Identity{
		UserID:        user.ID,
		DynamicUserID: user.DynamicUserID,
		SessionID:     claims.SessionID,
		Scopes:        claims.Scopes,
	}
	if user.Email != nil {
		identity.Email = *user.Email
	}
	if wallet != nil {
		identity.WalletAddress = models.NormalizeAddress(wallet.Chain, wallet.Address)
		identity.Chain = string(wallet.Chain)
	}
	return identity, nil
}

// displayNameFor picks the friendliest label the claims offer, falling back to
// a truncated wallet address so the UI always has something to render.
func displayNameFor(claims *auth.Claims) string {
	if claims.Alias != "" {
		return claims.Alias
	}
	if claims.Email != "" {
		if local, _, found := strings.Cut(claims.Email, "@"); found {
			return local
		}
		return claims.Email
	}
	if wallet, ok := claims.PrimaryWallet(); ok && len(wallet.Address) > 10 {
		return wallet.Address[:6] + "…" + wallet.Address[len(wallet.Address)-4:]
	}
	return "anonymous"
}

// walletChain maps Dynamic's chain label onto our enum.
func walletChain(chain string) models.WalletChain {
	switch strings.ToLower(chain) {
	case "solana", "sol":
		return models.ChainSolana
	default:
		return models.ChainEVM
	}
}

func (s *UserService) Get(ctx context.Context, id uuid.UUID) (*models.User, error) {
	user, err := s.deps.Repos.Users.ByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, httpx.NotFound("user")
	}
	return user, err
}

// UpdateProfileInput carries the fields a user may change about themselves.
type UpdateProfileInput struct {
	DisplayName *string `json:"displayName"`
	Username    *string `json:"username"`
	AvatarURL   *string `json:"avatarUrl"`
}

func (s *UserService) UpdateProfile(ctx context.Context, id uuid.UUID, in UpdateProfileInput) (*models.User, error) {
	updates := map[string]any{}

	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if name == "" || len(name) > 128 {
			return nil, httpx.UnprocessableEntity("invalid profile",
				map[string]any{"displayName": "must be between 1 and 128 characters"})
		}
		updates["display_name"] = name
	}

	if in.Username != nil {
		username := strings.ToLower(strings.TrimSpace(*in.Username))
		if !validUsername(username) {
			return nil, httpx.UnprocessableEntity("invalid profile",
				map[string]any{"username": "must be 3-64 characters of a-z, 0-9, _ or -"})
		}
		updates["username"] = username
	}

	if in.AvatarURL != nil {
		updates["avatar_url"] = strings.TrimSpace(*in.AvatarURL)
	}

	user, err := s.deps.Repos.Users.Update(ctx, id, updates)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, httpx.Conflict("that username is already taken")
		}
		if errors.Is(err, repository.ErrNotFound) {
			return nil, httpx.NotFound("user")
		}
		return nil, err
	}

	s.invalidate(ctx, user.DynamicUserID)
	return user, nil
}

func validUsername(username string) bool {
	if len(username) < 3 || len(username) > 64 {
		return false
	}
	for _, r := range username {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isAllowed {
			return false
		}
	}
	return true
}

// invalidate drops the cached identity after a profile write.
func (s *UserService) invalidate(ctx context.Context, dynamicUserID string) {
	if err := s.deps.Cache.Delete(ctx, "identity:"+dynamicUserID); err != nil {
		s.deps.Logger.Warn("identity cache eviction failed", slog.String("error", err.Error()))
	}
}

func (s *UserService) List(ctx context.Context, search string, limit, offset int) ([]models.User, int64, error) {
	return s.deps.Repos.Users.List(ctx, repository.Page{Limit: limit, Offset: offset}, search)
}

func (s *UserService) Organizations(ctx context.Context, userID uuid.UUID) ([]models.Organization, error) {
	return s.deps.Repos.Organizations.ForUser(ctx, userID)
}

// isUniqueViolation detects Postgres error 23505 without importing pgx here.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}
