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
	// secret is set when the server made Legato a confidential client.
	secret string
	auth   *oauthclient.Authorization
}

// ConnectRequest is a connection to make.
type ConnectRequest struct {
	// Name is the integration's ("argus", "burnlite").
	Name string
	// URL is its address, where discovery starts.
	URL        string
	ClientName string
	Scope      string
	// RedirectURI is where the browser comes back to: "" is a loopback
	// listener of Legato's own (the CLI), else an address of Legato's web
	// server, whose handler gives the connection the address (Paste).
	RedirectURI string
}

// BurnliteScope is what Legato asks Burnlite for: reading tasks, since
// Burnlite is a read-only ticket provider.
const BurnliteScope = "read:tasks"

// IntegrationRefreshSecret is the name, in Legato's sealed store, of an
// integration's refresh token.
func IntegrationRefreshSecret(name string) string { return "integration-" + name + "-refresh" }

// IntegrationClientSecret is the name, in Legato's sealed store, of the
// client secret an integration gave at registration. Legato registers as a
// public client, but some servers (Burnlite) give every client a secret and
// want it on each token request.
func IntegrationClientSecret(name string) string { return "integration-" + name + "-client-secret" }

// clientSecret reads an integration's client secret; "" when it gave none.
func clientSecret(ctx context.Context, secrets IntegrationSecrets, name string) string {
	if secrets == nil {
		return ""
	}
	v, err := secrets.Read(ctx, IntegrationClientSecret(name))
	if err != nil {
		return ""
	}
	return v
}

// Begin discovers Argus's authorization server, registers Legato with it,
// and prepares the address the person opens to consent.
func (c *IntegrationConnector) Begin(ctx context.Context, name, integrationURL, clientName string) (*PendingConnection, error) {
	return c.BeginWith(ctx, ConnectRequest{Name: name, URL: integrationURL, ClientName: clientName, Scope: "argus:reply"})
}

// BeginWith discovers an integration's authorization server, registers
// Legato with it, and prepares the address the person opens to consent.
func (c *IntegrationConnector) BeginWith(ctx context.Context, r ConnectRequest) (*PendingConnection, error) {
	md, err := oauthclient.Discover(ctx, c.http, r.URL)
	if err != nil {
		return nil, fmt.Errorf("finding %s's authorization server: %w", r.Name, err)
	}
	// A loopback redirect is registered without its port (RFC 8252 §7.3), so
	// each connect binds a free one.
	registered := r.RedirectURI
	if registered == "" {
		registered = argusRedirect
	}
	reg, err := oauthclient.Register(ctx, c.http, md, oauthclient.RegistrationRequest{ClientName: r.ClientName, RedirectURI: registered})
	if err != nil {
		return nil, fmt.Errorf("registering with %s: %w", r.Name, err)
	}
	var auth *oauthclient.Authorization
	if r.RedirectURI == "" {
		auth, err = oauthclient.StartAuthorization(md, reg.ClientID, r.Scope)
	} else {
		auth, err = oauthclient.StartRedirectAuthorization(md, reg.ClientID, r.Scope, r.RedirectURI)
	}
	if err != nil {
		return nil, err
	}
	return &PendingConnection{c: c, name: r.Name, url: r.URL, md: md, clientID: reg.ClientID, secret: reg.ClientSecret, auth: auth}, nil
}

// Name is the integration being connected.
func (p *PendingConnection) Name() string { return p.name }

// State is the state the browser's return carries, by which a web server's
// callback finds this connection.
func (p *PendingConnection) State() string { return p.auth.State() }

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
	token, err := oauthclient.Exchange(ctx, p.c.http, p.md, p.clientID, code, p.auth.Verifier(), p.auth.RedirectURI(), oauthclient.WithClientSecret(p.secret))
	if err != nil {
		return fmt.Errorf("getting tokens from %s: %w", p.name, err)
	}
	if token.RefreshToken == "" {
		return errors.New(p.name + " gave no refresh token, so Legato could not keep the connection")
	}
	if err := p.c.secrets.Write(ctx, IntegrationRefreshSecret(p.name), token.RefreshToken); err != nil {
		return fmt.Errorf("keeping the refresh token: %w", err)
	}
	if p.secret != "" {
		if err := p.c.secrets.Write(ctx, IntegrationClientSecret(p.name), p.secret); err != nil {
			return fmt.Errorf("keeping the client secret: %w", err)
		}
	} else if remover, ok := p.c.secrets.(SecretRemover); ok {
		// One kept from an earlier registration is not this client's.
		_ = remover.Remove(ctx, IntegrationClientSecret(p.name))
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
		_ = remover.Remove(ctx, IntegrationRefreshSecret(name))
		_ = remover.Remove(ctx, IntegrationClientSecret(name))
	}
	if err := c.db.DeleteIntegration(ctx, name); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}
