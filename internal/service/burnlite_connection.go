package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/burnlite"
	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/store"
)

// burnliteMCPPath is where a Burnlite serves MCP: its one protected resource,
// which both the board and agents use.
const burnliteMCPPath = "/api/llm/mcp"

// BurnliteLink is Legato's one connection to Burnlite (LEG-544): the board
// integration and the MCP connection agents use share one OAuth client, one
// consent and one token set, kept as the MCP connection's authorization.
type BurnliteLink struct {
	// State is none, connected, or disconnected (the board knows its
	// Burnlite but the shared authorization is gone or ended).
	State       string `json:"state"`
	URL         string `json:"url,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
	// Connection is the MCP connection that holds the tokens; Rules how
	// many rules it has (none: agents get no Burnlite tools yet).
	Connection string `json:"mcp_connection,omitempty"`
	Rules      int    `json:"mcp_rules"`
	// Created: this connect made the MCP connection.
	Created bool `json:"mcp_created,omitempty"`
}

// BurnliteConnectionsOptions are what the configuration says of Burnlite.
type BurnliteConnectionsOptions struct {
	// ConfiguredURL is burnlite.url. When set, an MCP connection is taken
	// for the board's only at that address.
	ConfiguredURL string
}

// BurnliteConnections is the Burnlite board's side of the shared connection:
// which MCP connection is its, its tokens (through OAuthFlowService, so the
// board has the MCP side's refresh lease, revocation and egress policy), and
// connecting and disconnecting both at once.
type BurnliteConnections struct {
	db      *store.Store
	conns   *MCPConnectionService
	flows   *OAuthFlowService
	secrets OAuthFlowSecrets
	opts    BurnliteConnectionsOptions
}

func NewBurnliteConnections(db *store.Store, conns *MCPConnectionService, flows *OAuthFlowService, secrets OAuthFlowSecrets, opts BurnliteConnectionsOptions) *BurnliteConnections {
	return &BurnliteConnections{db: db, conns: conns, flows: flows, secrets: secrets, opts: opts}
}

// burnliteLinks says whether c is the MCP connection of the Burnlite board
// integration in: an HTTP OAuth connection at its resource, with the same
// issuer when the connection pins one.
func burnliteLinks(c store.MCPConnection, in *store.Integration) bool {
	if in == nil || in.Resource == "" || c.Kind != "http" || c.Auth != "oauth" {
		return false
	}
	if strings.TrimRight(c.URL, "/") != strings.TrimRight(in.Resource, "/") {
		return false
	}
	return c.Issuer == "" || in.Issuer == "" || strings.TrimRight(c.Issuer, "/") == strings.TrimRight(in.Issuer, "/")
}

func urlOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// loopbackLiteral says whether raw names a loopback address literally, as a
// Burnlite run locally is typed (http://127.0.0.1:3000).
func loopbackLiteral(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	a, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]"))
	return err == nil && a.Unmap().IsLoopback()
}

// pick prefers the connection named burnlite, then an authorized one.
func pick(candidates []MCPConnectionState) (MCPConnectionState, bool) {
	var best MCPConnectionState
	found := false
	for _, c := range candidates {
		score := func(c MCPConnectionState) int {
			n := 0
			if c.Name == BurnliteIntegration {
				n += 2
			}
			if c.Authorization == "authorized" {
				n++
			}
			return n
		}
		if !found || score(c) > score(best) {
			best, found = c, true
		}
	}
	return best, found
}

// linked is the MCP connection of the board integration in.
func (b *BurnliteConnections) linked(ctx context.Context, in *store.Integration) (MCPConnectionState, bool, error) {
	all, err := b.conns.List(ctx)
	if err != nil {
		return MCPConnectionState{}, false, err
	}
	var candidates []MCPConnectionState
	for _, c := range all {
		if burnliteLinks(c.MCPConnection, in) {
			candidates = append(candidates, c)
		}
	}
	c, ok := pick(candidates)
	return c, ok, nil
}

// discover is an authorized MCP connection to a Burnlite, for a board that
// has none yet: one connected from Settings › MCP connects the board too.
func (b *BurnliteConnections) discover(ctx context.Context) (MCPConnectionState, bool, error) {
	all, err := b.conns.List(ctx)
	if err != nil {
		return MCPConnectionState{}, false, err
	}
	var candidates []MCPConnectionState
	for _, c := range all {
		u, err := url.Parse(c.URL)
		if err != nil || c.Kind != "http" || c.Auth != "oauth" || c.Authorization != "authorized" || strings.TrimRight(u.Path, "/") != burnliteMCPPath {
			continue
		}
		if b.opts.ConfiguredURL != "" && urlOrigin(c.URL) != urlOrigin(b.opts.ConfiguredURL) {
			continue
		}
		candidates = append(candidates, c)
	}
	c, ok := pick(candidates)
	return c, ok, nil
}

// Current is the board's Burnlite and the state of the shared connection.
// An authorized MCP connection to Burnlite connects a board that has none,
// and is remembered as its.
func (b *BurnliteConnections) Current(ctx context.Context) (BurnliteLink, error) {
	in, err := b.db.GetIntegration(ctx, BurnliteIntegration)
	if errors.Is(err, store.ErrNotFound) {
		c, ok, err := b.discover(ctx)
		if err != nil || !ok {
			return BurnliteLink{State: "none"}, err
		}
		md, err := b.flows.AuthorizationMetadata(ctx, c.Name)
		if err != nil {
			return BurnliteLink{State: "none"}, nil
		}
		in = &store.Integration{Name: BurnliteIntegration, URL: urlOrigin(c.URL), Issuer: md.Issuer, TokenEndpoint: md.TokenEndpoint, Resource: c.URL}
		if err := b.db.SaveIntegration(ctx, *in); err != nil {
			return BurnliteLink{}, err
		}
		if in, err = b.db.GetIntegration(ctx, BurnliteIntegration); err != nil {
			return BurnliteLink{}, err
		}
	} else if err != nil {
		return BurnliteLink{}, err
	}
	link := BurnliteLink{State: store.IntegrationDisconnected, URL: in.URL, ConnectedAt: in.ConnectedAt}
	c, ok, err := b.linked(ctx, in)
	if err != nil {
		return BurnliteLink{}, err
	}
	if ok {
		link.Connection, link.Rules = c.Name, len(c.Rules)
		if c.Authorization == "authorized" {
			link.State = store.IntegrationConnected
		}
	}
	return link, nil
}

// connection is the name of the MCP connection holding the board's tokens.
func (b *BurnliteConnections) connection(ctx context.Context) (string, error) {
	link, err := b.Current(ctx)
	if err != nil {
		return "", err
	}
	if link.Connection == "" {
		return "", fmt.Errorf("Legato is not connected to Burnlite: %w", ErrNotConnected)
	}
	return link.Connection, nil
}

// Tokens is the board's access to the shared tokens. They are refreshed by
// OAuthFlowService under its lease, whoever asks.
func (b *BurnliteConnections) Tokens() burnlite.TokenSource { return burnliteTokens{b} }

type burnliteTokens struct{ b *BurnliteConnections }

func (t burnliteTokens) Token(ctx context.Context, force bool) (string, error) {
	name, err := t.b.connection(ctx)
	if err != nil {
		return "", err
	}
	var token string
	if force {
		token, err = t.b.flows.RefreshAccessToken(ctx, name)
	} else {
		token, err = t.b.flows.AccessToken(ctx, name)
	}
	if errors.Is(err, ErrOAuthNotAuthorized) || errors.Is(err, ErrOAuthReauthRequired) {
		return "", fmt.Errorf("Legato is not connected to Burnlite: %w (%w)", ErrNotConnected, err)
	}
	return token, err
}

// Client is the HTTP client for the board's calls to its Burnlite: the
// shared connection's egress policy.
func (b *BurnliteConnections) Client(ctx context.Context) (*http.Client, error) {
	name, err := b.connection(ctx)
	if err != nil {
		return nil, err
	}
	p, err := b.conns.EgressPolicy(ctx, name)
	if err != nil {
		return nil, err
	}
	return p.Client(), nil
}

// ConnectClient is the HTTP client a board connect to address goes through:
// the linked connection's egress policy, else the policy a new one would
// have. An address it would refuse is refused before anything is sent.
func (b *BurnliteConnections) ConnectClient(ctx context.Context, address string) (*http.Client, error) {
	in := &store.Integration{Resource: strings.TrimRight(address, "/") + burnliteMCPPath}
	var p egress.Policy
	if c, ok, err := b.linked(ctx, in); err != nil {
		return nil, err
	} else if ok {
		if p, err = b.conns.EgressPolicy(ctx, c.Name); err != nil {
			return nil, err
		}
	} else {
		p = b.conns.PolicyFor(store.MCPConnection{Loopback: loopbackLiteral(address)})
	}
	if err := p.CheckURL(address); err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrBurnliteInput, address, err)
	}
	return p.Client(), nil
}

// Adopt makes a board connect's tokens the shared connection's: the MCP
// connection at the resource is authorized with them (made, with no rules,
// when there is none), and the board remembers its Burnlite.
func (b *BurnliteConnections) Adopt(ctx context.Context, address string, auth AdoptedAuthorization) (BurnliteLink, error) {
	address = strings.TrimRight(address, "/")
	// The resource, and whether it may be a loopback address, come from the
	// address the operator typed: a server cannot name another one.
	resource := address + burnliteMCPPath
	if named := strings.TrimRight(auth.Metadata.Resource, "/"); named != "" && named != resource {
		return BurnliteLink{}, fmt.Errorf("%w: the Burnlite at %s names its resource %s, not %s", ErrBurnliteInput, address, named, resource)
	}
	in := &store.Integration{Name: BurnliteIntegration, URL: address, Issuer: auth.Metadata.Issuer, TokenEndpoint: auth.Metadata.TokenEndpoint, Resource: resource, ClientID: auth.ClientID}
	c, ok, err := b.linked(ctx, in)
	if err != nil {
		return BurnliteLink{}, err
	}
	created := false
	if !ok {
		name, err := b.freeName(ctx)
		if err != nil {
			return BurnliteLink{}, err
		}
		if c, err = b.conns.Add(ctx, MCPConnectionInput{Name: name, URL: resource, Auth: "oauth", Loopback: loopbackLiteral(address)}); err != nil {
			return BurnliteLink{}, fmt.Errorf("making the MCP connection for Burnlite: %w", err)
		}
		created = true
	}
	if _, err := b.flows.Adopt(ctx, c.Name, auth); err != nil {
		return BurnliteLink{}, err
	}
	if err := b.db.SaveIntegration(ctx, *in); err != nil {
		return BurnliteLink{}, err
	}
	b.forgetLegacy(ctx)
	link, err := b.Current(ctx)
	link.Created = created
	return link, err
}

// freeName is burnlite, or burnlite-2, -3 … when another connection has it.
func (b *BurnliteConnections) freeName(ctx context.Context) (string, error) {
	for i := 1; i < 100; i++ {
		name := BurnliteIntegration
		if i > 1 {
			name += "-" + strconv.Itoa(i)
		}
		if _, err := b.db.GetMCPConnection(ctx, name); errors.Is(err, store.ErrNotFound) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("no free name for Burnlite's MCP connection")
}

// forgetLegacy removes the board's own tokens from before LEG-544.
func (b *BurnliteConnections) forgetLegacy(ctx context.Context) {
	_ = b.secrets.Remove(ctx, IntegrationRefreshSecret(BurnliteIntegration))
	_ = b.secrets.Remove(ctx, IntegrationClientSecret(BurnliteIntegration))
}

// Disconnect disconnects both: the shared authorization is revoked at
// Burnlite where it can be and forgotten, so agents lose Burnlite's tools,
// and the board forgets its Burnlite. The MCP connection and its rules stay.
func (b *BurnliteConnections) Disconnect(ctx context.Context) (OAuthDisconnect, error) {
	result := OAuthDisconnect{Upstream: "none"}
	var c MCPConnectionState
	in, err := b.db.GetIntegration(ctx, BurnliteIntegration)
	ok := false
	switch {
	case err == nil:
		c, ok, err = b.linked(ctx, in)
	case errors.Is(err, store.ErrNotFound):
		// Connected in MCP servers, and no board read has recorded it yet.
		c, ok, err = b.discover(ctx)
	}
	if err != nil {
		return OAuthDisconnect{}, err
	}
	if ok {
		if result, err = b.flows.Disconnect(ctx, c.Name); err != nil {
			return result, err
		}
	}
	b.forgetLegacy(ctx)
	if err := b.db.DeleteIntegration(ctx, BurnliteIntegration); err != nil && !errors.Is(err, store.ErrNotFound) {
		return result, err
	}
	return result, nil
}

// burnliteTime parses an integration's connected_at.
func burnliteTime(raw string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.999Z", time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
