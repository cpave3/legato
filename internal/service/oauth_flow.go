package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

var (
	ErrOAuthFlowInput   = errors.New("invalid OAuth connection or receiver")
	ErrOAuthFlowRefused = errors.New("OAuth attempt is unavailable, expired or belongs to another receiver")
)

// OAuthFlowSecrets is implemented by the existing host sealed store adapter.
// OAuth values must not be exposed through agent credential read/exec operations.
type OAuthFlowSecrets interface {
	IntegrationSecrets
	Remove(context.Context, string) error
}
type OAuthFlowOptions struct {
	HTTPClient   *http.Client
	Now          func() time.Time
	Lifetime     time.Duration
	SecretSource func(context.Context) (string, error)
	// Connections, when set, is the registry an attempt must name: the
	// resource address and pinned issuer come from it, never the request,
	// and the attempt is fenced by the connection's revision.
	Connections OAuthFlowConnections
}

// OAuthFlowConnections resolves a connection name to what may be authorized.
type OAuthFlowConnections interface {
	AuthorizationTarget(ctx context.Context, name string) (MCPAuthorizationTarget, error)
}

// OAuthFlowRequest's Owner is supplied by the authenticated host handler, never
// decoded from an operator's request body. An app can only use a loopback profile
// until a different native callback profile has been explicitly implemented.
type OAuthFlowRequest struct{ Connection, URL, Scope, Owner, Mode, RedirectURI string }
type OAuthFlowBegin struct {
	ID           string `json:"id"`
	AuthorizeURL string `json:"authorize_url"`
	ReturnKey    string `json:"return_key,omitempty"`
}
type OAuthFlowStatus struct {
	ID         string `json:"id"`
	Connection string `json:"connection"`
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	ExpiresAt  int64  `json:"expires_at"`
}

type oauthFlowSecret struct {
	Metadata                                             oauthclient.Metadata
	ClientID, ClientSecret, State, Verifier, RedirectURI string
	Token                                                *oauthclient.Token
}
type oauthLocalReceiver struct {
	connection, owner string
	cancel            context.CancelFunc
	receiver          *oauthclient.LoopbackReceiver
}

// OAuthFlowService keeps authorization response delivery separate from account
// activation. It is host-only; callers must supply a policy-constrained HTTP
// client before exposing arbitrary upstream connections through an API.
type OAuthFlowService struct {
	db        *store.Store
	secrets   OAuthFlowSecrets
	opts      OAuthFlowOptions
	mu        sync.Mutex
	receivers map[string]oauthLocalReceiver
	closed    bool
}

func NewOAuthFlowService(db *store.Store, secrets OAuthFlowSecrets, opts OAuthFlowOptions) *OAuthFlowService {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Lifetime == 0 {
		opts.Lifetime = 10 * time.Minute
	}
	return &OAuthFlowService{db: db, secrets: secrets, opts: opts, receivers: map[string]oauthLocalReceiver{}}
}
func oauthHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func oauthEqualHash(value, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(oauthHash(value)), []byte(hash)) == 1
}
func oauthRandom() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
func oauthFlowSecretName(id string) string { return "oauth-attempt-" + id }

func validOAuthAppRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "http" || u.Path != "/callback" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	return ip != nil && ip.IsLoopback() && err == nil && port > 0 && port <= 65535
}

func (s *OAuthFlowService) Begin(ctx context.Context, r OAuthFlowRequest) (OAuthFlowBegin, error) {
	if r.Owner == "" || r.Connection == "" || len(r.Connection) > 100 || (r.Mode != "web" && r.Mode != "app") || (r.Mode == "web" && r.RedirectURI != "") || (r.Mode == "app" && !validOAuthAppRedirect(r.RedirectURI)) {
		return OAuthFlowBegin{}, ErrOAuthFlowInput
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return OAuthFlowBegin{}, ErrOAuthFlowRefused
	}
	if s.opts.SecretSource != nil {
		source, err := s.opts.SecretSource(ctx)
		if err != nil {
			return OAuthFlowBegin{}, err
		}
		if source == "passphrase" {
			return OAuthFlowBegin{}, errors.New("OAuth requires a host secret store that can be unlocked without an interactive passphrase")
		}
	}
	var target MCPAuthorizationTarget
	client := s.opts.HTTPClient
	if s.opts.Connections != nil {
		if r.URL != "" {
			return OAuthFlowBegin{}, ErrOAuthFlowInput
		}
		var err error
		if target, err = s.opts.Connections.AuthorizationTarget(ctx, r.Connection); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return OAuthFlowBegin{}, ErrOAuthFlowInput
			}
			return OAuthFlowBegin{}, err
		}
		r.URL = target.URL
		// Every hop (resource and authorization server metadata,
		// registration, token) goes through the connection's policy.
		client = target.Egress.Client()
	}
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return OAuthFlowBegin{}, err
	}
	id, key := oauthRandom(), oauthRandom()
	expires := s.opts.Now().Add(s.opts.Lifetime)
	var local oauthLocalReceiver
	ready := make(chan struct{})
	if r.Mode == "web" {
		receiverCtx, cancel := context.WithTimeout(context.Background(), s.opts.Lifetime)
		local = oauthLocalReceiver{connection: r.Connection, owner: r.Owner, cancel: cancel}
		local.receiver, err = oauthclient.ListenLoopback(receiverCtx, func(ctx context.Context, response oauthclient.AuthorizationResponse) error {
			select {
			case <-ready:
			default:
				return ErrOAuthFlowRefused
			}
			_, err := s.Deliver(ctx, id, r.Owner, key, response)
			return err
		})
		if err != nil {
			cancel()
			return OAuthFlowBegin{}, err
		}
		r.RedirectURI = local.receiver.RedirectURI()
	}
	keep := false
	defer func() {
		if !keep && local.cancel != nil {
			local.cancel()
			_ = local.receiver.Close()
		}
	}()
	md, err := oauthclient.Discover(ctx, client, r.URL)
	if err != nil {
		return OAuthFlowBegin{}, errors.New("OAuth discovery failed: check the endpoint, TLS, issuer and PKCE S256 metadata")
	}
	if target.Issuer != "" && md.Issuer != target.Issuer {
		return OAuthFlowBegin{}, errors.New("OAuth discovery found an issuer other than the one this connection pins")
	}
	// The new flow is strict even while existing integration CLI callers migrate.
	s256 := false
	for _, method := range md.CodeChallengeMethods {
		if method == "S256" {
			s256 = true
		}
	}
	if !s256 {
		return OAuthFlowBegin{}, errors.New("OAuth provider must advertise PKCE S256 support")
	}
	reg, err := oauthclient.Register(ctx, client, md, oauthclient.RegistrationRequest{ClientName: "Legato", RedirectURI: r.RedirectURI})
	if err != nil {
		return OAuthFlowBegin{}, errors.New("OAuth client registration failed: check provider registration and callback support")
	}
	auth, err := oauthclient.StartRedirectAuthorization(md, reg.ClientID, r.Scope, r.RedirectURI)
	if err != nil {
		return OAuthFlowBegin{}, err
	}
	defer auth.Close()
	secret := oauthFlowSecret{Metadata: *md, ClientID: reg.ClientID, ClientSecret: reg.ClientSecret, State: auth.State(), Verifier: auth.Verifier(), RedirectURI: r.RedirectURI}
	if err := s.write(ctx, id, secret); err != nil {
		return OAuthFlowBegin{}, err
	}
	_, err = s.db.BeginOAuthAttempt(ctx, store.OAuthAttempt{ID: id, Connection: r.Connection, Owner: oauthHash(r.Owner), InstanceID: instance, ReceiverHash: oauthHash(key), StateHash: oauthHash(auth.State()), ExpiresAt: expires.Unix(), ConnectionRevision: target.Revision}, s.opts.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		err = errors.Join(ErrOAuthFlowRefused, errors.New("the connection changed while authorization was starting; start again"))
	}
	if err != nil {
		return OAuthFlowBegin{}, errors.Join(err, s.remove(ctx, id))
	}
	if local.cancel != nil {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			endErr := s.db.EndOAuthAttempt(ctx, id, oauthHash(r.Owner), "interrupted", "receiver_closed", s.opts.Now().Unix())
			if errors.Is(endErr, store.ErrConflict) {
				endErr = nil
			}
			return OAuthFlowBegin{}, errors.Join(ErrOAuthFlowRefused, endErr, s.remove(ctx, id))
		}
		s.receivers[id] = local
		s.mu.Unlock()
		attempt, err := s.db.GetOAuthAttempt(ctx, id)
		if err != nil || attempt.Status != "pending" {
			s.stopReceiver(id)
			return OAuthFlowBegin{}, errors.Join(ErrOAuthFlowRefused, err, s.remove(ctx, id))
		}
	}
	close(ready)
	keep = true
	result := OAuthFlowBegin{ID: id, AuthorizeURL: auth.URL()}
	if r.Mode == "app" {
		result.ReturnKey = key
	}
	return result, nil
}

func (s *OAuthFlowService) write(ctx context.Context, id string, secret oauthFlowSecret) error {
	b, err := json.Marshal(secret)
	if err != nil {
		return err
	}
	if err := s.secrets.Write(ctx, oauthFlowSecretName(id), string(b)); err != nil {
		return fmt.Errorf("saving host OAuth credentials: %w", err)
	}
	return nil
}
func (s *OAuthFlowService) read(ctx context.Context, id string) (oauthFlowSecret, error) {
	raw, err := s.secrets.Read(ctx, oauthFlowSecretName(id))
	if err != nil {
		return oauthFlowSecret{}, fmt.Errorf("reading host OAuth credentials: %w", err)
	}
	var secret oauthFlowSecret
	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		return oauthFlowSecret{}, errors.New("stored OAuth credentials are not valid; reconnect")
	}
	return secret, nil
}
func (s *OAuthFlowService) remove(ctx context.Context, id string) error {
	err := s.secrets.Remove(ctx, oauthFlowSecretName(id))
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	return err
}
func (s *OAuthFlowService) owned(ctx context.Context, id, owner string) (store.OAuthAttempt, error) {
	a, err := s.db.GetOAuthAttempt(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.OAuthAttempt{}, ErrOAuthFlowRefused
		}
		return store.OAuthAttempt{}, err
	}
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return store.OAuthAttempt{}, err
	}
	if owner == "" || !oauthEqualHash(owner, a.Owner) || instance != a.InstanceID {
		return store.OAuthAttempt{}, ErrOAuthFlowRefused
	}
	return a, nil
}
func flowStatus(a store.OAuthAttempt) OAuthFlowStatus {
	return OAuthFlowStatus{ID: a.ID, Connection: a.Connection, Status: a.Status, ErrorCode: a.ErrorCode, ExpiresAt: a.ExpiresAt}
}

// Deliver is called by a native app over its authenticated pinned connection or
// by the host-owned loopback receiver. The capability alone is not authority.
func (s *OAuthFlowService) Deliver(ctx context.Context, id, owner, key string, response oauthclient.AuthorizationResponse) (OAuthFlowStatus, error) {
	a, err := s.owned(ctx, id, owner)
	if err != nil {
		return OAuthFlowStatus{}, err
	}
	if key == "" || !oauthEqualHash(key, a.ReceiverHash) || !oauthEqualHash(response.State, a.StateHash) {
		return OAuthFlowStatus{}, ErrOAuthFlowRefused
	}
	if a.Status != "pending" {
		return s.Status(ctx, id, owner)
	}
	if a.ExpiresAt <= s.opts.Now().Unix() {
		return s.Status(ctx, id, owner)
	}
	secret, err := s.read(ctx, id)
	if err != nil {
		return OAuthFlowStatus{}, err
	}
	if err := response.Check(secret.State, secret.Metadata.Issuer, secret.Metadata.ResponseIssuerSupported); err != nil {
		return OAuthFlowStatus{}, err
	}
	if err := s.db.ClaimOAuthAttempt(ctx, id, s.opts.Now().Unix()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return s.Status(ctx, id, owner)
		}
		return OAuthFlowStatus{}, err
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	if response.Error != "" {
		err := s.db.EndOAuthAttempt(work, id, a.Owner, "denied", "authorization_denied", s.opts.Now().Unix())
		if err != nil {
			return OAuthFlowStatus{}, err
		}
		if err := s.remove(work, id); err != nil {
			return OAuthFlowStatus{}, err
		}
		s.closeReceiverAfterReturn(id)
		return s.Status(work, id, owner)
	}
	client, err := s.clientFor(work, a.Connection)
	if err != nil {
		return OAuthFlowStatus{}, s.failConsumedAttempt(work, a, "connection_unavailable", err)
	}
	token, err := oauthclient.Exchange(work, client, &secret.Metadata, secret.ClientID, response.Code, secret.Verifier, secret.RedirectURI, oauthclient.WithClientSecret(secret.ClientSecret))
	if err != nil {
		finishErr := s.db.EndOAuthAttempt(work, id, a.Owner, "failed", "token_exchange_failed", s.opts.Now().Unix())
		cleanupErr := s.remove(work, id)
		s.closeReceiverAfterReturn(id)
		if finishErr != nil || cleanupErr != nil {
			return OAuthFlowStatus{}, errors.Join(finishErr, cleanupErr)
		}
		return s.Status(work, id, owner)
	}
	secret.State, secret.Verifier = "", ""
	secret.Token = token
	if err := s.write(work, id, secret); err != nil {
		return OAuthFlowStatus{}, s.failConsumedAttempt(work, a, "token_storage_failed", err)
	}
	if err := s.db.StageOAuthAttempt(work, id, s.opts.Now().Unix()); err != nil {
		return OAuthFlowStatus{}, s.failConsumedAttempt(work, a, "token_activation_failed", err)
	}
	s.closeReceiverAfterReturn(id)
	return s.Status(work, id, owner)
}

// clientFor is the HTTP client for a connection's requests: its egress
// policy when the registry is in use.
func (s *OAuthFlowService) clientFor(ctx context.Context, connection string) (*http.Client, error) {
	if s.opts.Connections == nil {
		return s.opts.HTTPClient, nil
	}
	target, err := s.opts.Connections.AuthorizationTarget(ctx, connection)
	if err != nil {
		return nil, err
	}
	return target.Egress.Client(), nil
}

func (s *OAuthFlowService) failConsumedAttempt(ctx context.Context, a store.OAuthAttempt, code string, cause error) error {
	err := s.db.EndOAuthAttempt(ctx, a.ID, a.Owner, "failed", code, s.opts.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		err = nil
	}
	s.closeReceiverAfterReturn(a.ID)
	return errors.Join(cause, err, s.remove(ctx, a.ID))
}

func (s *OAuthFlowService) Status(ctx context.Context, id, owner string) (OAuthFlowStatus, error) {
	a, err := s.owned(ctx, id, owner)
	if err != nil {
		return OAuthFlowStatus{}, err
	}
	if a.ExpiresAt <= s.opts.Now().Unix() && (a.Status == "pending" || a.Status == "exchanging" || a.Status == "awaiting_confirmation") {
		err := s.db.EndOAuthAttempt(ctx, id, a.Owner, "expired", "authorization_expired", s.opts.Now().Unix())
		if err != nil && !errors.Is(err, store.ErrConflict) {
			return OAuthFlowStatus{}, err
		}
		a, err = s.owned(ctx, id, owner)
		if err != nil {
			return OAuthFlowStatus{}, err
		}
		if a.Status == "expired" {
			if err := s.remove(ctx, id); err != nil {
				return OAuthFlowStatus{}, err
			}
			s.stopReceiver(id)
		}
	}
	return flowStatus(a), nil
}
func (s *OAuthFlowService) Confirm(ctx context.Context, id, owner string) error {
	a, err := s.owned(ctx, id, owner)
	if err != nil {
		return err
	}
	if a.Status != "awaiting_confirmation" || a.ExpiresAt <= s.opts.Now().Unix() {
		return ErrOAuthFlowRefused
	}
	secret, err := s.read(ctx, id)
	if err != nil {
		return err
	}
	if secret.Token == nil || secret.Token.AccessToken == "" {
		return ErrOAuthFlowRefused
	}
	if err := s.db.ActivateOAuthAttempt(ctx, id, a.Owner, s.opts.Now().Unix()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrOAuthFlowRefused
		}
		return err
	}
	attempts, err := s.db.ListOAuthAttempts(ctx, a.Connection)
	if err != nil {
		return err
	}
	var problems []error
	for _, prior := range attempts {
		if prior.Status == "superseded" {
			s.stopReceiver(prior.ID)
			if err := s.remove(ctx, prior.ID); err != nil {
				problems = append(problems, err)
			}
		}
	}
	return errors.Join(problems...)
}

// Cancel ends an unfinished attempt without changing an active connection.
func (s *OAuthFlowService) Cancel(ctx context.Context, id, owner string) error {
	a, err := s.owned(ctx, id, owner)
	if err != nil {
		return err
	}
	if err := s.db.EndOAuthAttempt(ctx, id, a.Owner, "cancelled", "authorization_cancelled", s.opts.Now().Unix()); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrOAuthFlowRefused
		}
		return err
	}
	s.stopReceiver(id)
	return s.remove(ctx, id)
}

func (s *OAuthFlowService) stopReceiver(id string) {
	s.mu.Lock()
	local, ok := s.receivers[id]
	delete(s.receivers, id)
	s.mu.Unlock()
	if ok {
		local.cancel()
		_ = local.receiver.Close()
	}
}
func (s *OAuthFlowService) closeReceiverAfterReturn(id string) {
	time.AfterFunc(2*time.Second, func() { s.stopReceiver(id) })
}
func (s *OAuthFlowService) Close() error {
	s.mu.Lock()
	s.closed = true
	receivers := s.receivers
	s.receivers = map[string]oauthLocalReceiver{}
	s.mu.Unlock()
	var problems []error
	for id, local := range receivers {
		local.cancel()
		if err := local.receiver.Close(); err != nil {
			problems = append(problems, err)
		}
		a, err := s.db.GetOAuthAttempt(context.Background(), id)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if a.Status == "pending" || a.Status == "exchanging" {
			err := s.db.EndOAuthAttempt(context.Background(), id, a.Owner, "interrupted", "receiver_closed", s.opts.Now().Unix())
			if err == nil {
				if err := s.remove(context.Background(), id); err != nil {
					problems = append(problems, err)
				}
			} else if !errors.Is(err, store.ErrConflict) {
				problems = append(problems, err)
			}
		}
	}
	return errors.Join(problems...)
}

// Disconnect is an operator/host operation. Invalidation happens before secret
// cleanup so a late delivery cannot recreate an active connection.
func (s *OAuthFlowService) Disconnect(ctx context.Context, connection string) error {
	attempts, err := s.db.InvalidateOAuthConnection(ctx, connection, s.opts.Now().Unix())
	if err != nil {
		return err
	}
	return s.ForgetAttempts(ctx, attempts)
}

// ForgetAttempts stops the receivers of attempts that have already been
// fenced in the store and removes their sealed records.
func (s *OAuthFlowService) ForgetAttempts(ctx context.Context, attempts []store.OAuthAttempt) error {
	var problems []error
	for _, a := range attempts {
		s.stopReceiver(a.ID)
		if err := s.remove(ctx, a.ID); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
