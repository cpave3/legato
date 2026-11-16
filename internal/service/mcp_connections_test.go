package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/store"
)

// loopbackFixture lets the registry reach the plain-HTTP fake provider on
// loopback; production policies never set it.
func loopbackFixture(p egress.Policy) egress.Policy {
	p.LoopbackFixture = true
	return p
}

type mcpFixture struct {
	db      *store.Store
	secrets flowMemorySecrets
	flows   *OAuthFlowService
	conns   *MCPConnectionService
}

// flowConnections is a registry with the connections the flow tests name,
// each at the fake provider's MCP endpoint.
func flowConnections(t *testing.T, db *store.Store, f *fakeOAuth) *MCPConnectionService {
	t.Helper()
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Egress: loopbackFixture})
	for _, name := range []string{"mcp-burnlite", "one", "connection", "sealed", "local"} {
		if _, err := conns.Add(context.Background(), MCPConnectionInput{Name: name, URL: f.srv.URL + "/api/llm/mcp"}); err != nil && !errors.Is(err, ErrMCPConnectionExists) {
			t.Fatal(err)
		}
	}
	return conns
}

func newMCPFixture(t *testing.T) mcpFixture {
	t.Helper()
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Egress: loopbackFixture})
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: conns})
	conns.SetOAuth(flows)
	t.Cleanup(func() { flows.Close() })
	return mcpFixture{db: db, secrets: secrets, flows: flows, conns: conns}
}

// authorize runs the local web flow to an authorized connection.
func (f mcpFixture) authorize(t *testing.T, name string) {
	t.Helper()
	begun, err := f.flows.Begin(context.Background(), OAuthFlowRequest{Connection: name, Owner: "operator", Mode: "web"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: time.Second}).Get(begun.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := f.flows.Confirm(context.Background(), begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}
}

func TestMCPConnectionsValidateWhatTheOperatorAdds(t *testing.T) {
	f := newMCPFixture(t)
	ctx := context.Background()
	for _, bad := range []MCPConnectionInput{
		{Name: "", URL: "https://mcp.example.test/mcp"},
		{Name: "Has Spaces", URL: "https://mcp.example.test/mcp"},
		{Name: strings.Repeat("a", 64), URL: "https://mcp.example.test/mcp"},
		{Name: "notion", URL: "http://mcp.example.test/mcp"},
		{Name: "notion", URL: "https://user:pass@mcp.example.test/mcp"},
		{Name: "notion", URL: "https://mcp.example.test/mcp#frag"},
		{Name: "notion", URL: "mcp.example.test/mcp"},
		{Name: "notion", URL: "https://mcp.example.test/mcp", Auth: "basic"},
		{Name: "notion", URL: "https://mcp.example.test/mcp", Issuer: "http://issuer.example.test"},
	} {
		if _, err := f.conns.Add(ctx, bad); !errors.Is(err, ErrMCPConnectionInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	added, err := f.conns.Add(ctx, MCPConnectionInput{Name: "notion", URL: "https://mcp.example.test/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if added.Auth != "oauth" || !added.Enabled || added.Authorization != "required" || added.Connection != "unknown" || added.Problem == nil || added.Problem.Code != "authorization_required" {
		t.Fatalf("added %+v", added)
	}
	if _, err := f.conns.Add(ctx, MCPConnectionInput{Name: "notion", URL: "https://other.example.test/mcp"}); !errors.Is(err, ErrMCPConnectionExists) {
		t.Fatalf("duplicate: %v", err)
	}
	anon, err := f.conns.Add(ctx, MCPConnectionInput{Name: "docs", URL: "https://docs.example.test/mcp", Auth: "none"})
	if err != nil || anon.Authorization != "not_required" || anon.Problem != nil {
		t.Fatalf("anonymous %+v %v", anon, err)
	}
}

func TestMCPConnectionStatesAreSeparate(t *testing.T) {
	provider := newFakeOAuth(t)
	f := newMCPFixture(t)
	ctx := context.Background()
	if _, err := f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: provider.srv.URL + "/api/llm/mcp"}); err != nil {
		t.Fatal(err)
	}
	f.authorize(t, "burnlite")
	got, err := f.conns.Get(ctx, "burnlite")
	if err != nil || got.Authorization != "authorized" || got.Connection != "unknown" || got.Problem != nil {
		t.Fatalf("authorized %+v %v", got, err)
	}
	if err := f.conns.RecordContact(ctx, "burnlite", got.Revision, "unreachable"); err != nil {
		t.Fatal(err)
	}
	got, _ = f.conns.Get(ctx, "burnlite")
	if got.Connection != "failed" || got.Problem == nil || got.Problem.Code != "unreachable" || got.Problem.Message == "" {
		t.Fatalf("failed contact %+v", got)
	}
	if err := f.conns.RecordContact(ctx, "burnlite", got.Revision, "raw provider text <script>"); err != nil {
		t.Fatal(err)
	}
	got, _ = f.conns.Get(ctx, "burnlite")
	if got.Problem.Code != "upstream_error" {
		t.Fatalf("unknown code passed through: %+v", got.Problem)
	}
	if err := f.conns.RecordContact(ctx, "burnlite", got.Revision, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = f.conns.Get(ctx, "burnlite")
	if got.Connection != "connected" || got.Problem != nil {
		t.Fatalf("connected %+v", got)
	}
	// Authorized and connected is still not usable by an agent without a grant.
	use, err := f.conns.Usable(ctx, "burnlite", MCPCaller{ExecutionID: "ex-1", TaskID: "t1"})
	if err != nil || use.Usable || use.Code != "not_granted" {
		t.Fatalf("usable without grant %+v %v", use, err)
	}
	encoded, _ := json.Marshal(got)
	for _, secret := range []string{"access-", "refresh-", "legato-client", "code-1"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("state exposed %q", secret)
		}
	}
}

type grantAll struct{ callers []MCPCaller }

func (g *grantAll) MCPGranted(_ context.Context, _ store.MCPConnection, c MCPCaller) (bool, error) {
	g.callers = append(g.callers, c)
	return true, nil
}

func TestMCPConnectionUsabilityChecksEachLayer(t *testing.T) {
	provider := newFakeOAuth(t)
	f := newMCPFixture(t)
	grants := &grantAll{}
	f.conns.SetGrants(grants)
	ctx := context.Background()
	caller := MCPCaller{ExecutionID: "ex-1", TaskID: "t1"}
	if use, _ := f.conns.Usable(ctx, "missing", caller); use.Usable || use.Code != "not_found" {
		t.Fatalf("missing %+v", use)
	}
	added, _ := f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: provider.srv.URL + "/api/llm/mcp"})
	if use, _ := f.conns.Usable(ctx, "burnlite", caller); use.Usable || use.Code != "authorization_required" {
		t.Fatalf("unauthorized %+v", use)
	}
	f.authorize(t, "burnlite")
	if use, _ := f.conns.Usable(ctx, "burnlite", caller); !use.Usable || use.Revision == 0 {
		t.Fatalf("granted %+v", use)
	}
	off := false
	got, _ := f.conns.Get(ctx, "burnlite")
	if _, err := f.conns.Update(ctx, "burnlite", MCPConnectionChange{Enabled: &off, ExpectedRevision: got.Revision}); err != nil {
		t.Fatal(err)
	}
	if use, _ := f.conns.Usable(ctx, "burnlite", caller); use.Usable || use.Code != "disabled" {
		t.Fatalf("disabled %+v", use)
	}
	if _, err := f.conns.Update(ctx, "burnlite", MCPConnectionChange{Enabled: &off, ExpectedRevision: added.Revision}); !errors.Is(err, ErrMCPConnectionStale) {
		t.Fatalf("stale update: %v", err)
	}
	if len(grants.callers) != 1 || grants.callers[0] != caller {
		t.Fatalf("grant checker saw %+v", grants.callers)
	}
}

func TestChangingAnMCPResourceRemovesItsSealedCredentials(t *testing.T) {
	provider := newFakeOAuth(t)
	f := newMCPFixture(t)
	ctx := context.Background()
	f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: provider.srv.URL + "/api/llm/mcp"})
	f.authorize(t, "burnlite")
	f.secrets.mu.Lock()
	stored := len(f.secrets.values)
	f.secrets.mu.Unlock()
	if stored == 0 {
		t.Fatal("authorization stored nothing")
	}
	got, _ := f.conns.Get(ctx, "burnlite")
	moved := provider.srv.URL + "/elsewhere/mcp"
	changed, err := f.conns.Update(ctx, "burnlite", MCPConnectionChange{URL: &moved, ExpectedRevision: got.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Authorization != "required" || changed.URL != moved {
		t.Fatalf("changed %+v", changed)
	}
	f.secrets.mu.Lock()
	left := len(f.secrets.values)
	f.secrets.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d sealed OAuth records survived a resource change", left)
	}
}

func TestRemovingAnMCPConnectionEndsItsAuthorizationAttempts(t *testing.T) {
	provider := newFakeOAuth(t)
	f := newMCPFixture(t)
	ctx := context.Background()
	f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: provider.srv.URL + "/api/llm/mcp"})
	begun, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.conns.Remove(ctx, "burnlite"); err != nil {
		t.Fatal(err)
	}
	if err := f.conns.Remove(ctx, "burnlite"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
	// The browser returning afterwards cannot settle the attempt: its receiver is gone.
	if resp, err := (&http.Client{Timeout: time.Second}).Get(begun.AuthorizeURL); err == nil {
		resp.Body.Close()
		state, _ := f.flows.Status(ctx, begun.ID, "operator")
		if state.Status != "cancelled" {
			t.Fatalf("attempt after removal: %+v", state)
		}
	}
	f.secrets.mu.Lock()
	left := len(f.secrets.values)
	f.secrets.mu.Unlock()
	if left != 0 {
		t.Fatal("sealed attempt survived removal")
	}
}

func TestOAuthBeginUsesTheRegisteredConnectionOnly(t *testing.T) {
	provider := newFakeOAuth(t)
	f := newMCPFixture(t)
	ctx := context.Background()
	if _, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "unknown", Owner: "operator", Mode: "web"}); err == nil {
		t.Fatal("began OAuth for an unregistered connection")
	}
	f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: provider.srv.URL + "/api/llm/mcp"})
	f.conns.Add(ctx, MCPConnectionInput{Name: "anon", URL: provider.srv.URL + "/api/llm/mcp", Auth: "none"})
	if _, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "anon", Owner: "operator", Mode: "web"}); err == nil {
		t.Fatal("began OAuth for an anonymous connection")
	}
	pinned := "https://issuer.example.test"
	got, _ := f.conns.Get(ctx, "burnlite")
	if _, err := f.conns.Update(ctx, "burnlite", MCPConnectionChange{Issuer: &pinned, ExpectedRevision: got.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"}); err == nil {
		t.Fatal("began OAuth with an issuer other than the pinned one")
	}
}

func TestAnMCPConnectionOfAnotherInstanceIsNotUsable(t *testing.T) {
	f := newMCPFixture(t)
	ctx := context.Background()
	f.conns.Add(ctx, MCPConnectionInput{Name: "notion", URL: "https://mcp.example.test/mcp", Auth: "none"})
	// Simulate a database whose instance identity changed (copied or restored).
	if _, err := f.db.DB().Exec(`UPDATE mcp_connections SET instance_id='srv-other'`); err != nil {
		t.Fatal(err)
	}
	got, err := f.conns.Get(ctx, "notion")
	if err != nil || got.Problem == nil || got.Problem.Code != "other_instance" {
		t.Fatalf("foreign %+v %v", got, err)
	}
	f.conns.SetGrants(&grantAll{})
	if use, _ := f.conns.Usable(ctx, "notion", MCPCaller{ExecutionID: "ex"}); use.Usable || use.Code != "other_instance" {
		t.Fatalf("foreign usable %+v", use)
	}
}

func TestTheProductionEgressPolicyChecksWhatTheOperatorAdds(t *testing.T) {
	db := newIntegrationStore(t)
	conns := NewMCPConnectionService(db, MCPConnectionOptions{})
	ctx := context.Background()
	for _, bad := range []MCPConnectionInput{
		{Name: "a", URL: "https://127.0.0.1/mcp"},
		{Name: "b", URL: "http://127.0.0.1:8080/mcp"},
		{Name: "c", URL: "https://169.254.169.254/latest"},
		{Name: "d", URL: "https://localhost/mcp"},
		{Name: "e", URL: "https://mcp.example.test/mcp?access_token=x"},
		{Name: "f", URL: "https://10.1.2.3/mcp"},
		{Name: "g", URL: "https://mcp.example.test/mcp", PrivateNetworks: []string{"8.8.8.0/24"}},
		{Name: "h", URL: "https://mcp.example.test/mcp", PrivateNetworks: []string{"0.0.0.0/0"}},
		{Name: "i", URL: "https://mcp.example.test/mcp", PrivateNetworks: []string{"not-a-prefix"}},
	} {
		if _, err := conns.Add(ctx, bad); !errors.Is(err, ErrMCPConnectionInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	// A private upstream needs the operator to name its network.
	added, err := conns.Add(ctx, MCPConnectionInput{Name: "vpn", URL: "https://10.1.2.3/mcp", PrivateNetworks: []string{"10.1.0.0/16"}})
	if err != nil || len(added.PrivateNetworks) != 1 || added.PrivateNetworks[0] != "10.1.0.0/16" {
		t.Fatalf("private upstream %+v %v", added, err)
	}
	none := []string{}
	if _, err := conns.Update(ctx, "vpn", MCPConnectionChange{PrivateNetworks: &none, ExpectedRevision: added.Revision}); !errors.Is(err, ErrMCPConnectionInvalid) {
		t.Fatalf("dropping the network of a private upstream: %v", err)
	}
}

// Every hop of authorization goes through the connection's policy: an
// authorization server the resource names on an internal address is refused
// without a connection being made.
func TestEveryOAuthHopIsConstrained(t *testing.T) {
	provider := newFakeOAuth(t)
	provider.authServer = "https://10.9.9.9"
	f := newMCPFixture(t)
	ctx := context.Background()
	if _, err := f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: provider.srv.URL + "/api/llm/mcp"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"})
	if err == nil {
		t.Fatal("began authorization through an internal authorization server")
	}
	f.secrets.mu.Lock()
	left := len(f.secrets.values)
	f.secrets.mu.Unlock()
	if left != 0 {
		t.Fatal("a refused attempt left sealed records")
	}
}
