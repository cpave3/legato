package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// argusRedirect is the redirect Legato registers. Argus matches a loopback
// redirect without its port (RFC 8252 §7.3), so each connect binds a free one.
const argusRedirect = "http://127.0.0.1/callback"

// IntegrationConnector connects Legato to an integration as its OAuth
// client: discovery, registration, the person's consent, and the tokens.
type IntegrationConnector struct {
	db      *store.Store
	secrets IntegrationSecrets
	http    *http.Client
}

func NewIntegrationConnector(db *store.Store, secrets IntegrationSecrets, client *http.Client) *IntegrationConnector {
	if client == nil {
		client = http.DefaultClient
	}
	return &IntegrationConnector{db: db, secrets: secrets, http: client}
}

// PendingConnection waits for the person's consent in their browser.
type PendingConnection struct {
	c        *IntegrationConnector
	name     string
	url      string
	md       *oauthclient.Metadata
	clientID string
	auth     *oauthclient.Authorization
}

// Begin discovers the integration's authorization server, registers Legato
// with it, and prepares the address the person opens to consent.
func (c *IntegrationConnector) Begin(ctx context.Context, name, integrationURL, clientName string) (*PendingConnection, error) {
	md, err := oauthclient.Discover(ctx, c.http, integrationURL)
	if err != nil {
		return nil, fmt.Errorf("finding %s's authorization server: %w", name, err)
	}
	reg, err := oauthclient.Register(ctx, c.http, md, oauthclient.RegistrationRequest{ClientName: clientName, RedirectURI: argusRedirect})
	if err != nil {
		return nil, fmt.Errorf("registering with %s: %w", name, err)
	}
	auth, err := oauthclient.StartAuthorization(md, reg.ClientID, "argus:reply")
	if err != nil {
		return nil, err
	}
	return &PendingConnection{c: c, name: name, url: integrationURL, md: md, clientID: reg.ClientID, auth: auth}, nil
}

// URL is the address the person opens to consent.
func (p *PendingConnection) URL() string { return p.auth.URL() }

// Paste takes the address the browser was sent back to, for a browser on
// another machine whose redirect to 127.0.0.1 could not reach Legato.
func (p *PendingConnection) Paste(redirected string) error { return p.auth.Paste(redirected) }

// Close gives up waiting.
func (p *PendingConnection) Close() { _ = p.auth.Close() }

// Finish waits for consent, exchanges the code for tokens, and keeps the
// refresh token sealed and the registration in the database.
func (p *PendingConnection) Finish(ctx context.Context) error {
	code, err := p.auth.Wait(ctx)
	if err != nil {
		return err
	}
	token, err := oauthclient.Exchange(ctx, p.c.http, p.md, p.clientID, code, p.auth.Verifier(), p.auth.RedirectURI())
	if err != nil {
		return fmt.Errorf("getting tokens from %s: %w", p.name, err)
	}
	if token.RefreshToken == "" {
		return errors.New(p.name + " gave no refresh token, so Legato could not keep the connection")
	}
	if err := p.c.secrets.Write(ctx, ArgusRefreshSecret, token.RefreshToken); err != nil {
		return fmt.Errorf("keeping the refresh token: %w", err)
	}
	return p.c.db.SaveIntegration(ctx, store.Integration{Name: p.name, URL: p.url, Issuer: p.md.Issuer,
		TokenEndpoint: p.md.TokenEndpoint, Resource: p.md.Resource, ClientID: p.clientID})
}

// IntegrationStatus is a connection and its outbox.
type IntegrationStatus struct {
	Name        string         `json:"name"`
	URL         string         `json:"url,omitempty"`
	State       string         `json:"state"`
	ConnectedAt string         `json:"connected_at,omitempty"`
	Replies     map[string]int `json:"replies"`
}

// Status reports a connection; state is none when there is none.
func (c *IntegrationConnector) Status(ctx context.Context, name string) (IntegrationStatus, error) {
	counts, err := c.db.CountCaseReplies(ctx)
	if err != nil {
		return IntegrationStatus{}, err
	}
	st := IntegrationStatus{Name: name, State: "none", Replies: counts}
	in, err := c.db.GetIntegration(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return IntegrationStatus{}, err
	}
	st.URL, st.State, st.ConnectedAt = in.URL, in.State, in.ConnectedAt
	return st, nil
}

// SecretRemover removes a sealed secret.
type SecretRemover interface {
	Remove(ctx context.Context, name string) error
}

// Disconnect forgets the registration and the refresh token.
func (c *IntegrationConnector) Disconnect(ctx context.Context, name string) error {
	if remover, ok := c.secrets.(SecretRemover); ok {
		_ = remover.Remove(ctx, ArgusRefreshSecret)
	}
	if err := c.db.DeleteIntegration(ctx, name); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}
