package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// burnliteLinkFixture is one Legato with a fake Burnlite: the board's
// settings and the MCP registry share one connection (LEG-544).
type burnliteLinkFixture struct {
	provider *fakeOAuth
	db       *store.Store
	secrets  flowMemorySecrets
	conns    *MCPConnectionService
	flows    *OAuthFlowService
	links    *BurnliteConnections
	settings *BurnliteSettings
	clock    *testClock
}

// newBurnliteLinkFixture leaves the egress policy as production has it: the
// fake Burnlite is at a loopback literal, as a Burnlite run locally is.
func newBurnliteLinkFixture(t *testing.T, adjust func(egress.Policy) egress.Policy) burnliteLinkFixture {
	t.Helper()
	provider := newFakeOAuth(t)
	provider.confidential = true
	provider.revocation = true
	provider.grantScope = BurnliteScope
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	clock := &testClock{now: time.Now()}
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Secrets: secrets, Egress: adjust, Now: clock.Now})
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: conns, Now: clock.Now})
	conns.SetOAuth(flows)
	t.Cleanup(func() { flows.Close() })
	links := NewBurnliteConnections(db, conns, flows, secrets, BurnliteConnectionsOptions{})
	settings := NewBurnliteSettings(db, NewIntegrationConnector(db, secrets, nil), BurnliteSettingsOptions{
		Connections:  links,
		SecretSource: func(context.Context) (string, error) { return "auto", nil },
	})
	return burnliteLinkFixture{provider: provider, db: db, secrets: secrets, conns: conns, flows: flows, links: links, settings: settings, clock: clock}
}

// connectBoard connects from Settings › Board and tickets, as the web UI does.
func (f burnliteLinkFixture) connectBoard(t *testing.T) BurnliteLink {
	t.Helper()
	ctx := context.Background()
	authorize, err := f.settings.Begin(ctx, f.provider.srv.URL, "https://legato.local:3080/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustQuery(t, authorize).Get("scope"); got != BurnliteScope {
		t.Errorf("asked for scope %q, want %q", got, BurnliteScope)
	}
	link, err := f.settings.Callback(ctx, consent(t, authorize))
	if err != nil {
		t.Fatal(err)
	}
	return link
}

func (f burnliteLinkFixture) revoked() int {
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	return len(f.provider.revoked)
}

func TestOneBoardConnectAlsoAuthorizesAgentsBurnliteConnection(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()

	link := f.connectBoard(t)
	if link.State != store.IntegrationConnected || link.URL != f.provider.srv.URL || link.Connection != "burnlite" || !link.Created || link.Rules != 0 {
		t.Fatalf("link = %+v", link)
	}
	conn, err := f.conns.Get(ctx, "burnlite")
	if err != nil {
		t.Fatal(err)
	}
	if conn.Authorization != "authorized" || conn.URL != f.provider.srv.URL+"/api/llm/mcp" || len(conn.Rules) != 0 || conn.SharedWith != BurnliteIntegration {
		t.Fatalf("MCP connection = %+v", conn)
	}
	if v, _ := f.secrets.Read(ctx, IntegrationRefreshSecret(BurnliteIntegration)); v != "" {
		t.Error("the board kept a token set of its own")
	}

	// One token set: the board and the broker get the same access token,
	// and one refresh renews it for both.
	board, err := f.links.Tokens().Token(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := f.flows.AccessToken(ctx, "burnlite")
	if err != nil || agents != board {
		t.Fatalf("board token %q, agents' %q (%v)", board, agents, err)
	}
	f.clock.Add(2 * time.Hour)
	board, err = f.links.Tokens().Token(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	agents, err = f.flows.AccessToken(ctx, "burnlite")
	if err != nil || agents != board || f.provider.refreshes != 1 {
		t.Fatalf("after expiry: board %q, agents %q, %d refreshes (%v)", board, agents, f.provider.refreshes, err)
	}

	// Connecting again keeps the connection and its rules, and makes none.
	if _, err := f.conns.Update(ctx, "burnlite", MCPConnectionChange{ExpectedRevision: conn.Revision, Rules: &[]MCPRule{{Tools: "get_task", Repository: "*/*", Approve: "auto"}}}); err != nil {
		t.Fatal(err)
	}
	again := f.connectBoard(t)
	if again.Created || again.Connection != "burnlite" || again.Rules != 1 {
		t.Fatalf("reconnect = %+v", again)
	}
	if all, _ := f.conns.List(ctx); len(all) != 1 {
		t.Fatalf("%d MCP connections", len(all))
	}
}

func TestDisconnectingEitherSideDisconnectsBoth(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()

	f.connectBoard(t)
	if _, err := f.settings.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if f.revoked() != 1 {
		t.Errorf("revoked %d tokens at Burnlite", f.revoked())
	}
	if conn, _ := f.conns.Get(ctx, "burnlite"); conn.Authorization != "required" {
		t.Errorf("MCP authorization after a board disconnect = %q", conn.Authorization)
	}
	if link, _ := f.links.Current(ctx); link.State != "none" {
		t.Errorf("board after its disconnect = %+v", link)
	}

	f.connectBoard(t)
	if _, err := f.flows.Disconnect(ctx, "burnlite"); err != nil {
		t.Fatal(err)
	}
	if f.revoked() != 2 {
		t.Errorf("revoked %d tokens at Burnlite", f.revoked())
	}
	if link, _ := f.links.Current(ctx); link.State != store.IntegrationDisconnected || link.Connection != "burnlite" {
		t.Errorf("board after an MCP disconnect = %+v", link)
	}
	if _, err := f.links.Tokens().Token(ctx, false); !errors.Is(err, ErrNotConnected) {
		t.Errorf("board token after an MCP disconnect: %v", err)
	}
}

func TestConnectingFromMCPSettingsConnectsTheBoard(t *testing.T) {
	f := newBurnliteLinkFixture(t, loopbackFixture)
	ctx := context.Background()
	if _, err := f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: f.provider.srv.URL + "/api/llm/mcp"}); err != nil {
		t.Fatal(err)
	}
	if link, _ := f.links.Current(ctx); link.State != "none" {
		t.Fatalf("before = %+v", link)
	}
	begun, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.flows.Deliver(ctx, begun.ID, "operator", begun.ReturnKey, oauthAppResponse(mustQuery(t, begun.AuthorizeURL))); err != nil {
		t.Fatal(err)
	}
	if err := f.flows.Confirm(ctx, begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}

	link, err := f.links.Current(ctx)
	if err != nil || link.State != store.IntegrationConnected || link.URL != f.provider.srv.URL || link.Connection != "burnlite" {
		t.Fatalf("board = %+v, %v", link, err)
	}
	token, err := f.links.Tokens().Token(ctx, false)
	if agents, _ := f.flows.AccessToken(ctx, "burnlite"); err != nil || token != agents {
		t.Fatalf("board token %q, agents' %q (%v)", token, agents, err)
	}
	if conn, _ := f.conns.Get(ctx, "burnlite"); conn.SharedWith != BurnliteIntegration {
		t.Errorf("the MCP connection does not say it is the board's: %+v", conn)
	}
}

// A connection to another server at the same path is not Burnlite's when
// burnlite.url names Burnlite.
func TestOnlyTheConfiguredBurnliteIsLinked(t *testing.T) {
	f := newBurnliteLinkFixture(t, loopbackFixture)
	ctx := context.Background()
	f.links.opts.ConfiguredURL = "https://burnlite.example.com"
	if _, err := f.conns.Add(ctx, MCPConnectionInput{Name: "other", URL: f.provider.srv.URL + "/api/llm/mcp"}); err != nil {
		t.Fatal(err)
	}
	begun, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "other", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.flows.Deliver(ctx, begun.ID, "operator", begun.ReturnKey, oauthAppResponse(mustQuery(t, begun.AuthorizeURL))); err != nil {
		t.Fatal(err)
	}
	if err := f.flows.Confirm(ctx, begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	if link, _ := f.links.Current(ctx); link.State != "none" || strings.Contains(link.URL, "127.0.0.1") {
		t.Fatalf("linked another server: %+v", link)
	}
}

// Turning the MCP connection off is about agents: the board still syncs.
func TestTurningAgentsConnectionOffKeepsTheBoard(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	f.connectBoard(t)
	conn, _ := f.conns.Get(ctx, "burnlite")
	off := false
	if _, err := f.conns.Update(ctx, "burnlite", MCPConnectionChange{ExpectedRevision: conn.Revision, Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(2 * time.Hour)
	if _, err := f.links.Tokens().Token(ctx, false); err != nil {
		t.Fatalf("the board lost Burnlite when agents' connection was turned off: %v", err)
	}
	if link, _ := f.links.Current(ctx); link.State != store.IntegrationConnected {
		t.Fatalf("board = %+v", link)
	}
	if u, _ := f.conns.Usable(ctx, "burnlite", MCPCaller{ExecutionID: "ex-1"}); u.Usable {
		t.Fatal("agents can use a connection that is off")
	}
}

// The loopback permission and the resource come from the address the
// operator typed, never from what the server says: a public Burnlite that
// names a loopback resource is refused, and nothing is made.
func TestAServerCannotNameALoopbackResourceForAPublicAddress(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	_, err := f.links.Adopt(ctx, "https://burnlite.example.com", AdoptedAuthorization{
		Metadata: oauthclient.Metadata{Issuer: "https://burnlite.example.com", TokenEndpoint: "https://burnlite.example.com/api/oauth/token",
			Resource: f.provider.srv.URL + "/api/llm/mcp", RevocationEndpoint: f.provider.srv.URL + "/revoke"},
		ClientID: "c", Token: &oauthclient.Token{RefreshToken: "r"},
	})
	if !errors.Is(err, ErrBurnliteInput) {
		t.Fatalf("adopting a loopback resource for a public address: %v", err)
	}
	if all, _ := f.conns.List(ctx); len(all) != 0 {
		t.Fatalf("made %+v", all)
	}
	if _, err := f.db.GetIntegration(ctx, BurnliteIntegration); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the board kept it: %v", err)
	}
}

// A connection made in MCP servers that no board read has seen yet is
// still the one a board disconnect revokes.
func TestABoardDisconnectFindsAConnectionMadeInMCPServers(t *testing.T) {
	f := newBurnliteLinkFixture(t, loopbackFixture)
	ctx := context.Background()
	f.connectAgents(t, BurnliteScope)
	if _, err := f.links.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if f.revoked() != 1 {
		t.Errorf("revoked %d tokens", f.revoked())
	}
	if _, err := f.flows.AccessToken(ctx, "burnlite"); err == nil {
		t.Fatal("agents kept Burnlite after the board disconnected")
	}
	if link, _ := f.links.Current(ctx); link.State != "none" {
		t.Fatalf("the board came back: %+v", link)
	}
}
