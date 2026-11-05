package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrNotConnected: Legato has no working connection to an integration; the
// person connects it again.
var ErrNotConnected = errors.New("not connected")

// OAuthTokens is the access token of a connected integration: refreshed
// when it runs out or is refused, the rotated refresh token kept first. When
// the integration refuses the refresh token, the connection is marked
// disconnected, which Attention reports.
type OAuthTokens struct {
	db      *store.Store
	secrets IntegrationSecrets
	http    *http.Client
	name    string
	now     func() time.Time

	mu        sync.Mutex
	access    string
	expiresAt time.Time
}

// NewOAuthTokens is the tokens of the integration called name. client may be nil.
func NewOAuthTokens(db *store.Store, secrets IntegrationSecrets, client *http.Client, name string) *OAuthTokens {
	if client == nil {
		client = http.DefaultClient
	}
	return &OAuthTokens{db: db, secrets: secrets, http: client, name: name, now: time.Now}
}

// Connected says whether the integration is connected now.
func (t *OAuthTokens) Connected(ctx context.Context) bool {
	in, err := t.db.GetIntegration(ctx, t.name)
	return err == nil && in.State == store.IntegrationConnected
}

// Token is the access token in hand, or a new one. force refreshes even when
// the one in hand has not run out.
func (t *OAuthTokens) Token(ctx context.Context, force bool) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !force && t.access != "" && t.now().Add(time.Minute).Before(t.expiresAt) {
		return t.access, nil
	}
	in, err := t.db.GetIntegration(ctx, t.name)
	if errors.Is(err, store.ErrNotFound) || (err == nil && in.State != store.IntegrationConnected) {
		return "", fmt.Errorf("Legato is not connected to %s: %w", t.name, ErrNotConnected)
	}
	if err != nil {
		return "", err
	}
	secret := IntegrationRefreshSecret(t.name)
	md := &oauthclient.Metadata{Issuer: in.Issuer, TokenEndpoint: in.TokenEndpoint, Resource: in.Resource}
	refresh, err := t.secrets.Read(ctx, secret)
	if err != nil {
		return "", fmt.Errorf("reading %s's refresh token: %w", t.name, err)
	}
	withSecret := oauthclient.WithClientSecret(clientSecret(ctx, t.secrets, t.name))
	token, err := oauthclient.Refresh(ctx, t.http, md, in.ClientID, refresh, withSecret)
	if errors.Is(err, oauthclient.ErrInvalidGrant) {
		// Another process may have rotated it since it was read.
		if again, readErr := t.secrets.Read(ctx, secret); readErr == nil && again != refresh {
			refresh = again
			token, err = oauthclient.Refresh(ctx, t.http, md, in.ClientID, again, withSecret)
		}
	}
	if errors.Is(err, oauthclient.ErrInvalidGrant) {
		t.access = ""
		if err := t.db.SetIntegrationState(ctx, t.name, store.IntegrationDisconnected); err != nil {
			log.Printf("integration %s: %v", t.name, err)
		}
		log.Printf("integration %s disconnected: it refused Legato's refresh token", t.name)
		return "", fmt.Errorf("%s refused Legato's refresh token: %w", t.name, ErrNotConnected)
	}
	if err != nil {
		return "", err
	}
	// The rotated token is kept before the new access token is used.
	if token.RefreshToken != "" && token.RefreshToken != refresh {
		if err := t.secrets.Write(ctx, secret, token.RefreshToken); err != nil {
			return "", fmt.Errorf("keeping %s's new refresh token: %w", t.name, err)
		}
	}
	lifetime := 5 * time.Minute
	if !token.ExpiresAt.IsZero() {
		lifetime = time.Until(token.ExpiresAt)
	}
	t.access, t.expiresAt = token.AccessToken, t.now().Add(lifetime)
	return t.access, nil
}
