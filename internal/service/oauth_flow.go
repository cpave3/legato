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
	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

var (
	ErrOAuthFlowInput   = errors.New("invalid OAuth connection or receiver")
	ErrOAuthFlowRefused = errors.New("OAuth attempt is unavailable, expired or belongs to another receiver")
	// ErrOAuthSecretStoreLocked: Legato's secret store needs a passphrase
	// typed at each start, so it could not seal an authorization later.
	ErrOAuthSecretStoreLocked = errors.New("OAuth needs a secret store Legato can open without a typed passphrase; change the secret store's master key source")
)

// OAuthFlowSecrets is implemented by the existing host sealed store adapter.
// OAuth values must not be exposed through agent credential read/exec operations.
type OAuthFlowSecrets interface {
	IntegrationSecrets
	Remove(context.Context, string) error
}
type OAuthFlowOptions struct {
	Now          func() time.Time
	Lifetime     time.Duration
	SecretSource func(context.Context) (string, error)
	// Connections is the registry an attempt must name: the resource
	// address, pinned issuer, egress policy and any pre-registered client
	// come from it, never the request, and the attempt is fenced by the
	// connection's revision. Begin refuses to run without it.
	Connections OAuthFlowConnections
	// ClientMetadataDocument is the HTTPS URL of a client ID metadata
	// document Legato may use as its client_id where a server supports
	// CIMD. Legato publishes none yet, so production leaves it empty; it is
	// used only when the document lists the attempt's exact redirect.
	ClientMetadataDocument string
}

// OAuthFlowConnections resolves a connection name to what may be authorized.
type OAuthFlowConnections interface {
	AuthorizationTarget(ctx context.Context, name string) (MCPAuthorizationTarget, error)
	// EgressPolicy is a connection's outbound policy, enabled or not, for
	// revocation when it is disconnected.
	EgressPolicy(ctx context.Context, name string) (egress.Policy, error)
}

// OAuthFlowRequest's Owner is supplied by the authenticated host handler, never
// decoded from an operator's request body. An app can only use a loopback profile
// until a different native callback profile has been explicitly implemented.
// Scope, when set, is the operator's explicit choice; otherwise the server's
// challenge, then the resource's scopes_supported, decide it.
type OAuthFlowRequest struct{ Connection, Scope, Owner, Mode, RedirectURI string }
type OAuthFlowBegin struct {
	ID           string `json:"id"`
	AuthorizeURL string `json:"authorize_url"`
	ReturnKey    string `json:"return_key,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
}
type OAuthFlowStatus struct {
	ID         string `json:"id"`
	Connection string `json:"connection"`
	Status     string `json:"status"`
	// Scope is what the attempt asked for, for the operator to check.
	Scope     string `json:"scope,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
	// Evidence is shown for confirmation: what was authorized, never a secret.
	Evidence *OAuthEvidence `json:"evidence,omitempty"`
}

type oauthFlowSecret struct {
	Metadata                                             oauthclient.Metadata
	ClientID, ClientSecret, State, Verifier, RedirectURI string
	// RegistrationID is the reused or new DCR registration ("" for a
	// pre-registered client or a metadata document).
	RegistrationID string
	Scope          string
	Token          *oauthclient.Token
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
	// holder names this process in refresh and receiver leases.
	holder    string
	leaseStop chan struct{}
	leaseOnce sync.Once
}

func NewOAuthFlowService(db *store.Store, secrets OAuthFlowSecrets, opts OAuthFlowOptions) *OAuthFlowService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Lifetime == 0 {
		opts.Lifetime = 10 * time.Minute
	}
	s := &OAuthFlowService{db: db, secrets: secrets, opts: opts, receivers: map[string]oauthLocalReceiver{}, holder: "proc-" + oauthRandom()[:16], leaseStop: make(chan struct{})}
	go s.renewLeases()
	return s
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
	if s.opts.Connections == nil {
		return OAuthFlowBegin{}, errors.New("OAuth connections need the MCP connection registry")
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
			return OAuthFlowBegin{}, ErrOAuthSecretStoreLocked
		}
	}
	target, err := s.opts.Connections.AuthorizationTarget(ctx, r.Connection)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return OAuthFlowBegin{}, ErrOAuthFlowInput
		}
		return OAuthFlowBegin{}, err
	}
	if target.StaticClientID != "" && r.Mode == "app" && r.RedirectURI != target.StaticRedirectURI {
		return OAuthFlowBegin{}, errors.Join(ErrOAuthFlowInput, errors.New("the pre-registered client is registered for another redirect"))
	}
	// Every hop (resource and authorization server metadata, registration,
	// token) goes through the connection's egress policy.
	client := target.Egress.Client()
	md, scope, err := s.discover(ctx, client, target, r.Scope)
	if err != nil {
		return OAuthFlowBegin{}, err
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
		// The receiver is bound before any registration, so the redirect
		// registered is the address actually listening.
		receiverCtx, cancel := context.WithTimeout(context.Background(), s.opts.Lifetime)
		local = oauthLocalReceiver{connection: r.Connection, owner: r.Owner, cancel: cancel}
		receive := func(ctx context.Context, response oauthclient.AuthorizationResponse) error {
			select {
			case <-ready:
			default:
				return ErrOAuthFlowRefused
			}
			_, err := s.Deliver(ctx, id, r.Owner, key, response)
			return err
		}
		local.receiver, err = s.listen(receiverCtx, target, md, receive)
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
	reg, err := s.register(ctx, client, target, md, r.RedirectURI)
	if err != nil {
		return OAuthFlowBegin{}, err
	}
	auth, err := oauthclient.StartRedirectAuthorization(md, reg.ClientID, scope, r.RedirectURI)
	if err != nil {
		return OAuthFlowBegin{}, err
	}
	defer auth.Close()
	secret := oauthFlowSecret{Metadata: *md, ClientID: reg.ClientID, ClientSecret: reg.ClientSecret, RegistrationID: reg.ID, State: auth.State(), Verifier: auth.Verifier(), RedirectURI: r.RedirectURI, Scope: scope}
	if err := s.write(ctx, id, secret); err != nil {
		return OAuthFlowBegin{}, err
	}
	row := store.OAuthAttempt{ID: id, Connection: r.Connection, Owner: oauthHash(r.Owner), InstanceID: instance, ReceiverHash: oauthHash(key), StateHash: oauthHash(auth.State()), Scope: scope, ExpiresAt: expires.Unix(), ConnectionRevision: target.Revision}
	if local.cancel != nil {
		row.ReceiverOwner, row.ReceiverLeaseUntil = s.holder, s.opts.Now().Add(oauthReceiverLease).Unix()
	}
	_, err = s.db.BeginOAuthAttempt(ctx, row, s.opts.Now().Unix())
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
	result := OAuthFlowBegin{ID: id, AuthorizeURL: auth.URL(), ExpiresAt: expires.Unix()}
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
	return OAuthFlowStatus{ID: a.ID, Connection: a.Connection, Status: a.Status, Scope: a.Scope, ErrorCode: a.ErrorCode, ExpiresAt: a.ExpiresAt}
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
		var forgetErr error
		if isInvalidClient(err) {
			forgetErr = s.forgetRegistration(work, secret.RegistrationID)
		}
		finishErr := s.db.EndOAuthAttempt(work, id, a.Owner, "failed", "token_exchange_failed", s.opts.Now().Unix())
		cleanupErr := s.remove(work, id)
		s.closeReceiverAfterReturn(id)
		if finishErr != nil || cleanupErr != nil || forgetErr != nil {
			return OAuthFlowStatus{}, errors.Join(finishErr, cleanupErr, forgetErr)
		}
		return s.Status(work, id, owner)
	}
	s.rebase(token)
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

// clientFor is the HTTP client for a connection's requests: its egress policy.
func (s *OAuthFlowService) clientFor(ctx context.Context, connection string) (*http.Client, error) {
	if s.opts.Connections == nil {
		return nil, errors.New("OAuth connections need the MCP connection registry")
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
	status := flowStatus(a)
	if a.Status == "awaiting_confirmation" || a.Status == "authorized" {
		status.Evidence = s.evidence(ctx, id)
	}
	return status, nil
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
	s.stopLeases()
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
