package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// Scopes an integration token can hold.
const (
	ScopeOwnerRead = "owner:read"
	ScopeDelegate  = "delegate"
)

// IntegrationTokenPrefix starts every integration token, so it can be told
// from the web auth token and recognised where it should not be.
const IntegrationTokenPrefix = "lgi_"

var (
	// ErrIntegrationUnauthorized is an integration token that is unknown or revoked.
	ErrIntegrationUnauthorized = errors.New("unknown or revoked integration token")

	integrationNamePattern = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	integrationScopes      = []string{ScopeOwnerRead, ScopeDelegate}
)

// IntegrationIdentity is who a valid integration token belongs to.
type IntegrationIdentity struct {
	TokenID string
	Name    string
	Scopes  []string
}

// Has says whether the identity holds a scope.
func (i IntegrationIdentity) Has(scope string) bool { return slices.Contains(i.Scopes, scope) }

// IntegrationTokenService creates, lists, revokes and checks integration tokens.
type IntegrationTokenService struct {
	db *store.Store
}

func NewIntegrationTokenService(db *store.Store) *IntegrationTokenService {
	return &IntegrationTokenService{db: db}
}

// Create makes a token for an integration and returns it. It is the only
// time the token is seen: only its hash is kept.
func (s *IntegrationTokenService) Create(ctx context.Context, name string, scopes []string) (string, error) {
	if !integrationNamePattern.MatchString(name) {
		return "", fmt.Errorf("integration name %q: use 1 to 40 of a-z, 0-9 and -", name)
	}
	if len(scopes) == 0 {
		return "", errors.New("give at least one scope: " + strings.Join(integrationScopes, ", "))
	}
	var kept []string
	for _, scope := range scopes {
		if !slices.Contains(integrationScopes, scope) {
			return "", fmt.Errorf("unknown scope %q: the scopes are %s", scope, strings.Join(integrationScopes, ", "))
		}
		if !slices.Contains(kept, scope) {
			kept = append(kept, scope)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := IntegrationTokenPrefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	idBytes := make([]byte, 5)
	if _, err := rand.Read(idBytes); err != nil {
		return "", err
	}
	err := s.db.InsertIntegrationToken(ctx, store.IntegrationToken{ID: "it-" + hex.EncodeToString(idBytes), Name: name, Scopes: kept, TokenHash: hashIntegrationToken(token)})
	if errors.Is(err, store.ErrConflict) {
		return "", fmt.Errorf("integration %q already has a token; revoke it first", name)
	}
	if err != nil {
		return "", err
	}
	return token, nil
}

// List lists every token, revoked ones too. It never holds a token.
func (s *IntegrationTokenService) List(ctx context.Context) ([]store.IntegrationToken, error) {
	return s.db.ListIntegrationTokens(ctx)
}

// Revoke stops an integration's token working at once.
func (s *IntegrationTokenService) Revoke(ctx context.Context, name string) error {
	if err := s.db.RevokeIntegrationToken(ctx, name); errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("integration %q has no live token", name)
	} else if err != nil {
		return err
	}
	return nil
}

// Authenticate returns who a token belongs to, or ErrIntegrationUnauthorized.
func (s *IntegrationTokenService) Authenticate(ctx context.Context, token string) (IntegrationIdentity, error) {
	if !strings.HasPrefix(token, IntegrationTokenPrefix) {
		return IntegrationIdentity{}, ErrIntegrationUnauthorized
	}
	t, err := s.db.IntegrationTokenByHash(ctx, hashIntegrationToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return IntegrationIdentity{}, ErrIntegrationUnauthorized
	}
	if err != nil {
		return IntegrationIdentity{}, err
	}
	_ = s.db.TouchIntegrationToken(ctx, t.ID)
	return IntegrationIdentity{TokenID: t.ID, Name: t.Name, Scopes: t.Scopes}, nil
}

func hashIntegrationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
