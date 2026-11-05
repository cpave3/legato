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
	ScopeAttention = "attention"
	// ScopeHandoff reaches /api/integrations/handoff/, where a main instance
	// hands tasks to this worker. Only pairing a main gives it (CreateMain),
	// and alone; Create refuses it.
	ScopeHandoff = "handoff"
)

// Kinds of integration token: Argus's, made with the CLI, and a paired main
// instance's, made by pairing. Names are unique within a kind.
const (
	IntegrationKindArgus = store.IntegrationKindArgus
	IntegrationKindMain  = store.IntegrationKindMain
)

// IntegrationTokenPrefix starts every integration token, so it can be told
// from the web auth token and recognised where it should not be.
const IntegrationTokenPrefix = "lgi_"

var (
	// ErrIntegrationUnauthorized is an integration token that is unknown or revoked.
	ErrIntegrationUnauthorized = errors.New("unknown or revoked integration token")
	// ErrMainNameTaken: another main instance is paired under that name.
	ErrMainNameTaken = errors.New("another main instance is paired under that name")
	// ErrMainNotPaired: no main instance of that instance ID is paired.
	ErrMainNotPaired = errors.New("that main instance is not paired")

	integrationNamePattern = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	integrationScopes      = []string{ScopeOwnerRead, ScopeDelegate, ScopeAttention}
)

// IntegrationIdentity is who a valid integration token belongs to. A paired
// main instance's has Kind main and its InstanceID, which is who it is: its
// Name is only what it is called.
type IntegrationIdentity struct {
	TokenID    string
	Kind       string
	Name       string
	InstanceID string
	Scopes     []string
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
	token, row, err := newIntegrationToken()
	if err != nil {
		return "", err
	}
	row.Kind, row.Name, row.Scopes = IntegrationKindArgus, name, kept
	err = s.db.InsertIntegrationToken(ctx, row)
	if errors.Is(err, store.ErrConflict) {
		return "", fmt.Errorf("integration %q already has a token; revoke it first", name)
	}
	if err != nil {
		return "", err
	}
	return token, nil
}

// newIntegrationToken makes a token and the row that keeps its hash.
func newIntegrationToken() (string, store.IntegrationToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", store.IntegrationToken{}, err
	}
	token := IntegrationTokenPrefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	idBytes := make([]byte, 5)
	if _, err := rand.Read(idBytes); err != nil {
		return "", store.IntegrationToken{}, err
	}
	return token, store.IntegrationToken{ID: "it-" + hex.EncodeToString(idBytes), TokenHash: hashIntegrationToken(token)}, nil
}

// CreateMain makes the token a main instance gets when it pairs with this
// worker: kind main, its instance ID, the handoff scope and nothing else.
// The name must already be normalised (NormaliseMainName). A token the same
// instance held is replaced in the same transaction, so pairing again just
// works; another live main of the same name is ErrMainNameTaken.
func (s *IntegrationTokenService) CreateMain(ctx context.Context, name, instanceID string) (string, error) {
	if !ValidInstanceID(instanceID) {
		return "", fmt.Errorf("%q is not an instance ID", instanceID)
	}
	if normalised, err := NormaliseMainName(name); err != nil || normalised != name {
		return "", fmt.Errorf("main instance name %q: use 1 to 40 of a-z, 0-9 and -", name)
	}
	token, row, err := newIntegrationToken()
	if err != nil {
		return "", err
	}
	row.Kind, row.Name, row.InstanceID, row.Scopes = IntegrationKindMain, name, instanceID, []string{ScopeHandoff}
	err = s.db.ReplaceMainIntegrationToken(ctx, row)
	if errors.Is(err, store.ErrConflict) {
		return "", fmt.Errorf("%w: %q; unpair it on this worker first, or pair under another name", ErrMainNameTaken, name)
	}
	if err != nil {
		return "", err
	}
	return token, nil
}

// PairedMain is a main instance paired with this worker, as Settings › Main
// instances lists it. LastPullAt is when its token was last used.
type PairedMain struct {
	Name         string  `json:"name"`
	InstanceID   string  `json:"instance_id"`
	PairedAt     string  `json:"paired_at"`
	LastPullAt   *string `json:"last_pull_at,omitempty"`
	OpenHandoffs int     `json:"open_handoffs"`
}

// ListMains lists the main instances paired with this worker, oldest first.
// OpenHandoffs is left at 0: the handoff service counts them.
func (s *IntegrationTokenService) ListMains(ctx context.Context) ([]PairedMain, error) {
	tokens, err := s.db.ListMainIntegrationTokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PairedMain, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, PairedMain{Name: t.Name, InstanceID: t.InstanceID, PairedAt: t.CreatedAt, LastPullAt: t.LastUsedAt})
	}
	return out, nil
}

// RevokeMain unpairs a main instance: its token stops working at once.
func (s *IntegrationTokenService) RevokeMain(ctx context.Context, instanceID string) error {
	err := s.db.RevokeMainIntegrationToken(ctx, instanceID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrMainNotPaired, instanceID)
	}
	return err
}

// List lists every Argus-kind token, revoked ones too. It never holds a
// token. Main instances are listed by ListMains.
func (s *IntegrationTokenService) List(ctx context.Context) ([]store.IntegrationToken, error) {
	return s.db.ListIntegrationTokens(ctx)
}

// Revoke stops an Argus-kind integration's token working at once. A main
// instance is unpaired with RevokeMain.
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
	return IntegrationIdentity{TokenID: t.ID, Kind: t.Kind, Name: t.Name, InstanceID: t.InstanceID, Scopes: t.Scopes}, nil
}

func hashIntegrationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
