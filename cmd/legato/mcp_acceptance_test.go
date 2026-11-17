package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Values that must never leave the host broker: no route answer, agent
// payload, audit entry or log line may hold them.
const (
	sentinelAccess  = "sentinel-access-7f3a"
	sentinelRefresh = "sentinel-refresh-91c2"
	sentinelCode    = "sentinel-code-5d0e"
)

// acceptanceProvider is one upstream MCP server and its OAuth
// authorization server, both on one fixture address.
type acceptanceProvider struct {
	srv      *httptest.Server
	mu       sync.Mutex
	tokens   map[string]bool
	seen     []string
	revoked  []string
	searches int
}

func newAcceptanceProvider(t *testing.T) *acceptanceProvider {
	t.Helper()
	p := &acceptanceProvider{tokens: map[string]bool{}}
	up := mcp.NewServer(&mcp.Implementation{Name: "notion", Version: "1"}, nil)
	up.AddTool(&mcp.Tool{Name: "search", Description: "Search pages.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			p.mu.Lock()
			p.searches++
			p.mu.Unlock()
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "3 pages"}}}, nil
		})
	up.AddTool(&mcp.Tool{Name: "delete_page", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "deleted"}}}, nil
		})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return up }, nil)
	mux := http.NewServeMux()
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	base := p.srv.URL
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p.mu.Lock()
		ok := p.tokens[token]
		if token != "" {
			p.seen = append(p.seen, token)
		}
		p.mu.Unlock()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+base+`/.well-known/oauth-protected-resource", scope="read"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}, "scopes_supported": []string{"read"}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token",
			"registration_endpoint": base + "/register", "revocation_endpoint": base + "/revoke", "code_challenge_methods_supported": []string{"S256"},
			"authorization_response_iss_parameter_supported": true})
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 201, map[string]any{"client_id": "legato-acceptance"})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		to, _ := url.Parse(q.Get("redirect_uri"))
		values := url.Values{"state": {q.Get("state")}, "iss": {base}}
		if q.Get("prompt") == "deny" {
			values.Set("error", "access_denied")
		} else {
			values.Set("code", sentinelCode)
		}
		to.RawQuery = values.Encode()
		http.Redirect(w, r, to.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") == "authorization_code" && r.PostForm.Get("code") != sentinelCode {
			reply(w, 400, map[string]any{"error": "invalid_grant"})
			return
		}
		p.mu.Lock()
		p.tokens[sentinelAccess] = true
		p.mu.Unlock()
		reply(w, 200, map[string]any{"access_token": sentinelAccess, "refresh_token": sentinelRefresh, "token_type": "Bearer", "expires_in": 3600, "scope": "read"})
	})
	mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p.mu.Lock()
		p.revoked = append(p.revoked, r.PostForm.Get("token"))
		delete(p.tokens, sentinelAccess)
		p.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return p
}

type acceptance struct {
	db       *store.Store
	dbPath   string
	srv      *server.Server
	host     *brokerHost
	provider *acceptanceProvider
	seen     bytes.Buffer // every route answer and agent payload
}

// call is the operator's web UI calling a route.
func (a *acceptance) call(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	a.srv.Handler().ServeHTTP(rec, req)
	a.seen.WriteString(rec.Body.String())
	return rec
}

// agent opens an agent's `legato mcp serve` for an execution in repo.
func (a *acceptance) agent(t *testing.T, task, execution, repo string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	if task != "task1" {
		if err := a.db.CreateTask(ctx, store.Task{ID: task, Title: task, Status: "Doing"}); err != nil {
			t.Fatal(err)
		}
		if err := a.db.InsertAgentSession(ctx, store.AgentSession{TaskID: task, TmuxSession: "legato-" + task, Command: "claude", Status: "running"}); err != nil {
			t.Fatal(err)
		}
		session, _ := a.db.GetAgentSessionByTaskID(ctx, task)
		if err := a.db.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: task, ExecutionTaskID: task, AgentSessionID: &session.ID, StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	token := "lgb_" + execution
	if err := a.db.SetExecutionBrokerToken(ctx, execution, service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	if err := a.db.SetCodeContext(ctx, task, &store.CodeContext{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_TASK_ID", task)
	t.Setenv("LEGATO_EXECUTION_ID", execution)
	t.Setenv("LEGATO_BROKER_SOCKET", a.host.SocketPath())
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	exited := make(chan int, 1)
	go func() { exited <- runMCPServe(nil, serverIn, serverOut) }()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "agent-" + execution, Version: "0"}, nil).
		Connect(cctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		<-exited
	})
	return session
}

func (a *acceptance) tools(t *testing.T, s *mcp.ClientSession) map[string]bool {
	t.Helper()
	listed, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(listed)
	a.seen.Write(raw)
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	return names
}

func (a *acceptance) use(t *testing.T, s *mcp.ClientSession, name string) (string, bool) {
	t.Helper()
	result, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"query": "roadmap"}})
	if err != nil {
		a.seen.WriteString(err.Error())
		return err.Error(), true
	}
	raw, _ := json.Marshal(result)
	a.seen.Write(raw)
	return mcpResultText(result), result.IsError
}

// browse is the operator's browser on Legato's computer: it follows the
// consent page's redirect to the loopback receiver Legato bound.
func browse(t *testing.T, authorize string) {
	t.Helper()
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(authorize)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func newAcceptance(t *testing.T) *acceptance {
	t.Helper()
	previous := mcpEgress
	mcpEgress = func(p egress.Policy) egress.Policy {
		p.LoopbackFixture = true
		return p
	}
	t.Cleanup(func() { mcpEgress = previous })
	db, dbPath, _, _ := brokerFixture(t)
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	credentials, err := service.NewCredentialBroker(db, approvals, service.CredentialBrokerOptions{WaitTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	rt, stop := startMCPBroker(&config.Config{DataDir: t.TempDir(), Secrets: config.SecretsConfig{MasterKey: "file"}}, db, credentials, nil)
	t.Cleanup(stop)
	srv := server.New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	rt.attach(srv)
	host := startBrokerHost(context.Background(), db, shortDir(t), brokerHostOptions{Deps: cli.BrokerDeps{
		Ops: cli.MCPBrokerOps(func(cli.BrokerCaller) cli.MCPOptions { return cli.MCPOptions{Store: db, Upstream: rt.upstream()} }),
	}})
	t.Cleanup(host.Stop)
	return &acceptance{db: db, dbPath: dbPath, srv: srv, host: host, provider: newAcceptanceProvider(t)}
}

// The whole path on one Legato: the operator adds and authorizes a server
// from a browser on Legato's computer, confirms the account, grants a tool;
// two agents in the granted repository share the one authorization, an
// agent elsewhere gets nothing and is audited as itself; disconnecting
// revokes upstream and stops every agent at once. No token leaves the host.
func TestAcceptanceOneAuthorizationServesGrantedAgentsOnly(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	a := newAcceptance(t)
	ctx := context.Background()

	rec := a.call(t, http.MethodPost, "/api/settings/mcp/connections", fmt.Sprintf(`{"name":"notion","url":%q}`, a.provider.srv.URL+"/mcp"))
	var added service.MCPConnectionState
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &added) != nil {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	rec = a.call(t, http.MethodPatch, "/api/settings/mcp/connections/notion", fmt.Sprintf(`{"rules":[{"tools":"search","repository":"acme/app","approve":"auto"}],"expected_revision":%d}`, added.Revision))
	if rec.Code != http.StatusOK {
		t.Fatalf("rules: %d %s", rec.Code, rec.Body)
	}

	// A refused consent ends the attempt without an authorization.
	rec = a.call(t, http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"web"}`)
	var denied service.OAuthFlowBegin
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &denied) != nil || strings.Contains(rec.Body.String(), "return_key") {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	browse(t, denied.AuthorizeURL+"&prompt=deny")
	if rec := a.call(t, http.MethodGet, "/api/mcp/oauth/attempts/"+denied.ID, ""); !strings.Contains(rec.Body.String(), `"status":"denied"`) {
		t.Fatalf("denied: %s", rec.Body)
	}

	rec = a.call(t, http.MethodPost, "/api/mcp/connections/notion/oauth/begin", `{"mode":"web"}`)
	var begun service.OAuthFlowBegin
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &begun) != nil {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	browse(t, begun.AuthorizeURL)
	rec = a.call(t, http.MethodGet, "/api/mcp/oauth/attempts/"+begun.ID, "")
	var status service.OAuthFlowStatus
	if json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.Status != "awaiting_confirmation" || status.Evidence == nil || status.Evidence.Issuer != a.provider.srv.URL {
		t.Fatalf("status: %s", rec.Body)
	}
	// Until the operator confirms, no agent can use it.
	first := a.agent(t, "task1", "ex-1", "acme/app")
	if a.tools(t, first)["notion__search"] {
		t.Fatal("an unconfirmed authorization was offered to an agent")
	}
	if rec := a.call(t, http.MethodPost, "/api/mcp/oauth/attempts/"+begun.ID+"/confirm", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body)
	}

	// Agents learn of tools when their session starts: a session of the
	// same agent started after confirming gets the tool.
	first = a.agent(t, "task1", "ex-1", "acme/app")
	second := a.agent(t, "task2", "ex-2", "acme/app")
	other := a.agent(t, "task3", "ex-3", "acme/other")
	for _, s := range []*mcp.ClientSession{first, second} {
		names := a.tools(t, s)
		if !names["notion__search"] || names["notion__delete_page"] {
			t.Fatalf("a granted agent's tools: %v", names)
		}
		if text, failed := a.use(t, s, "notion__search"); failed || !strings.Contains(text, "3 pages") {
			t.Fatalf("search: %q", text)
		}
	}
	if names := a.tools(t, other); names["notion__search"] {
		t.Fatalf("an agent in another repository was offered the tool: %v", names)
	}
	if _, failed := a.use(t, other, "notion__search"); !failed {
		t.Fatal("an agent in another repository used the tool")
	}
	// An agent that names the tool to the host itself, past its MCP
	// server, is refused there and audited as the execution it is.
	var direct any
	callErr := broker.Call(ctx, a.host.SocketPath(), "lgb_ex-3", "mcp.call", map[string]any{"name": "notion__search", "arguments": map[string]any{"query": "roadmap"}}, &direct)
	raw, _ := json.Marshal(direct)
	a.seen.Write(raw)
	if callErr == nil && !strings.Contains(string(raw), `"is_error":true`) {
		t.Fatalf("the host ran a call for an agent in another repository: %s", raw)
	}
	if _, failed := a.use(t, first, "notion__delete_page"); !failed {
		t.Fatal("an ungranted tool ran")
	}
	a.provider.mu.Lock()
	searches, seen := a.provider.searches, append([]string(nil), a.provider.seen...)
	a.provider.mu.Unlock()
	if searches != 2 {
		t.Fatalf("%d searches reached the server", searches)
	}
	for _, token := range seen {
		if token != sentinelAccess {
			t.Fatalf("the upstream saw a token other than the one authorization: %q", token)
		}
	}
	entries, err := a.db.ListCredentialAudit(ctx, store.CredentialAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var refusedOther bool
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		a.seen.Write(raw)
		if e.Credential == "mcp:notion" && e.ExecutionID == "ex-3" && e.Event == "refuse" && strings.HasPrefix(e.Resource, "acme/other/") {
			refusedOther = true
		}
	}
	if !refusedOther {
		t.Fatalf("the other repository's refusal was not audited as ex-3: %+v", entries)
	}

	rec = a.call(t, http.MethodPost, "/api/mcp/connections/notion/oauth/disconnect", `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"upstream":"revoked"`) {
		t.Fatalf("disconnect: %d %s", rec.Code, rec.Body)
	}
	if text, failed := a.use(t, second, "notion__search"); !failed {
		t.Fatalf("a disconnected server still served an agent: %q", text)
	}
	a.provider.mu.Lock()
	revoked := append([]string(nil), a.provider.revoked...)
	a.provider.mu.Unlock()
	if len(revoked) != 1 || (revoked[0] != sentinelRefresh && revoked[0] != sentinelAccess) {
		t.Fatalf("revoked %q", revoked)
	}

	// The database holds tokens only sealed.
	var stored []byte
	for _, file := range []string{a.dbPath, a.dbPath + "-wal"} {
		if raw, err := os.ReadFile(file); err == nil {
			stored = append(stored, raw...)
		}
	}
	for _, secret := range []string{sentinelAccess, sentinelRefresh, sentinelCode} {
		if bytes.Contains(stored, []byte(secret)) {
			t.Errorf("%s is in the database unsealed", secret)
		}
		if strings.Contains(a.seen.String(), secret) {
			t.Errorf("%s reached a route answer, an agent or the audit", secret)
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("%s reached the log", secret)
		}
	}
}
