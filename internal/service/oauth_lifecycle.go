package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

var (
	// ErrOAuthNotAuthorized is a connection with no confirmed authorization.
	ErrOAuthNotAuthorized = errors.New("this MCP connection is not authorized; connect it in Settings")
	// ErrOAuthReauthRequired is an authorization that ended: its refresh
	// token was refused, or it expired with none. Only the operator can
	// connect it again; an agent's call never starts consent.
	ErrOAuthReauthRequired = errors.New("Legato's authorization for this MCP server ended; connect it again in Settings")
	// ErrOAuthRefreshUnavailable is a refresh that could not reach the
	// server, or waited too long for another process's refresh. The
	// authorization is kept.
	ErrOAuthRefreshUnavailable = errors.New("Legato could not refresh its authorization for this MCP server; try again shortly")
)

const (
	oauthReceiverLease  = 60 * time.Second
	oauthRefreshLease   = 30 * time.Second
	oauthRefreshWait    = 15 * time.Second
	oauthExpirySkew     = 60 * time.Second
	oauthExchangeStuck  = 2 * time.Minute
	oauthAttemptRetains = 7 * 24 * time.Hour
)

// OAuthEvidence is what the operator checks before confirming an
// authorization. Account is empty unless the server named it in a standard
// way; Legato does not invent introspection.
type OAuthEvidence struct {
	Issuer         string     `json:"issuer"`
	Resource       string     `json:"resource"`
	RequestedScope string     `json:"requested_scope,omitempty"`
	GrantedScope   string     `json:"granted_scope,omitempty"`
	Refreshable    bool       `json:"refreshable"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Account        string     `json:"account,omitempty"`
}

// OAuthDisconnect says what a disconnect did upstream: revoked,
// not_supported (the server offers no revocation; the token may stay valid
// there until it expires), failed, or none (Legato held no token).
type OAuthDisconnect struct {
	Upstream string `json:"upstream"`
}

// rebase moves a token's expiry onto this service's clock: oauthclient
// computes it from the wall clock.
func (s *OAuthFlowService) rebase(tok *oauthclient.Token) {
	if !tok.ExpiresAt.IsZero() {
		tok.ExpiresAt = s.opts.Now().Add(time.Until(tok.ExpiresAt))
	}
}

func (s *OAuthFlowService) fresh(tok *oauthclient.Token) bool {
	return tok != nil && tok.AccessToken != "" && (tok.ExpiresAt.IsZero() || s.opts.Now().Add(oauthExpirySkew).Before(tok.ExpiresAt))
}

// AccessToken is the host broker's access to a connection's current access
// token, refreshing it when it is about to expire. Only one process refreshes
// a connection at a time (a lease in the database); the rotated refresh token
// is sealed before the new access token is returned, and a refresh that
// finishes after a disconnect or a newer authorization is discarded. It never
// starts consent. Agents never call it: they reach upstream tools only
// through the broker.
func (s *OAuthFlowService) AccessToken(ctx context.Context, connection string) (string, error) {
	return s.accessToken(ctx, connection, false)
}

// RefreshAccessToken refreshes even a token that looks fresh: the server
// refused it.
func (s *OAuthFlowService) RefreshAccessToken(ctx context.Context, connection string) (string, error) {
	return s.accessToken(ctx, connection, true)
}

func (s *OAuthFlowService) accessToken(ctx context.Context, connection string, force bool) (string, error) {
	// The connection's egress policy whether or not it is turned on: the
	// switch is about agents (the broker refuses calls on a connection that
	// is off), and the Burnlite board shares the authorization (LEG-544).
	client, err := s.refreshClient(ctx, connection)
	if err != nil {
		return "", err
	}
	deadline := time.Now().Add(oauthRefreshWait)
	// refused is the token the server turned down, when forced: one another
	// process has replaced since is good.
	refused := ""
	for {
		a, err := s.db.ActiveOAuthAttempt(ctx, connection)
		if errors.Is(err, store.ErrNotFound) {
			if cur, err := s.db.CurrentOAuthAttempt(ctx, connection); err == nil && cur.Status == "reauth_required" {
				return "", ErrOAuthReauthRequired
			}
			return "", ErrOAuthNotAuthorized
		}
		if err != nil {
			return "", err
		}
		secret, err := s.read(ctx, a.ID)
		if err != nil {
			return "", err
		}
		if force && refused == "" && secret.Token != nil {
			refused = secret.Token.AccessToken
		}
		if s.fresh(secret.Token) && (!force || secret.Token.AccessToken != refused) {
			return secret.Token.AccessToken, nil
		}
		if secret.Token == nil || secret.Token.RefreshToken == "" {
			return "", s.endAuthorization(ctx, a, "token_expired")
		}
		now := s.opts.Now()
		err = s.db.ClaimOAuthRefresh(ctx, connection, a.ID, s.holder, now.Unix(), now.Add(oauthRefreshLease).Unix())
		if errors.Is(err, store.ErrConflict) {
			// Another process is refreshing: wait for its result.
			if time.Now().After(deadline) {
				return "", ErrOAuthRefreshUnavailable
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		if err != nil {
			return "", err
		}
		token, err := s.refresh(ctx, client, a, refused, force)
		_ = s.db.ReleaseOAuthRefresh(context.WithoutCancel(ctx), connection, s.holder)
		return token, err
	}
}

// refreshClient is the HTTP client for refreshing and revoking a
// connection's tokens: its egress policy, enabled or not.
func (s *OAuthFlowService) refreshClient(ctx context.Context, connection string) (*http.Client, error) {
	if s.opts.Connections == nil {
		return nil, errors.New("OAuth connections need the MCP connection registry")
	}
	policy, err := s.opts.Connections.EgressPolicy(ctx, connection)
	if err != nil {
		return nil, err
	}
	return policy.Client(), nil
}

// refresh runs under the connection's refresh lease.
func (s *OAuthFlowService) refresh(ctx context.Context, client *http.Client, a store.OAuthAttempt, refused string, force bool) (string, error) {
	// Another process may have refreshed between our read and our claim.
	secret, err := s.read(ctx, a.ID)
	if err != nil {
		return "", err
	}
	if s.fresh(secret.Token) && (!force || secret.Token.AccessToken != refused) {
		return secret.Token.AccessToken, nil
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	tok, err := oauthclient.Refresh(work, client, &secret.Metadata, secret.ClientID, secret.Token.RefreshToken, oauthclient.WithClientSecret(secret.ClientSecret))
	switch {
	case errors.Is(err, oauthclient.ErrInvalidGrant):
		return "", s.endAuthorization(work, a, "refresh_refused")
	case isInvalidClient(err):
		return "", errors.Join(s.endAuthorization(work, a, "client_refused"), s.forgetRegistration(work, secret.RegistrationID))
	case err != nil:
		return "", ErrOAuthRefreshUnavailable
	}
	s.rebase(tok)
	secret.Token = tok
	// Durable before published: the rotated refresh token is sealed first.
	if err := s.write(work, a.ID, secret); err != nil {
		return "", errors.Join(ErrOAuthRefreshUnavailable, err)
	}
	// Fence: a disconnect or a newer authorization during the refresh wins.
	if current, err := s.db.ActiveOAuthAttempt(work, a.Connection); err != nil || current.ID != a.ID {
		return "", errors.Join(ErrOAuthNotAuthorized, s.remove(work, a.ID))
	}
	return tok.AccessToken, nil
}

// endAuthorization records that the operator must connect again and drops
// the dead tokens.
func (s *OAuthFlowService) endAuthorization(ctx context.Context, a store.OAuthAttempt, code string) error {
	err := s.db.RequireOAuthReauthorization(ctx, a.Connection, a.ID, code, s.opts.Now().Unix())
	if err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return errors.Join(ErrOAuthReauthRequired, s.remove(ctx, a.ID))
}

// revoke asks the server to revoke what an attempt holds, the refresh token
// if there is one. It reports revoked, not_supported, failed or none.
func (s *OAuthFlowService) revoke(ctx context.Context, client *http.Client, id string) string {
	secret, err := s.read(ctx, id)
	if err != nil || secret.Token == nil {
		return "none"
	}
	token, hint := secret.Token.RefreshToken, "refresh_token"
	if token == "" {
		token, hint = secret.Token.AccessToken, "access_token"
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	switch err := oauthclient.Revoke(work, client, &secret.Metadata, secret.ClientID, token, hint, oauthclient.WithClientSecret(secret.ClientSecret)); {
	case err == nil:
		return "revoked"
	case errors.Is(err, oauthclient.ErrRevocationUnsupported):
		return "not_supported"
	default:
		return "failed"
	}
}

// worst keeps the outcome the operator most needs to hear about.
func worst(a, b string) string {
	rank := map[string]int{"": 0, "none": 1, "revoked": 2, "not_supported": 3, "failed": 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// forgetAttempts revokes upstream what fenced attempts held (best effort,
// through the connection's policy), stops their receivers and removes their
// sealed records.
func (s *OAuthFlowService) forgetAttempts(ctx context.Context, attempts []store.OAuthAttempt, policy egress.Policy) (OAuthDisconnect, error) {
	result := OAuthDisconnect{Upstream: "none"}
	client := policy.Client()
	var problems []error
	for _, a := range attempts {
		s.stopReceiver(a.ID)
		result.Upstream = worst(result.Upstream, s.revoke(ctx, client, a.ID))
		if err := s.remove(ctx, a.ID); err != nil {
			problems = append(problems, err)
		}
	}
	return result, errors.Join(problems...)
}

// ForgetAttempts is forgetAttempts for the registry, which has fenced them.
func (s *OAuthFlowService) ForgetAttempts(ctx context.Context, attempts []store.OAuthAttempt, policy egress.Policy) error {
	_, err := s.forgetAttempts(ctx, attempts, policy)
	return err
}

// Disconnect is an operator operation. It fences new calls and attempts
// first, then revokes upstream where the server supports it, then removes
// the sealed records. Local removal is not proof of upstream revocation: the
// result says what happened there.
func (s *OAuthFlowService) Disconnect(ctx context.Context, connection string) (OAuthDisconnect, error) {
	if s.opts.Connections == nil {
		return OAuthDisconnect{}, errors.New("OAuth connections need the MCP connection registry")
	}
	policy, err := s.opts.Connections.EgressPolicy(ctx, connection)
	if err != nil {
		return OAuthDisconnect{}, err
	}
	attempts, err := s.db.InvalidateOAuthConnection(ctx, connection, s.opts.Now().Unix())
	if err != nil {
		return OAuthDisconnect{}, err
	}
	return s.forgetAttempts(ctx, attempts, policy)
}

// Sweep settles abandoned attempts (expired, exchanges a crash cut short,
// web receivers whose process is gone), removes the sealed records of every
// finished attempt and deletes finished attempts past retention. Run it at
// start and periodically.
func (s *OAuthFlowService) Sweep(ctx context.Context) error {
	now := s.opts.Now()
	finished, err := s.db.SweepOAuthAttempts(ctx, store.OAuthSweep{Now: now.Unix(), StuckBefore: now.Add(-oauthExchangeStuck).Unix(), RetainBefore: now.Add(-oauthAttemptRetains).Unix()})
	if err != nil {
		return err
	}
	var problems []error
	for _, id := range finished {
		s.stopReceiver(id)
		if err := s.remove(ctx, id); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// renewLeases keeps this process's web receivers' leases alive until Close.
func (s *OAuthFlowService) renewLeases() {
	ticker := time.NewTicker(oauthReceiverLease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-s.leaseStop:
			return
		case <-ticker.C:
			_ = s.db.RenewOAuthReceiverLeases(context.Background(), s.holder, s.opts.Now().Add(oauthReceiverLease).Unix())
		}
	}
}

func (s *OAuthFlowService) stopLeases() { s.leaseOnce.Do(func() { close(s.leaseStop) }) }

// evidence is what an attempt's sealed record says about the authorization.
func (s *OAuthFlowService) evidence(ctx context.Context, id string) *OAuthEvidence {
	secret, err := s.read(ctx, id)
	if err != nil || secret.Token == nil {
		return nil
	}
	ev := &OAuthEvidence{Issuer: secret.Metadata.Issuer, Resource: secret.Metadata.Resource, RequestedScope: secret.Scope, GrantedScope: secret.Token.Scope, Refreshable: secret.Token.RefreshToken != ""}
	if !secret.Token.ExpiresAt.IsZero() {
		at := secret.Token.ExpiresAt.UTC()
		ev.ExpiresAt = &at
	}
	return ev
}
