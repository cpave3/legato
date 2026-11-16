package service

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

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
}

// MCPConnectionChange edits a connection read at ExpectedRevision. A nil
// field is left as it is.
type MCPConnectionChange struct {
	URL              *string `json:"url,omitempty"`
	Issuer           *string `json:"issuer,omitempty"`
	Auth             *string `json:"auth,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
	ExpectedRevision int64   `json:"expected_revision"`
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
	// Authorization: not_required, required, pending, awaiting_confirmation, authorized.
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
	ForgetAttempts(ctx context.Context, attempts []store.OAuthAttempt) error
}

// MCPAuthorizationTarget is what an OAuth attempt may be begun against.
type MCPAuthorizationTarget struct {
	URL      string
	Issuer   string
	Revision int64
}

type MCPConnectionOptions struct {
	Now func() time.Time
	// CheckURL checks a resource or issuer address; the default requires an
	// absolute HTTPS URL without credentials or fragment. The egress policy
	// (LEG-204) applies again to every request made.
	CheckURL func(string) error
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
	if opts.CheckURL == nil {
		opts.CheckURL = defaultMCPURLCheck
	}
	return &MCPConnectionService{db: db, opts: opts}
}

func (s *MCPConnectionService) SetOAuth(c MCPOAuthCleaner)  { s.oauth = c }
func (s *MCPConnectionService) SetGrants(g MCPGrantChecker) { s.grants = g }

func defaultMCPURLCheck(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, " \t\r\n") {
		return ErrMCPConnectionInvalid
	}
	return nil
}

func (s *MCPConnectionService) checkFields(c store.MCPConnection) error {
	if !mcpConnectionName.MatchString(c.Name) {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("name: lowercase letters, digits and dashes, at most 63"))
	}
	if s.opts.CheckURL(c.URL) != nil {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("url: an absolute https address without credentials"))
	}
	if c.Issuer != "" && s.opts.CheckURL(c.Issuer) != nil {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("issuer: an absolute https address"))
	}
	if c.Auth != "oauth" && c.Auth != "none" {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("auth: oauth or none"))
	}
	if c.Auth == "none" && c.Issuer != "" {
		return errors.Join(ErrMCPConnectionInvalid, errors.New("issuer: an anonymous connection has none"))
	}
	return nil
}

func (s *MCPConnectionService) Add(ctx context.Context, in MCPConnectionInput) (MCPConnectionState, error) {
	instance, err := InstanceID(ctx, s.db)
	if err != nil {
		return MCPConnectionState{}, err
	}
	c := store.MCPConnection{Name: strings.TrimSpace(in.Name), URL: strings.TrimSpace(in.URL), Issuer: strings.TrimSpace(in.Issuer), Auth: in.Auth, InstanceID: instance}
	if c.Auth == "" {
		c.Auth = "oauth"
	}
	if err := s.checkFields(c); err != nil {
		return MCPConnectionState{}, err
	}
	created, err := s.db.CreateMCPConnection(ctx, c, s.opts.Now().Unix())
	if errors.Is(err, store.ErrConflict) {
		return MCPConnectionState{}, ErrMCPConnectionExists
	}
	if err != nil {
		return MCPConnectionState{}, err
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
	if err := s.forget(ctx, fenced); err != nil {
		return MCPConnectionState{}, err
	}
	return s.state(ctx, updated, instance)
}

// Remove deletes a connection. Its authorization and attempts are fenced in
// the same transaction, then their sealed records are removed. Local removal
// does not revoke anything at the provider.
func (s *MCPConnectionService) Remove(ctx context.Context, name string) error {
	fenced, err := s.db.DeleteMCPConnection(ctx, name, s.opts.Now().Unix())
	if err != nil {
		return err
	}
	return s.forget(ctx, fenced)
}

func (s *MCPConnectionService) forget(ctx context.Context, attempts []store.OAuthAttempt) error {
	if len(attempts) == 0 || s.oauth == nil {
		return nil
	}
	return s.oauth.ForgetAttempts(ctx, attempts)
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
	st := MCPConnectionState{MCPConnection: c, Connection: "unknown"}
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
	return MCPAuthorizationTarget{URL: c.URL, Issuer: c.Issuer, Revision: c.Revision}, nil
}
