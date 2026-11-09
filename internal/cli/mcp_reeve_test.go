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
