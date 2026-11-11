package cli_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// seedReeve gives the instance a running Reeve session with an API token.
func seedReeve(t *testing.T, s *store.Store) (taskID, executionID string) {
	t.Helper()
	ctx := context.Background()
	taskID, err := s.CreateReeveTask(ctx, "The Reeve")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertReeveSession(ctx, store.ReeveSession{TaskID: taskID, State: store.ReeveRunning, APIToken: "lgr_test", APITokenHash: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	executionID = linkTestExecution(t, s, taskID)
	session, _ := s.GetAgentSessionByTaskID(ctx, taskID)
	s.LinkExecutionAgentSession(ctx, executionID, session.ID)
	return taskID, executionID
}

func reeveToolNames(t *testing.T, session *mcp.ClientSession) map[string]bool {
	t.Helper()
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestTheReevesToolsAreServedOnlyToTheReeve(t *testing.T) {
	s := newTestStore(t)
	reeve := &cli.MCPReeve{Snapshot: func(context.Context) string { return "## This instance\n- Running agents: 3" }}
	taskID, executionID := seedReeve(t, s)
	other := seedMCPAgent(t, s, "work-1")

	names := reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve}))
	for _, tool := range []string{"docs", "instance_status", "read_logs", "legato_api"} {
		if !names[tool] {
			t.Errorf("the Reeve lacks %s", tool)
		}
	}
	names = reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: "work-1", ExecutionID: other, Reeve: reeve}))
	if names["docs"] || names["legato_api"] {
		t.Fatal("another session is offered the Reeve's tools")
	}
}

func TestTheReeveReadsTheManualTheStatusAndTheLog(t *testing.T) {
	s := newTestStore(t)
	logPath := filepath.Join(t.TempDir(), "legato.log")
	os.WriteFile(logPath, []byte("2026/11/09 sync: ok\n2026/11/09 sync: jira answered 401\n"), 0o600)
	taskID, executionID := seedReeve(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{
		Snapshot: func(context.Context) string { return "## This instance\n- Running agents: 3" }, LogPath: logPath}})

	if got := mcpText(callMCP(t, session, "docs", map[string]any{"action": "read", "path": "using/reeve"})); !strings.Contains(got, "The Reeve") {
		t.Fatalf("docs read: %s", got)
	}
	if got := mcpText(callMCP(t, session, "docs", map[string]any{"action": "search", "query": "restarts itself on request"})); !strings.Contains(got, "running/updates") {
		t.Fatalf("docs search: %s", got)
	}
	if got := mcpText(callMCP(t, session, "instance_status", nil)); !strings.Contains(got, "Running agents: 3") {
		t.Fatalf("status: %s", got)
	}
	if got := mcpText(callMCP(t, session, "read_logs", map[string]any{"filter": "401"})); got != "2026/11/09 sync: jira answered 401" {
		t.Fatalf("logs: %q", got)
	}
}

func TestTheReeveCallsTheAPIWithItsOwnToken(t *testing.T) {
	s := newTestStore(t)
	var auth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"agents":[]}`))
	}))
	defer api.Close()
	taskID, executionID := seedReeve(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{APIBase: api.URL, HTTP: api.Client()}})
	got := mcpText(callMCP(t, session, "legato_api", map[string]any{"method": "GET", "path": "/api/agents"}))
	if !strings.Contains(got, "200") || !strings.Contains(got, `"agents"`) || auth != "Bearer lgr_test" {
		t.Fatalf("result %q, auth %q", got, auth)
	}
}

type fakeChangeSets struct {
	reason string
	calls  []service.ChangeCall
}

func (f *fakeChangeSets) Propose(_ context.Context, _ string, reason string, calls []service.ChangeCall) (service.ChangeSetOutcome, error) {
	f.reason, f.calls = reason, calls
	return service.ChangeSetOutcome{ID: "cs-1", ApprovalID: "ap-1", State: "pending", Level: service.LevelApprove}, nil
}

func TestTheReeveProposesAChangeSet(t *testing.T) {
	s := newTestStore(t)
	sets := &fakeChangeSets{}
	taskID, executionID := seedReeve(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{ChangeSets: sets}})
	got := mcpText(callMCP(t, session, "propose_changes", map[string]any{
		"reason": "turn reviews off",
		"calls":  []any{map[string]any{"kind": "api", "method": "PUT", "path": "/api/config/settings/agents.reviews", "body": map[string]any{"value": false}}, map[string]any{"kind": "restart"}},
	}))
	if !strings.Contains(got, "waits for the operator's approval") || sets.reason != "turn reviews off" || len(sets.calls) != 2 || sets.calls[1].Kind != "restart" {
		t.Fatalf("result %q, proposed %+v", got, sets)
	}
}

// A change made with legato_api is refused to the Reeve's token and becomes
// a change set of one.
func TestAChangeThroughTheAPIToolBecomesASetOfOne(t *testing.T) {
	s := newTestStore(t)
	sets := &fakeChangeSets{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":"needs_approval"}`))
	}))
	defer api.Close()
	taskID, executionID := seedReeve(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{APIBase: api.URL, HTTP: api.Client(), ChangeSets: sets}})
	got := mcpText(callMCP(t, session, "legato_api", map[string]any{"method": "POST", "path": "/api/restart"}))
	if !strings.Contains(got, "cs-1") || len(sets.calls) != 1 || sets.calls[0].Path != "/api/restart" || sets.reason != "POST /api/restart" {
		t.Fatalf("result %q, proposed %+v", got, sets)
	}
}

type fakeLegato struct{ ran [][]string }

func (f *fakeLegato) Classify(args []string) (string, string) {
	switch args[0] {
	case "version":
		return "read", "Print the version"
	case "serve":
		return "never", "Run the web server"
	}
	return "risky", "Create an integration token"
}

func (f *fakeLegato) Run(_ context.Context, args []string) (int, string, error) {
	f.ran = append(f.ran, args)
	return 0, "legato v1.2.3", nil
}

func TestTheLegatoToolReadsAtOnceAndProposesChanges(t *testing.T) {
	s := newTestStore(t)
	legato, sets := &fakeLegato{}, &fakeChangeSets{}
	taskID, executionID := seedReeve(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{Legato: legato, ChangeSets: sets}})

	if got := mcpText(callMCP(t, session, "legato", map[string]any{"args": []string{"version"}})); !strings.Contains(got, "v1.2.3") || len(legato.ran) != 1 {
		t.Fatalf("a read: %q", got)
	}
	if got := mcpText(callMCP(t, session, "legato", map[string]any{"args": []string{"integration", "token", "create", "argus"}})); !strings.Contains(got, "cs-1") || len(sets.calls) != 1 || sets.calls[0].Kind != "legato" {
		t.Fatalf("a change: %q, %+v", got, sets.calls)
	}
	if result := callMCP(t, session, "legato", map[string]any{"args": []string{"serve"}}); !result.IsError {
		t.Fatal("a never command was not refused")
	}
	if len(legato.ran) != 1 {
		t.Fatal("a change or a never command ran at once")
	}
}

func TestHostToolsAreOfferedOnlyWhileHostOperationsAreOn(t *testing.T) {
	s := newTestStore(t)
	taskID, executionID := seedReeve(t, s)
	on := true
	reeve := &cli.MCPReeve{ChangeSets: &fakeChangeSets{}, HostOperations: func(context.Context) bool { return on }}
	if names := reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve})); !names["host_shell"] || !names["edit_file"] || !names["manage_service"] {
		t.Fatalf("with host operations on: %v", names)
	}
	on = false
	if names := reeveToolNames(t, connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: reeve})); names["host_shell"] {
		t.Fatal("host_shell offered with host operations off")
	}
}

type fakeReevePeers struct {
	to, text string
}

func (f *fakeReevePeers) List(context.Context) ([]service.Peer, error) {
	return []service.Peer{{InstanceID: "srv-0123456789abcdef", Name: "erebus", Via: service.PeerViaWorker}}, nil
}

func (f *fakeReevePeers) Send(_ context.Context, to, text string) (service.SendOutcome, error) {
	f.to, f.text = to, text
	return service.SendOutcome{To: service.Peer{Name: "erebus"}, State: "delivered", Replies: []string{"all well here"}}, nil
}

// The Reeve messages a paired instance's Reeve, and lists them when it
// names none.
func TestTheReeveMessagesAnotherInstancesReeve(t *testing.T) {
	s := newTestStore(t)
	peers := &fakeReevePeers{}
	taskID, executionID := seedReeve(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: taskID, ExecutionID: executionID, Reeve: &cli.MCPReeve{Peers: peers}})
	got := mcpText(callMCP(t, session, "message_reeve", map[string]any{"to": "erebus", "text": "how are your workers?"}))
	if peers.to != "erebus" || peers.text != "how are your workers?" || !strings.Contains(got, "all well here") {
		t.Fatalf("result %q, sent %+v", got, peers)
	}
	listed := mcpText(callMCP(t, session, "message_reeve", map[string]any{}))
	if !strings.Contains(listed, "erebus") {
		t.Fatalf("listing %q", listed)
	}
}
