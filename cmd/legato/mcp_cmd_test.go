package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startMCPServe runs `legato mcp serve <args>` against a database holding one
// running agent session, and returns an MCP client connected to it the way an
// agent tool is: over the command's stdin and stdout.
func startMCPServe(t *testing.T, args ...string) (*mcp.ClientSession, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task1", Title: "Explain the sync design", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-task1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task1", ExecutionTaskID: "task1", TaskTitle: "Explain the sync design", AgentKind: "claude-code", WorkingDir: "/work/legato", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkExecutionAgentSession(ctx, "ex-task1", sessionID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\nagents:\n  mcp:\n    resource_domains: [cdn.example.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	oldFactory := newAgentContractTmux
	newAgentContractTmux = func() (service.TmuxManager, error) {
		return &contractTmux{alive: map[string]bool{"legato-task1": true}}, nil
	}
	t.Cleanup(func() { newAgentContractTmux = oldFactory })

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	exited := make(chan int, 1)
	go func() { exited <- runMCPServe(args, serverIn, serverOut) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "0"}, nil)
	connectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	t.Cleanup(cancel)
	session, err := client.Connect(connectCtx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		select {
		case code := <-exited:
			if code != exitOK {
				t.Errorf("mcp serve exited with %d after its client left", code)
			}
		case <-time.After(10 * time.Second):
			t.Error("mcp serve kept running after its client left")
		}
	})
	return session, dbPath
}

func mcpResultText(result *mcp.CallToolResult) string {
	var out strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			out.WriteString(text.Text)
		}
	}
	return out.String()
}

func TestMCPServeActsForTheSessionNamedByItsEnvironment(t *testing.T) {
	t.Setenv("LEGATO_TASK_ID", "task1")
	t.Setenv("LEGATO_EXECUTION_ID", "ex-task1")
	session, dbPath := startMCPServe(t)
	ctx := context.Background()

	info, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "session_info"})
	if err != nil || info.IsError || !strings.Contains(mcpResultText(info), "Explain the sync design") {
		t.Fatalf("session_info = %v, %v", info, err)
	}

	shown, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "show_artifact", Arguments: map[string]any{"title": "Sync design", "content": "<h1>Design</h1>"}})
	if err != nil || shown.IsError {
		t.Fatalf("show_artifact = %v, %v", shown, err)
	}
	match := regexp.MustCompile(`\[legato-ui:(ui-[0-9a-f]{10}):1\]`).FindStringSubmatch(mcpResultText(shown))
	if match == nil {
		t.Fatalf("no item marker in %q", mcpResultText(shown))
	}
	verify, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	item, err := verify.GetAgentUIItem(ctx, match[1], 0)
	if err != nil || item.TaskID != "task1" || item.ExecutionID != "ex-task1" || item.Content != "<h1>Design</h1>" {
		t.Fatalf("stored item = %#v, %v", item, err)
	}

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, tool := range listed.Tools {
		names[tool.Name] = tool.Description
	}
	if _, ok := names["list_peers"]; !ok {
		t.Fatalf("peer tools are missing: %v", names)
	}
	if !strings.Contains(names["show_card"], "cdn.example.com") {
		t.Fatalf("show_card does not name the configured content host: %q", names["show_card"])
	}
}

func TestMCPServeFlagsNameTheSessionWhenTheEnvironmentDoesNot(t *testing.T) {
	t.Setenv("LEGATO_TASK_ID", "")
	t.Setenv("LEGATO_EXECUTION_ID", "")
	session, _ := startMCPServe(t, "--task", "task1", "--execution-id", "ex-task1")

	info, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "session_info"})
	if err != nil || info.IsError || !strings.Contains(mcpResultText(info), "task1") {
		t.Fatalf("session_info = %v, %v", info, err)
	}
}

func TestMCPServeOffersNothingOutsideALegatoSession(t *testing.T) {
	t.Setenv("LEGATO_TASK_ID", "")
	t.Setenv("LEGATO_EXECUTION_ID", "")
	session, _ := startMCPServe(t)

	if listed, err := session.ListTools(context.Background(), nil); err == nil && len(listed.Tools) != 0 {
		t.Fatalf("tools = %#v, want none", listed.Tools)
	}
}

func TestMCPServeIsARegisteredIntegrationCommand(t *testing.T) {
	node, ok := findCommand(commandTree(), []string{"mcp", "serve"})
	if !ok || node.Run == nil {
		t.Fatal("mcp serve is not a registered leaf command")
	}
	if !node.Capabilities.Integration || !node.Capabilities.LongRunning {
		t.Fatalf("capabilities = %#v, want a long-running integration command", node.Capabilities)
	}
}
