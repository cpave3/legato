package service

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/store"
)

var (
	ErrMCPConnectionInvalid  = errors.New("invalid MCP connection")
	ErrMCPConnectionExists   = errors.New("an MCP connection of that name already exists")
	ErrMCPConnectionStale    = errors.New("the MCP connection changed since it was read; reload and try again")
	ErrMCPConnectionForeign  = errors.New("this MCP connection belongs to another Legato instance")
	ErrMCPConnectionUnusable = errors.New("this MCP connection cannot be authorized now")
)

var mcpConnectionName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// MCPConnectionInput is what the operator gives to add a connection. Auth is
// "oauth" (the default) or "none" for an anonymous server.
type MCPConnectionInput struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Issuer string `json:"issuer,omitempty"`
	Auth   string `json:"auth,omitempty"`
	// PrivateNetworks are the private prefixes (CIDR) this connection may
	// reach, for an upstream on a LAN or VPN; none means public only.
	PrivateNetworks []string `json:"private_networks,omitempty"`
	// An advanced pre-registered OAuth client, for a server without usable
	// registration: its ID, the bare loopback redirect registered for it
	// (http://127.0.0.1:<port>/callback), and its secret if it has one.
	StaticClientID     string `json:"static_client_id,omitempty"`
	StaticRedirectURI  string `json:"static_redirect_uri,omitempty"`
	StaticClientSecret string `json:"static_client_secret,omitempty"`
	// Rules say which agents may use which tools; none means no agent may.
	// ReadOnlyTools are the tools the operator calls read-only.
	Rules         []MCPRule `json:"rules,omitempty"`
	ReadOnlyTools []string  `json:"read_only_tools,omitempty"`
}

// MCPRule is one credential-broker rule for a connection's tools: which
// tools (a pattern of tool names), for agents working in which repository
// (an owner/name pattern; agents with none are "-/-"), which access (read,
// write, or "" for both, where read means a tool in ReadOnlyTools), and how
// a use is approved (auto, session, window, request, deny) with the window
// and limits of any broker rule.
type MCPRule struct {
	Tools      string            `json:"tools"`
	Repository string            `json:"repository"`
	Access     string            `json:"access,omitempty"`
	Approve    string            `json:"approve"`
	Window     string            `json:"window,omitempty"`
	Limit      map[string]string `json:"limit,omitempty"`
}

// MCPConnectionChange edits a connection read at ExpectedRevision. A nil
// field is left as it is.
type MCPConnectionChange struct {
	URL             *string   `json:"url,omitempty"`
	Issuer          *string   `json:"issuer,omitempty"`
	Auth            *string   `json:"auth,omitempty"`
	PrivateNetworks *[]string `json:"private_networks,omitempty"`
	// StaticClientID "" removes the pre-registered client (and its secret);
	// StaticClientSecret "" removes only the secret.
	StaticClientID     *string    `json:"static_client_id,omitempty"`
	StaticRedirectURI  *string    `json:"static_redirect_uri,omitempty"`
	StaticClientSecret *string    `json:"static_client_secret,omitempty"`
	Rules              *[]MCPRule `json:"rules,omitempty"`
	ReadOnlyTools      *[]string  `json:"read_only_tools,omitempty"`
	Enabled            *bool      `json:"enabled,omitempty"`
	ExpectedRevision   int64      `json:"expected_revision"`
}

// MCPProblem is a fixed code and what the operator can do about it. It never
// carries upstream or provider text.
type MCPProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// MCPConnectionState keeps the layers apart: Enabled is the operator's
// switch, Authorization whether Legato holds a confirmed authorization,
// Connection what happened when Legato last reached the server. Whether a
// given agent may use it is Usable's question, not part of this state.
type MCPConnectionState struct {
	store.MCPConnection
	PrivateNetworks []string  `json:"private_networks,omitempty"`
	Rules           []MCPRule `json:"rules"`
	ReadOnlyTools   []string  `json:"read_only_tools,omitempty"`
	// Authorization: not_required, required, pending, awaiting_confirmation,
	// authorized, reauth_required.
	Authorization string `json:"authorization"`
	// Connection: unknown, connected, failed.
	Connection    string      `json:"connection"`
	LastContactAt *time.Time  `json:"last_contact_at,omitempty"`
	Problem       *MCPProblem `json:"problem,omitempty"`
}

// MCPCaller is who wants to use a connection: an agent execution on this
// instance, as Legato attributes it (never as the caller claims).
type MCPCaller struct {
	ExecutionID string
	TaskID      string
	Slot        string
	Repository  string
}

// MCPUsability says whether a caller may use a connection now and, if not, why.
type MCPUsability struct {
	Usable   bool   `json:"usable"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
	Revision int64  `json:"revision,omitempty"`
}

// MCPGrantChecker decides whether a caller holds a grant for a connection.
// Without one, nothing is granted.
type MCPGrantChecker interface {
	MCPGranted(ctx context.Context, c store.MCPConnection, caller MCPCaller) (bool, error)
}

// MCPOAuthCleaner removes the sealed records and receivers of attempts that
// the registry has already fenced.
type MCPOAuthCleaner interface {
	// ForgetAttempts revokes upstream what they held, through policy, then
	// removes their sealed records.
	ForgetAttempts(ctx context.Context, attempts []store.OAuthAttempt, policy egress.Policy) error
}

// MCPAuthorizationTarget is what an OAuth attempt may be begun against.
type MCPAuthorizationTarget struct {
	URL      string
	Issuer   string
	Revision int64
	// Egress is the policy every request of the attempt goes through.
	Egress egress.Policy
	// A pre-registered client, when the operator configured one.
	StaticClientID, StaticRedirectURI, StaticClientSecret string
}

type MCPConnectionOptions struct {
	Now func() time.Time
	// Egress adjusts a connection's outbound policy before use. Production
	// leaves it nil; tests use it to reach their loopback fixtures.
	Egress func(egress.Policy) egress.Policy
	// Secrets seals a pre-registered client's secret.
	Secrets OAuthFlowSecrets
}

// MCPConnectionService is the instance-owned registry of upstream MCP
// servers. Its mutations are operator operations: the HTTP layer admits only
// the operator's token to them.
type MCPConnectionService struct {
	db     *store.Store
	opts   MCPConnectionOptions
	oauth  MCPOAuthCleaner
	grants MCPGrantChecker
}

func NewMCPConnectionService(db *store.Store, opts MCPConnectionOptions) *MCPConnectionService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &MCPConnectionService{db: db, opts: opts}
}

func (s *MCPConnectionService) SetOAuth(c MCPOAuthCleaner)  { s.oauth = c }
func (s *MCPConnectionService) SetGrants(g MCPGrantChecker) { s.grants = g }

func parseNetworks(raw []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, r := range raw {
		p, err := netip.ParsePrefix(strings.TrimSpace(r))
		if err != nil {
			return nil, fmt.Errorf("%q is not a network prefix", r)
		}
		if err := egress.CheckPrivate(p); err != nil {
			return nil, err
		}
		prefixes = append(prefixes, p.Masked())
	}
	return prefixes, nil
}

func joinNetworks(prefixes []netip.Prefix) string {
	parts := make([]string, len(prefixes))
	for i, p := range prefixes {
		parts[i] = p.String()
	}
	return strings.Join(parts, ",")
}

func splitNetworks(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, ",")
}

// policy is the outbound policy for a connection: public addresses, plus
// the private networks its operator named.
func (s *MCPConnectionService) policy(c store.MCPConnection) (egress.Policy, error) {
	prefixes, err := parseNetworks(splitNetworks(c.Networks))
	if err != nil {
		return egress.Policy{}, err
	}
	p := egress.Policy{Private: prefixes}
	if s.opts.Egress != nil {
		p = s.opts.Egress(p)
	}
	return p, nil
}

func (s *MCPConnectionService) checkFields(c store.MCPConnection) error {
	if !mcpConnectionName.MatchString(c.Name) {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("name: lowercase letters, digits and dashes, at most 63"))
	}
	if c.Auth != "oauth" && c.Auth != "none" {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("auth: oauth or none"))
	}
	if c.Auth == "none" && c.Issuer != "" {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("issuer: an anonymous connection has none"))
	}
	if c.StaticClientID == "" && (c.StaticRedirectURI != "" || c.StaticSecretSet) {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("static_client_id: a pre-registered redirect or secret needs its client ID"))
	}
	if c.StaticClientID != "" {
		if c.Auth != "oauth" || len(c.StaticClientID) > 512 || strings.ContainsFunc(c.StaticClientID, func(r rune) bool { return r < ' ' }) {
			return errors.Join(ErrMCPConnectionInvalid, errors.New("static_client_id: a client ID for an OAuth connection"))
		}
		if !validOAuthAppRedirect(c.StaticRedirectURI) {
			return errors.Join(ErrMCPConnectionInvalid, errors.New("static_redirect_uri: the bare redirect registered for the client, http://127.0.0.1:<port>/callback"))
		}
	}
	p, err := s.policy(c)
	if err != nil {
		return errors.Join(ErrMCPConnectionInvalid, fmt.Errorf("private_networks: %w", err))
	}
	if err := p.CheckURL(c.URL); err != nil {
		return errors.Join(ErrMCPConnectionInvalid, fmt.Errorf("url: %w", err))
	}
	if c.Issuer != "" {
		if err := p.CheckURL(c.Issuer); err != nil {
			return errors.Join(ErrMCPConnectionInvalid, fmt.Errorf("issuer: %w", err))
		}
	}
	return nil
}

func normalizeNetworks(raw []string) (string, error) {
	prefixes, err := parseNetworks(raw)
	if err != nil {
		return "", errors.Join(ErrMCPConnectionInvalid, fmt.Errorf("private_networks: %w", err))
	}
	return joinNetworks(prefixes), nil
}

func (s *MCPConnectionService) Add(ctx context.Context, in MCPConnectionInput) (MCPConnectionState, error) {
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return MCPConnectionState{}, err
	}
	networks, err := normalizeNetworks(in.PrivateNetworks)
	if err != nil {
		return MCPConnectionState{}, err
	}
	c := store.MCPConnection{Name: strings.TrimSpace(in.Name), URL: strings.TrimSpace(in.URL), Issuer: strings.TrimSpace(in.Issuer), Networks: networks, Auth: in.Auth, InstanceID: instance,
		StaticClientID: strings.TrimSpace(in.StaticClientID), StaticRedirectURI: strings.TrimSpace(in.StaticRedirectURI), StaticSecretSet: in.StaticClientSecret != ""}
	if c.Auth == "" {
		c.Auth = "oauth"
	}
	if c.RulesJSON, c.ReadOnlyTools, err = encodeMCPRules(in.Rules, in.ReadOnlyTools); err != nil {
		return MCPConnectionState{}, err
	}
	if err := s.checkFields(c); err != nil {
		return MCPConnectionState{}, err
	}
	if c.StaticSecretSet && s.opts.Secrets == nil {
		return MCPConnectionState{}, errors.New("this Legato cannot seal a client secret")
	}
	created, err := s.db.CreateMCPConnection(ctx, c, s.opts.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		return MCPConnectionState{}, ErrMCPConnectionExists
	}
	if err != nil {
		return MCPConnectionState{}, err
	}
	if c.StaticSecretSet {
		if err := s.opts.Secrets.Write(ctx, mcpStaticSecretName(c.Name), in.StaticClientSecret); err != nil {
			_, _ = s.db.DeleteMCPConnection(ctx, c.Name, s.opts.Now().Unix())
			return MCPConnectionState{}, fmt.Errorf("sealing the client secret: %w", err)
		}
	}
	return s.state(ctx, created, instance)
}

func (s *MCPConnectionService) List(ctx context.Context) ([]MCPConnectionState, error) {
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return nil, err
	}
	all, err := s.db.ListMCPConnections(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]MCPConnectionState, 0, len(all))
	for _, c := range all {
		st, err := s.state(ctx, c, instance)
		if err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	return states, nil
}

func (s *MCPConnectionService) Get(ctx context.Context, name string) (MCPConnectionState, error) {
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return MCPConnectionState{}, err
	}
	c, err := s.db.GetMCPConnection(ctx, name)
	if err != nil {
		return MCPConnectionState{}, err
	}
	return s.state(ctx, c, instance)
}

// Update applies an operator's change. Changing the resource, issuer or auth
// mode discards the authorization (it was for another resource); disabling
// ends unfinished authorization attempts and keeps the authorization.
func (s *MCPConnectionService) Update(ctx context.Context, name string, ch MCPConnectionChange) (MCPConnectionState, error) {
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return MCPConnectionState{}, err
	}
	c, err := s.db.GetMCPConnection(ctx, name)
	if err != nil {
		return MCPConnectionState{}, err
	}
	if c.InstanceID != instance {
		return MCPConnectionState{}, ErrMCPConnectionForeign
	}
	if ch.ExpectedRevision != c.Revision {
		return MCPConnectionState{}, ErrMCPConnectionStale
	}
	// Revocation of what the old resource held goes through its own policy.
	oldPolicy, err := s.policy(c)
	if err != nil {
		return MCPConnectionState{}, err
	}
	before := c
	if ch.URL != nil {
		c.URL = strings.TrimSpace(*ch.URL)
	}
	if ch.Issuer != nil {
		c.Issuer = strings.TrimSpace(*ch.Issuer)
	}
	if ch.Auth != nil {
		c.Auth = *ch.Auth
	}
	if ch.Enabled != nil {
		c.Enabled = *ch.Enabled
	}
	if ch.PrivateNetworks != nil {
		if c.Networks, err = normalizeNetworks(*ch.PrivateNetworks); err != nil {
			return MCPConnectionState{}, err
		}
	}
	if ch.StaticClientID != nil {
		c.StaticClientID = strings.TrimSpace(*ch.StaticClientID)
		if c.StaticClientID == "" {
			c.StaticRedirectURI, c.StaticSecretSet = "", false
		}
	}
	if ch.StaticRedirectURI != nil {
		c.StaticRedirectURI = strings.TrimSpace(*ch.StaticRedirectURI)
	}
	if ch.Rules != nil || ch.ReadOnlyTools != nil {
		rules, readOnly := decodeMCPRules(c)
		if ch.Rules != nil {
			rules = *ch.Rules
		}
		if ch.ReadOnlyTools != nil {
			readOnly = *ch.ReadOnlyTools
		}
		if c.RulesJSON, c.ReadOnlyTools, err = encodeMCPRules(rules, readOnly); err != nil {
			return MCPConnectionState{}, err
		}
	}
	if ch.StaticClientSecret != nil {
		c.StaticSecretSet = *ch.StaticClientSecret != ""
		if c.StaticSecretSet && s.opts.Secrets == nil {
			return MCPConnectionState{}, errors.New("this Legato cannot seal a client secret")
		}
	}
	if err := s.checkFields(c); err != nil {
		return MCPConnectionState{}, err
	}
	updated, fenced, err := s.db.UpdateMCPConnection(ctx, c, s.opts.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		return MCPConnectionState{}, ErrMCPConnectionStale
	}
	if err != nil {
		return MCPConnectionState{}, err
	}
	if err := s.forget(ctx, fenced, oldPolicy); err != nil {
		return MCPConnectionState{}, err
	}
	if fencedResource(before, updated) {
		if err := s.endGrants(ctx, name); err != nil {
			return MCPConnectionState{}, err
		}
	}
	switch {
	case ch.StaticClientSecret != nil && *ch.StaticClientSecret != "":
		if err := s.opts.Secrets.Write(ctx, mcpStaticSecretName(name), *ch.StaticClientSecret); err != nil {
			return MCPConnectionState{}, fmt.Errorf("sealing the client secret: %w", err)
		}
	case !updated.StaticSecretSet:
		if err := s.removeStaticSecret(ctx, name); err != nil {
			return MCPConnectionState{}, err
		}
	}
	return s.state(ctx, updated, instance)
}

// Remove deletes a connection. Its authorization and attempts are fenced in
// the same transaction, then their sealed records are removed. Local removal
// does not revoke anything at the provider.
func (s *MCPConnectionService) Remove(ctx context.Context, name string) error {
	c, err := s.db.GetMCPConnection(ctx, name)
	if err != nil {
		return err
	}
	policy, err := s.policy(c)
	if err != nil {
		return err
	}
	fenced, err := s.db.DeleteMCPConnection(ctx, name, s.opts.Now().Unix())
	if err != nil {
		return err
	}
	return errors.Join(s.forget(ctx, fenced, policy), s.removeStaticSecret(ctx, name), s.endGrants(ctx, name))
}

func mcpStaticSecretName(name string) string { return "mcp-static-client-" + name }

func (s *MCPConnectionService) removeStaticSecret(ctx context.Context, name string) error {
	if s.opts.Secrets == nil {
		return nil
	}
	err := s.opts.Secrets.Remove(ctx, mcpStaticSecretName(name))
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	return err
}

func (s *MCPConnectionService) forget(ctx context.Context, attempts []store.OAuthAttempt, policy egress.Policy) error {
	if len(attempts) == 0 || s.oauth == nil {
		return nil
	}
	return s.oauth.ForgetAttempts(ctx, attempts, policy)
}

// EgressPolicy is a connection's outbound policy, whether or not it is enabled.
func (s *MCPConnectionService) EgressPolicy(ctx context.Context, name string) (egress.Policy, error) {
	c, err := s.db.GetMCPConnection(ctx, name)
	if err != nil {
		return egress.Policy{}, err
	}
	return s.policy(c)
}

// mcpContactCodes are the outcomes the broker may record; anything else is
// shown as upstream_error, so no upstream text reaches the operator's screen.
var mcpContactCodes = map[string]string{
	"unreachable":    "Legato could not reach the server. Check the address and that the server is up.",
	"tls":            "The server's certificate was not trusted.",
	"unauthorized":   "The server refused Legato's authorization. Connect it again.",
	"forbidden":      "The server refused the request for this account.",
	"egress_denied":  "Legato's network policy does not allow this address.",
	"protocol":       "The server did not answer as an MCP server.",
	"too_large":      "The server's answer was larger than Legato accepts.",
	"upstream_error": "The server reported an error. Try again later.",
}

// RecordContact records the result of reaching the server at revision: "" for
// success, else one of the fixed codes. A result for an older revision is ignored.
func (s *MCPConnectionService) RecordContact(ctx context.Context, name string, revision int64, code string) error {
	if code != "" {
		if _, ok := mcpContactCodes[code]; !ok {
			code = "upstream_error"
		}
	}
	err := s.db.RecordMCPContact(ctx, name, revision, code, s.opts.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		return nil
	}
	return err
}

var mcpProblemMessages = map[string]string{
	"other_instance":         "This connection was made by another Legato instance and cannot be used here. Remove it and add it again.",
	"disabled":               "This connection is turned off.",
	"authorization_required": "Connect this server from the Legato app, or from a browser on the computer Legato runs on.",
	"authorization_pending":  "Sign-in has started and has not finished.",
	"reauth_required":        "Legato's authorization for this server ended. Connect it again from the Legato app, or from a browser on the computer Legato runs on.",
	"awaiting_confirmation":  "Confirm the account Legato signed in with.",
	"not_found":              "There is no MCP connection of that name.",
	"not_granted":            "No grant lets this agent use the connection.",
}

func mcpProblem(code string) *MCPProblem {
	if msg, ok := mcpProblemMessages[code]; ok {
		return &MCPProblem{Code: code, Message: msg}
	}
	return &MCPProblem{Code: code, Message: mcpContactCodes[code]}
}

func (s *MCPConnectionService) authorization(ctx context.Context, c store.MCPConnection) (string, error) {
	if c.Auth == "none" {
		return "not_required", nil
	}
	if _, err := s.db.ActiveOAuthAttempt(ctx, c.Name); err == nil {
		return "authorized", nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	attempts, err := s.db.ListOAuthAttempts(ctx, c.Name)
	if err != nil {
		return "", err
	}
	now := s.opts.Now().Unix()
	state := "required"
	if cur, err := s.db.CurrentOAuthAttempt(ctx, c.Name); err == nil && cur.Status == "reauth_required" {
		state = "reauth_required"
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	for _, a := range attempts {
		if a.ExpiresAt <= now {
			continue
		}
		switch a.Status {
		case "awaiting_confirmation":
			return "awaiting_confirmation", nil
		case "pending", "exchanging":
			state = "pending"
		}
	}
	return state, nil
}

func (s *MCPConnectionService) state(ctx context.Context, c store.MCPConnection, instance string) (MCPConnectionState, error) {
	st := MCPConnectionState{MCPConnection: c, PrivateNetworks: splitNetworks(c.Networks), Connection: "unknown"}
	st.Rules, st.ReadOnlyTools = decodeMCPRules(c)
	var err error
	if st.Authorization, err = s.authorization(ctx, c); err != nil {
		return MCPConnectionState{}, err
	}
	switch {
	case c.LastError != "":
		st.Connection = "failed"
	case c.LastContactAt != 0:
		st.Connection = "connected"
		at := time.Unix(c.LastContactAt, 0).UTC()
		st.LastContactAt = &at
	}
	switch {
	case c.InstanceID != instance:
		st.Problem = mcpProblem("other_instance")
	case !c.Enabled:
		st.Problem = mcpProblem("disabled")
	case st.Authorization == "required":
		st.Problem = mcpProblem("authorization_required")
	case st.Authorization == "reauth_required":
		st.Problem = mcpProblem("reauth_required")
	case st.Authorization == "pending":
		st.Problem = mcpProblem("authorization_pending")
	case st.Authorization == "awaiting_confirmation":
		st.Problem = mcpProblem("awaiting_confirmation")
	case c.LastError != "":
		st.Problem = mcpProblem(c.LastError)
	}
	return st, nil
}

// Usable decides whether caller may use the connection now. It is checked on
// every discovery and dispatch; Revision lets the caller fence its call.
func (s *MCPConnectionService) Usable(ctx context.Context, name string, caller MCPCaller) (MCPUsability, error) {
	deny := func(code string) (MCPUsability, error) {
		return MCPUsability{Code: code, Message: mcpProblem(code).Message}, nil
	}
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return MCPUsability{}, err
	}
	c, err := s.db.GetMCPConnection(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return deny("not_found")
	}
	if err != nil {
		return MCPUsability{}, err
	}
	st, err := s.state(ctx, c, instance)
	if err != nil {
		return MCPUsability{}, err
	}
	if st.Problem != nil && st.Problem.Code != c.LastError {
		return deny(st.Problem.Code)
	}
	if s.grants == nil {
		return deny("not_granted")
	}
	ok, err := s.grants.MCPGranted(ctx, c, caller)
	if err != nil {
		return MCPUsability{}, err
	}
	if !ok {
		return deny("not_granted")
	}
	return MCPUsability{Usable: true, Revision: c.Revision}, nil
}

// AuthorizationTarget is what OAuthFlowService may begin an attempt against:
// an enabled OAuth connection of this instance, at its current revision.
func (s *MCPConnectionService) AuthorizationTarget(ctx context.Context, name string) (MCPAuthorizationTarget, error) {
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return MCPAuthorizationTarget{}, err
	}
	c, err := s.db.GetMCPConnection(ctx, name)
	if err != nil {
		return MCPAuthorizationTarget{}, err
	}
	if c.InstanceID != instance {
		return MCPAuthorizationTarget{}, ErrMCPConnectionForeign
	}
	if !c.Enabled || c.Auth != "oauth" {
		return MCPAuthorizationTarget{}, ErrMCPConnectionUnusable
	}
	p, err := s.policy(c)
	if err != nil {
		return MCPAuthorizationTarget{}, err
	}
	target := MCPAuthorizationTarget{URL: c.URL, Issuer: c.Issuer, Revision: c.Revision, Egress: p, StaticClientID: c.StaticClientID, StaticRedirectURI: c.StaticRedirectURI}
	if c.StaticSecretSet {
		if s.opts.Secrets == nil {
			return MCPAuthorizationTarget{}, errors.New("this Legato cannot read the sealed client secret")
		}
		if target.StaticClientSecret, err = s.opts.Secrets.Read(ctx, mcpStaticSecretName(c.Name)); err != nil {
			return MCPAuthorizationTarget{}, fmt.Errorf("reading the sealed client secret: %w", err)
		}
	}
	return target, nil
}
