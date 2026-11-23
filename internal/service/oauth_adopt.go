package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// AdoptedAuthorization is an authorization Legato holds for an MCP
// connection that did not come from an attempt of this service: the Burnlite
// board's own connect flow (LEG-544), or tokens moved over from the board's
// old storage. Token may hold only a refresh token: the first use refreshes.
type AdoptedAuthorization struct {
	Metadata     oauthclient.Metadata
	ClientID     string
	ClientSecret string
	// Scope is what was granted, else what was asked for.
	Scope string
	Token *oauthclient.Token
}

// Adopt makes auth the connection's active authorization, as Confirm does for
// an attempt: the tokens are sealed before the record names them, and the
// sealed records of the authorization and attempts it supersedes are
// removed. Superseded tokens are not revoked, as on reauthorization. It
// returns the new authorization's ID.
func (s *OAuthFlowService) Adopt(ctx context.Context, connection string, auth AdoptedAuthorization) (string, error) {
	if auth.Token == nil || (auth.Token.AccessToken == "" && auth.Token.RefreshToken == "") {
		return "", errors.New("adopting an authorization needs its tokens")
	}
	if auth.Metadata.TokenEndpoint == "" || auth.ClientID == "" {
		return "", errors.New("adopting an authorization needs its token endpoint and client")
	}
	if s.opts.SecretSource != nil {
		if source, err := s.opts.SecretSource(ctx); err != nil {
			return "", err
		} else if source == "passphrase" {
			return "", ErrOAuthSecretStoreLocked
		}
	}
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return "", err
	}
	token := *auth.Token
	s.rebase(&token)
	id := oauthRandom()
	if err := s.write(ctx, id, oauthFlowSecret{Metadata: auth.Metadata, ClientID: auth.ClientID, ClientSecret: auth.ClientSecret, Scope: auth.Scope, Token: &token}); err != nil {
		return "", err
	}
	now := s.opts.Now().Unix()
	superseded, err := s.db.AdoptOAuthAuthorization(ctx, store.OAuthAttempt{
		ID: id, Connection: connection, InstanceID: instance, Scope: auth.Scope,
		// No browser returns to an adopted authorization: its state and
		// receiver are random values nobody holds.
		StateHash: oauthHash(oauthRandom()), ReceiverHash: oauthHash(oauthRandom()), ExpiresAt: now,
	}, now)
	if err != nil {
		return "", errors.Join(fmt.Errorf("recording the authorization: %w", err), s.remove(ctx, id))
	}
	var problems []error
	for _, prior := range superseded {
		s.stopReceiver(prior.ID)
		if err := s.remove(ctx, prior.ID); err != nil {
			problems = append(problems, err)
		}
	}
	return id, errors.Join(problems...)
}

// Authorization is the scope, time and ID of a connection's active
// authorization; ErrNotFound when it has none.
func (s *OAuthFlowService) Authorization(ctx context.Context, connection string) (scope string, at int64, id string, err error) {
	a, err := s.db.ActiveOAuthAttempt(ctx, connection)
	if err != nil {
		return "", 0, "", err
	}
	scope = a.Scope
	if secret, err := s.read(ctx, a.ID); err == nil {
		if secret.Token != nil && secret.Token.Scope != "" {
			scope = secret.Token.Scope
		} else if secret.Scope != "" {
			scope = secret.Scope
		}
	}
	return scope, a.UpdatedAt, a.ID, nil
}

// AuthorizationMetadata is the server metadata of a connection's active
// authorization; ErrNotFound when it has none.
func (s *OAuthFlowService) AuthorizationMetadata(ctx context.Context, connection string) (oauthclient.Metadata, error) {
	a, err := s.db.ActiveOAuthAttempt(ctx, connection)
	if err != nil {
		return oauthclient.Metadata{}, err
	}
	secret, err := s.read(ctx, a.ID)
	if err != nil {
		return oauthclient.Metadata{}, err
	}
	return secret.Metadata, nil
}

// RevokeAuthorization asks the server to revoke the tokens of the
// connection's active authorization id, through its egress policy, before it
// is superseded; it keeps the authorization. It reports revoked,
// not_supported, failed or none, and refuses (ErrConflict) when id is no
// longer the active one.
func (s *OAuthFlowService) RevokeAuthorization(ctx context.Context, connection, id string) (string, error) {
	a, err := s.db.ActiveOAuthAttempt(ctx, connection)
	if errors.Is(err, store.ErrNotFound) {
		return "", store.ErrConflict
	}
	if err != nil {
		return "", err
	}
	if a.ID != id {
		return "", store.ErrConflict
	}
	client, err := s.refreshClient(ctx, connection)
	if err != nil {
		return "", err
	}
	return s.revoke(ctx, client, a.ID), nil
}
