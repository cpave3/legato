package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

type registrationFixture struct {
	mcpFixture
	provider *fakeOAuth
}

func newRegistrationFixture(t *testing.T, opts OAuthFlowOptions, conn MCPConnectionInput) registrationFixture {
	t.Helper()
	provider := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Egress: loopbackFixture, Secrets: secrets})
	opts.Connections = conns
	flows := NewOAuthFlowService(db, secrets, opts)
	conns.SetOAuth(flows)
	t.Cleanup(func() { flows.Close() })
	if conn.Name == "" {
		conn.Name = "burnlite"
	}
	conn.URL = provider.srv.URL + "/api/llm/mcp"
	if _, err := conns.Add(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	return registrationFixture{mcpFixture{db: db, secrets: secrets, flows: flows, conns: conns}, provider}
}

func (f registrationFixture) begin(t *testing.T, req OAuthFlowRequest) (OAuthFlowBegin, url.Values) {
	t.Helper()
	if req.Connection == "" {
		req.Connection = "burnlite"
	}
	req.Owner = "operator"
	if req.Mode == "" {
		req.Mode = "web"
	}
	begun, err := f.flows.Begin(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(begun.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	return begun, u.Query()
}

func (f registrationFixture) registrations() int {
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	return f.provider.registrations
}

// A web attempt reuses its earlier registration by binding the same loopback
// port again, so re-authorizing does not register a new client every time.
func TestAWebAttemptReusesItsRegistrationOnTheSamePort(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	first, q1 := f.begin(t, OAuthFlowRequest{})
	if err := f.flows.Cancel(context.Background(), first.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	waitForPortFree(t, q1.Get("redirect_uri"))
	_, q2 := f.begin(t, OAuthFlowRequest{})
	if f.registrations() != 1 || q2.Get("redirect_uri") != q1.Get("redirect_uri") || q2.Get("client_id") != q1.Get("client_id") {
		t.Fatalf("registrations %d, redirects %s %s", f.registrations(), q1.Get("redirect_uri"), q2.Get("redirect_uri"))
	}
}

// When the earlier port is taken, Legato does not pretend: it binds a new
// port and registers that exact redirect.
func TestAnOccupiedPortGetsANewRegistration(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	first, q1 := f.begin(t, OAuthFlowRequest{})
	_ = first // its receiver still holds the port
	_, q2 := f.begin(t, OAuthFlowRequest{})
	if f.registrations() != 2 || q2.Get("redirect_uri") == q1.Get("redirect_uri") {
		t.Fatalf("registrations %d, redirects %s %s", f.registrations(), q1.Get("redirect_uri"), q2.Get("redirect_uri"))
	}
	f.provider.mu.Lock()
	registered := f.provider.registered
	f.provider.mu.Unlock()
	if len(registered) != 1 || registered[0] != q2.Get("redirect_uri") {
		t.Fatalf("registered %v for %s", registered, q2.Get("redirect_uri"))
	}
}

func TestAnAppReceiverIsRegisteredExactly(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	_, q1 := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	_, q2 := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	_, q3 := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:45678/callback"})
	if f.registrations() != 2 || q1.Get("client_id") != q2.Get("client_id") || q3.Get("redirect_uri") != "http://127.0.0.1:45678/callback" {
		t.Fatalf("registrations %d", f.registrations())
	}
}

// A pre-registered client is used first: no registration, its exact redirect.
func TestAPreRegisteredClientComesFirst(t *testing.T) {
	port := freePort(t)
	redirect := "http://127.0.0.1:" + port + "/callback"
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{StaticClientID: "console-client", StaticRedirectURI: redirect, StaticClientSecret: "console-secret"})
	got, _ := f.conns.Get(context.Background(), "burnlite")
	if got.StaticClientID != "console-client" || !got.StaticSecretSet {
		t.Fatalf("state %+v", got)
	}
	begun, q := f.begin(t, OAuthFlowRequest{})
	if f.registrations() != 0 || q.Get("client_id") != "console-client" || q.Get("redirect_uri") != redirect {
		t.Fatalf("registrations %d, %v", f.registrations(), q)
	}
	resp, err := (&http.Client{Timeout: time.Second}).Get(begun.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	f.provider.mu.Lock()
	secret := f.provider.secretsSeen[len(f.provider.secretsSeen)-1]
	f.provider.mu.Unlock()
	if secret != "console-secret" {
		t.Fatal("the pre-registered client's secret was not sent")
	}
	// Its port is fixed: when taken, the attempt fails instead of using another.
	if _, err := f.flows.Begin(context.Background(), OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"}); err == nil {
		t.Fatal("began on an occupied pre-registered port")
	}
	// An app must use the registered redirect.
	if _, err := f.flows.Begin(context.Background(), OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:1/callback"}); err == nil {
		t.Fatal("an app used a redirect the client is not registered for")
	}
}

func TestPreRegistrationIsABareLoopbackRedirect(t *testing.T) {
	db := newIntegrationStore(t)
	conns := NewMCPConnectionService(db, MCPConnectionOptions{Secrets: flowMemorySecrets{&memorySecrets{values: map[string]string{}}}})
	for _, bad := range []string{
		"http://127.0.0.1:8080/callback?code=abc&state=x",
		"https://legato.example.test/oauth/callback",
		"http://127.0.0.1/callback",
		"http://localhost:8080/callback",
		"http://127.0.0.1:8080/other",
		"legato://oauth",
	} {
		_, err := conns.Add(context.Background(), MCPConnectionInput{Name: "s", URL: "https://mcp.example.test/mcp", StaticClientID: "c", StaticRedirectURI: bad})
		if !errors.Is(err, ErrMCPConnectionInvalid) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if _, err := conns.Add(context.Background(), MCPConnectionInput{Name: "s", URL: "https://mcp.example.test/mcp", StaticClientSecret: "orphan"}); !errors.Is(err, ErrMCPConnectionInvalid) {
		t.Fatalf("secret without client: %v", err)
	}
}

// A client ID metadata document is used only when Legato has one that lists
// the receiver's redirect, and the server advertises support.
func TestAClientMetadataDocumentIsUsedOnlyWhenItDescribesTheReceiver(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	document := f.provider.srv.URL + "/client.json"
	f.flows.opts.ClientMetadataDocument = document
	f.provider.mu.Lock()
	f.provider.cimd = true
	f.provider.cimdRedirects = []string{"http://127.0.0.1:34567/callback"}
	f.provider.mu.Unlock()
	_, q := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if q.Get("client_id") != document || f.registrations() != 0 {
		t.Fatalf("CIMD not used: %v, %d registrations", q.Get("client_id"), f.registrations())
	}
	_, q = f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:45678/callback"})
	if q.Get("client_id") == document || f.registrations() != 1 {
		t.Fatal("a document that does not list the redirect was used")
	}
}

func TestNoUsableRegistrationIsAClearLimit(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	f.provider.mu.Lock()
	f.provider.noDCR, f.provider.cimd = true, true
	f.provider.mu.Unlock()
	_, err := f.flows.Begin(context.Background(), OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"})
	if !errors.Is(err, ErrOAuthRegistrationUnsupported) {
		t.Fatalf("no registration path: %v", err)
	}
}

func TestMissingServerMetadataStopsBeforeAnyGuessedEndpoint(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	f.provider.mu.Lock()
	f.provider.noASMetadata = true
	f.provider.mu.Unlock()
	if _, err := f.flows.Begin(context.Background(), OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"}); err == nil {
		t.Fatal("began without authorization server metadata")
	}
	if f.registrations() != 0 {
		t.Fatal("registered at a guessed endpoint")
	}
}

func TestRequestedScopesFollowTheServer(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	set := func(change func(p *fakeOAuth)) {
		f.provider.mu.Lock()
		change(f.provider)
		f.provider.mu.Unlock()
	}
	app := OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"}
	if _, q := f.begin(t, app); q.Has("scope") {
		t.Fatalf("scope sent with none advertised: %q", q.Get("scope"))
	}
	set(func(p *fakeOAuth) {
		p.prmScopes = []string{"read:tasks", "write:tasks"}
		p.asScopes = []string{"read:tasks", "offline_access"}
	})
	if _, q := f.begin(t, app); q.Get("scope") != "read:tasks write:tasks offline_access" {
		t.Fatalf("resource scopes: %q", q.Get("scope"))
	}
	set(func(p *fakeOAuth) {
		p.challenge = `Bearer resource_metadata="` + p.srv.URL + `/.well-known/oauth-protected-resource", scope="read:tasks"`
	})
	begun, q := f.begin(t, app)
	if q.Get("scope") != "read:tasks offline_access" {
		t.Fatalf("challenge scope: %q", q.Get("scope"))
	}
	status, _ := f.flows.Status(context.Background(), begun.ID, "operator")
	if status.Scope != "read:tasks offline_access" {
		t.Fatalf("the operator is not shown the requested scope: %+v", status)
	}
}

// A reused registration the server no longer knows is forgotten, so the next
// attempt registers again instead of failing forever.
func TestARejectedReusedRegistrationIsForgotten(t *testing.T) {
	f := newRegistrationFixture(t, OAuthFlowOptions{}, MCPConnectionInput{})
	app := OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"}
	f.begin(t, app)
	begun, q := f.begin(t, app)
	f.provider.mu.Lock()
	f.provider.refuseClient = true
	f.provider.mu.Unlock()
	status, err := f.flows.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, oauthAppResponse(q))
	if err != nil || status.Status != "failed" {
		t.Fatalf("delivery: %+v %v", status, err)
	}
	f.begin(t, app)
	if f.registrations() != 2 {
		t.Fatalf("registrations %d", f.registrations())
	}
}

func oauthAppResponse(q url.Values) oauthclient.AuthorizationResponse {
	return oauthclient.AuthorizationResponse{Code: "code-1", State: q.Get("state")}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func waitForPortFree(t *testing.T, redirect string) {
	t.Helper()
	u, _ := url.Parse(redirect)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", u.Host)
		if err == nil {
			ln.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s stayed bound", u.Host)
}
