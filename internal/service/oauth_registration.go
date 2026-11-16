package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// ErrOAuthRegistrationUnsupported is a server Legato has no way to register
// with for this receiver: no pre-registered client, no usable client ID
// metadata document and no dynamic registration.
var ErrOAuthRegistrationUnsupported = errors.New("this server offers no client registration Legato can use; add a pre-registered client under Advanced")

// discover reads the connection's authorization as the current MCP
// specification asks (oauthclient.DiscoverStrict) and decides the scope to
// request: the operator's explicit choice, else the server's challenge, else
// the resource's scopes_supported, else none. offline_access is added when
// the authorization server offers it, so Legato can refresh.
func (s *OAuthFlowService) discover(ctx context.Context, client *http.Client, target MCPAuthorizationTarget, explicit string) (*oauthclient.Metadata, string, error) {
	challenge, _ := oauthclient.Challenge(ctx, client, target.URL)
	md, err := oauthclient.DiscoverStrict(ctx, client, target.URL, oauthclient.StrictOptions{ResourceMetadata: challenge.ResourceMetadata, Issuer: target.Issuer})
	switch {
	case errors.Is(err, egress.ErrDenied):
		return nil, "", errors.New("OAuth discovery reached an address Legato's network policy does not allow")
	case errors.Is(err, oauthclient.ErrNoServerMetadata):
		return nil, "", errors.New("the authorization server publishes no metadata; Legato does not guess its endpoints")
	case err != nil:
		return nil, "", errors.New("OAuth discovery failed: check the address, TLS, the resource and issuer it names, and PKCE S256 support")
	}
	scope := strings.TrimSpace(explicit)
	if scope == "" {
		scope = strings.TrimSpace(challenge.Scope)
	}
	if scope == "" {
		scope = strings.Join(md.ResourceScopes, " ")
	}
	if scope != "" && slices.Contains(md.ServerScopes, "offline_access") && !slices.Contains(strings.Fields(scope), "offline_access") {
		scope += " offline_access"
	}
	return md, scope, nil
}

func redirectPort(redirect string) int {
	u, err := url.Parse(redirect)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(u.Port())
	return port
}

// listen binds the local web receiver: a pre-registered client's exact port
// (failing if it is taken), else the port of an earlier registration for this
// issuer and resource when it is free, so that registration is reused, else
// any free port.
func (s *OAuthFlowService) listen(ctx context.Context, target MCPAuthorizationTarget, md *oauthclient.Metadata, receive func(context.Context, oauthclient.AuthorizationResponse) error) (*oauthclient.LoopbackReceiver, error) {
	if target.StaticClientID != "" {
		r, err := oauthclient.ListenLoopbackAt(ctx, redirectPort(target.StaticRedirectURI), receive)
		if errors.Is(err, oauthclient.ErrPortInUse) {
			return nil, errors.New("the pre-registered callback port is in use; free it and start again")
		}
		return r, err
	}
	earlier, err := s.db.OAuthClientRegistrations(ctx, md.Issuer, md.Resource)
	if err != nil {
		return nil, err
	}
	for _, reg := range earlier {
		if !validOAuthAppRedirect(reg.RedirectURI) || !strings.HasPrefix(reg.RedirectURI, "http://127.0.0.1:") {
			continue
		}
		if r, err := oauthclient.ListenLoopbackAt(ctx, redirectPort(reg.RedirectURI), receive); err == nil {
			return r, nil
		}
	}
	return oauthclient.ListenLoopbackAt(ctx, 0, receive)
}

type oauthClientChoice struct {
	ID, ClientID, ClientSecret string
}

func oauthRegistrationSecretName(id string) string { return "oauth-client-" + id }

// register picks the client for this attempt in the specification's order: a
// pre-registered client, then a client ID metadata document that lists this
// exact redirect (when the server supports them), then an earlier dynamic
// registration for the same issuer, resource and redirect, then a new
// dynamic registration of the receiver's actual redirect.
func (s *OAuthFlowService) register(ctx context.Context, client *http.Client, target MCPAuthorizationTarget, md *oauthclient.Metadata, redirect string) (oauthClientChoice, error) {
	if target.StaticClientID != "" {
		return oauthClientChoice{ClientID: target.StaticClientID, ClientSecret: target.StaticClientSecret}, nil
	}
	if md.CIMDSupported && s.opts.ClientMetadataDocument != "" && documentLists(ctx, client, s.opts.ClientMetadataDocument, redirect) {
		return oauthClientChoice{ClientID: s.opts.ClientMetadataDocument}, nil
	}
	earlier, err := s.db.FindOAuthClientRegistration(ctx, md.Issuer, md.Resource, redirect)
	switch {
	case err == nil:
		choice := oauthClientChoice{ID: earlier.ID, ClientID: earlier.ClientID}
		if earlier.AuthMethod == "client_secret_post" {
			if choice.ClientSecret, err = s.secrets.Read(ctx, oauthRegistrationSecretName(earlier.ID)); err != nil {
				return oauthClientChoice{}, fmt.Errorf("reading the sealed client secret: %w", err)
			}
		}
		return choice, nil
	case !errors.Is(err, store.ErrNotFound):
		return oauthClientChoice{}, err
	}
	if md.RegistrationEndpoint == "" {
		return oauthClientChoice{}, ErrOAuthRegistrationUnsupported
	}
	reg, err := oauthclient.Register(ctx, client, md, oauthclient.RegistrationRequest{ClientName: "Legato", RedirectURI: redirect})
	if err != nil {
		return oauthClientChoice{}, errors.New("OAuth client registration failed: the server may not accept this callback")
	}
	row := store.OAuthClientRegistration{ID: oauthRandom()[:24], Issuer: md.Issuer, Resource: md.Resource, RedirectURI: redirect, ClientID: reg.ClientID, AuthMethod: "none", CreatedAt: s.opts.Now().Unix()}
	if reg.ClientSecret != "" {
		row.AuthMethod = "client_secret_post"
		if err := s.secrets.Write(ctx, oauthRegistrationSecretName(row.ID), reg.ClientSecret); err != nil {
			return oauthClientChoice{}, fmt.Errorf("sealing the client secret: %w", err)
		}
	}
	replaced, err := s.db.SaveOAuthClientRegistration(ctx, row)
	if err != nil {
		return oauthClientChoice{}, errors.Join(err, s.removeSecret(ctx, oauthRegistrationSecretName(row.ID)))
	}
	if replaced != "" {
		if err := s.removeSecret(ctx, oauthRegistrationSecretName(replaced)); err != nil {
			return oauthClientChoice{}, err
		}
	}
	return oauthClientChoice{ID: row.ID, ClientID: reg.ClientID, ClientSecret: reg.ClientSecret}, nil
}

// forgetRegistration drops a registration the server refused (invalid_client),
// so the next attempt registers again.
func (s *OAuthFlowService) forgetRegistration(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return errors.Join(s.db.DeleteOAuthClientRegistration(ctx, id), s.removeSecret(ctx, oauthRegistrationSecretName(id)))
}

func (s *OAuthFlowService) removeSecret(ctx context.Context, name string) error {
	err := s.secrets.Remove(ctx, name)
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	return err
}

// documentLists says whether the client ID metadata document at doc names
// itself as client_id and lists redirect exactly.
func documentLists(ctx context.Context, client *http.Client, doc, redirect string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body) != nil {
		return false
	}
	return body.ClientID == doc && slices.Contains(body.RedirectURIs, redirect)
}

func isInvalidClient(err error) bool {
	var oe *oauthclient.Error
	return errors.As(err, &oe) && oe.Code == "invalid_client"
}
