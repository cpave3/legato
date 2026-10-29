package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// seedMCPAgent gives a task a running agent session and the execution that
// owns it, as Legato does before it starts an agent.
func seedMCPAgent(t *testing.T, s *store.Store, taskID string) string {
	t.Helper()
	ctx := context.Background()
	seedTask(t, s, taskID, "Doing")
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, taskID)
	session, err := s.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkExecutionAgentSession(ctx, executionID, session.ID); err != nil {
		t.Fatal(err)
	}
	return executionID
}

// connectMCP serves Legato's tools over a pair of pipes, as `legato mcp
// serve` serves them over stdio, and returns an MCP client on the other end.
func connectMCP(t *testing.T, opts cli.MCPOptions) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	go func() { _ = mcpserver.Serve(ctx, cli.MCPServerOptions(opts), serverIn, serverOut) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callMCP(t *testing.T, session *mcp.ClientSession, tool string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return result
}

func mcpText(result *mcp.CallToolResult) string {
	var out strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			out.WriteString(text.Text)
		}
	}
	return out.String()
}

var uiMarker = regexp.MustCompile(`\[legato-ui:(ui-[0-9a-f]{10}):(\d+)\]`)

func TestMCPShowArtifactStoresItForTheCallingSessionAndNotifiesLegato(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	received := make(chan ipc.Message, 4)
	server := newTestIPCServer(t, filepath.Join(ipc.SocketDir(), "legato-mcp-test.sock"), func(msg ipc.Message) { received <- msg })
	defer server.Close()

	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})
	result := callMCP(t, session, "show_artifact", map[string]any{
		"title": "Sync design", "content": "<h1>Design</h1>", "format": "html",
	})
	if result.IsError {
		t.Fatalf("show_artifact failed: %s", mcpText(result))
	}
	match := uiMarker.FindStringSubmatch(mcpText(result))
	if match == nil || match[2] != "1" {
		t.Fatalf("result has no item marker at revision 1: %q", mcpText(result))
	}
	// Some clients hand the model a result's structured content in place of
	// its text. The text carries the marker and the guidance, so it must be
	// all there is.
	if result.StructuredContent != nil {
		t.Fatalf("a display tool returned structured content beside its text: %#v", result.StructuredContent)
	}
	if !strings.Contains(mcpText(result), "do not repeat") {
		t.Fatalf("result does not tell the model the person already sees it: %q", mcpText(result))
	}

	ui := service.NewAgentUIService(s, nil, service.AgentUIOptions{})
	item, err := ui.Get(context.Background(), match[1], 0)
	if err != nil {
		t.Fatal(err)
	}
	if item.TaskID != "task1" || item.Placement != service.AgentUIPanel || item.Format != service.AgentUIFormatHTML || item.Title != "Sync design" || item.Content != "<h1>Design</h1>" {
		t.Fatalf("stored item = %#v", item)
	}

	select {
	case msg := <-received:
		if msg.Type != "agent_ui_changed" || msg.TaskID != "task1" || msg.ItemID != match[1] || msg.Placement != "panel" || msg.Revision != 1 || msg.Kind != "shown" {
			t.Fatalf("ipc message = %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("running Legato instances were not told about the artifact")
	}
}

func TestMCPOffersNoToolsOutsideALegatoSession(t *testing.T) {
	session := connectMCP(t, cli.MCPOptions{Store: newTestStore(t)})

	if listed, err := session.ListTools(context.Background(), nil); err == nil && len(listed.Tools) != 0 {
		t.Fatalf("tools outside a session = %#v", listed.Tools)
	}
	if instructions := session.InitializeResult().Instructions; !strings.Contains(instructions, "LEGATO_TASK_ID") {
		t.Fatalf("instructions do not say why there are no tools: %q", instructions)
	}
}

func TestMCPToolsRefuseASessionLegatoIsNotRunning(t *testing.T) {
	s := newTestStore(t)
	seedMCPAgent(t, s, "task1")
	seedTask(t, s, "idle-task", "Doing")
	arguments := map[string]any{"title": "Design", "content": "<p>x</p>"}

	for name, opts := range map[string]cli.MCPOptions{
		"task without a running agent":        {Store: s, TaskID: "idle-task"},
		"execution that is not the session's": {Store: s, TaskID: "task1", ExecutionID: "ex-someone-else"},
	} {
		t.Run(name, func(t *testing.T) {
			result := callMCP(t, connectMCP(t, opts), "show_artifact", arguments)
			if !result.IsError {
				t.Fatalf("call was accepted: %s", mcpText(result))
			}
			for _, taskID := range []string{"task1", "idle-task"} {
				items, err := service.NewAgentUIService(s, nil, service.AgentUIOptions{}).List(context.Background(), taskID)
				if err != nil || len(items) != 0 {
					t.Fatalf("items stored for %s after a refused call: %#v, %v", taskID, items, err)
				}
			}
		})
	}
}

func TestMCPShowCardStoresAnInlineCardWithItsFallbackText(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	result := callMCP(t, session, "show_card", map[string]any{
		"html": `<div class="weather">27 C</div>`, "fallback_text": "27 C and sunny in Brisbane", "title": "Weather in Brisbane",
	})
	if result.IsError {
		t.Fatalf("show_card failed: %s", mcpText(result))
	}
	match := uiMarker.FindStringSubmatch(mcpText(result))
	if match == nil {
		t.Fatalf("result has no item marker: %q", mcpText(result))
	}
	item, err := service.NewAgentUIService(s, nil, service.AgentUIOptions{}).Get(context.Background(), match[1], 0)
	if err != nil {
		t.Fatal(err)
	}
	if item.Placement != service.AgentUIInline || item.Format != service.AgentUIFormatHTML || item.Content != `<div class="weather">27 C</div>` || item.FallbackText != "27 C and sunny in Brisbane" || item.Title != "Weather in Brisbane" {
		t.Fatalf("stored card = %#v", item)
	}

	missingFallback := callMCP(t, session, "show_card", map[string]any{"html": "<b>x</b>", "fallback_text": "  "})
	if !missingFallback.IsError || !strings.Contains(mcpText(missingFallback), "fallback_text") {
		t.Fatalf("a card with blank fallback text = %q (error %v)", mcpText(missingFallback), missingFallback.IsError)
	}
}

func TestMCPShowComponentsStoresACardBuiltFromLegatosComponents(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	result := callMCP(t, session, "show_components", map[string]any{
		"fallback_text": "27 C and sunny in Brisbane",
		"components": []any{
			map[string]any{"type": "heading", "text": "Weather in Brisbane"},
			map[string]any{"type": "stats", "items": []any{map[string]any{"label": "Temperature", "value": "27 C", "tone": "positive"}}},
			map[string]any{"type": "bar_chart", "label": "Rain", "items": []any{map[string]any{"label": "Mon", "number": 0}}},
		},
	})
	if result.IsError {
		t.Fatalf("show_components failed: %s", mcpText(result))
	}
	match := uiMarker.FindStringSubmatch(mcpText(result))
	if match == nil {
		t.Fatalf("result has no item marker: %q", mcpText(result))
	}
	item, err := service.NewAgentUIService(s, nil, service.AgentUIOptions{}).Get(context.Background(), match[1], 0)
	if err != nil {
		t.Fatal(err)
	}
	var components []service.UIComponent
	if err := json.Unmarshal([]byte(item.Content), &components); err != nil {
		t.Fatalf("stored content: %v\n%s", err, item.Content)
	}
	if item.Placement != service.AgentUIInline || item.Format != service.AgentUIFormatComponents || len(components) != 3 || components[1].Items[0].Value != "27 C" {
		t.Fatalf("stored card = %#v (%#v)", item, components)
	}

	// The agent is told which component is wrong, so it can correct the call.
	rejected := callMCP(t, session, "show_components", map[string]any{
		"fallback_text": "x",
		"components":    []any{map[string]any{"type": "text", "text": "ok"}, map[string]any{"type": "table", "columns": []any{"a", "b"}, "rows": []any{[]any{"1"}}}},
	})
	if !rejected.IsError || !strings.Contains(mcpText(rejected), "components[1] (table): rows[0] has 1 cells") {
		t.Fatalf("rejected call = %q (error %v)", mcpText(rejected), rejected.IsError)
	}

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "show_components" && !strings.Contains(tool.Description, "bar_chart") {
			t.Fatalf("show_components does not describe its components: %q", tool.Description)
		}
	}
}

func TestMCPShowingAgainUnderTheSameKeyReplacesTheItem(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	first := uiMarker.FindStringSubmatch(mcpText(callMCP(t, session, "show_artifact", map[string]any{"title": "Notes", "content": "# One", "format": "markdown", "key": "notes"})))
	second := uiMarker.FindStringSubmatch(mcpText(callMCP(t, session, "show_artifact", map[string]any{"title": "Notes", "content": "# Two", "format": "markdown", "key": "notes"})))
	if first == nil || second == nil || first[1] != second[1] || first[2] != "1" || second[2] != "2" {
		t.Fatalf("markers = %v then %v, want one item at revisions 1 and 2", first, second)
	}
	items, err := service.NewAgentUIService(s, nil, service.AgentUIOptions{}).List(context.Background(), "task1")
	if err != nil || len(items) != 1 || items[0].Revision != 2 {
		t.Fatalf("items = %#v, %v", items, err)
	}
}

func TestMCPSessionInfoDescribesTheCallingSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedTask(t, s, "task1", "Doing")
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", AgentKind: "codex", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	agent, err := s.GetAgentSessionByTaskID(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{
		ID: "ex-task1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task1", ExecutionTaskID: "task1",
		TaskTitle: "Test task1", AgentSessionID: &agent.ID, AgentKind: "codex", WorkingDir: "/work/legato", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkExecutionAgentSession(ctx, "ex-task1", agent.ID); err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: "ex-task1"})

	result := callMCP(t, session, "session_info", nil)
	if result.IsError {
		t.Fatalf("session_info failed: %s", mcpText(result))
	}
	structured, _ := json.Marshal(result.StructuredContent)
	var info struct {
		TaskID      string `json:"task_id"`
		Title       string `json:"title"`
		AgentKind   string `json:"agent_kind"`
		WorkingDir  string `json:"working_dir"`
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal(structured, &info); err != nil {
		t.Fatalf("structured content %s: %v", structured, err)
	}
	if info.TaskID != "task1" || info.Title != "Test task1" || info.AgentKind != "codex" || info.WorkingDir != "/work/legato" || info.ExecutionID != "ex-task1" {
		t.Fatalf("session info = %#v", info)
	}
	for _, want := range []string{"task1", "Test task1", "codex", "/work/legato"} {
		if !strings.Contains(mcpText(result), want) {
			t.Fatalf("text %q does not mention %q", mcpText(result), want)
		}
	}
}

func TestMCPTaskContextReturnsTheTasksFullContext(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	executionID := seedMCPAgent(t, s, "task1")
	ctx := context.Background()
	if err := cli.TaskDescription(s, "task1", "Fix the retry loop in the sync scheduler."); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddTaskNote(ctx, s, "task1", "Triage: the callback is not bound to an attempt.", "workflow"); err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	result := callMCP(t, session, "task_context", nil)
	if result.IsError {
		t.Fatalf("task_context failed: %s", mcpText(result))
	}
	for _, want := range []string{"Test task1", "Fix the retry loop in the sync scheduler.", "Triage: the callback is not bound to an attempt."} {
		if !strings.Contains(mcpText(result), want) {
			t.Fatalf("task context does not contain %q:\n%s", want, mcpText(result))
		}
	}
}

func TestMCPAddTaskNoteRecordsADurableNoteAndRefreshesLegato(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	received := make(chan ipc.Message, 4)
	server := newTestIPCServer(t, filepath.Join(ipc.SocketDir(), "legato-mcp-note-test.sock"), func(msg ipc.Message) { received <- msg })
	defer server.Close()
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	result := callMCP(t, session, "add_task_note", map[string]any{"body": "Root cause: the scheduler retries a stale attempt."})
	if result.IsError {
		t.Fatalf("add_task_note failed: %s", mcpText(result))
	}
	notes, err := s.ListTaskNotes(context.Background(), "task1")
	if err != nil || len(notes) != 1 || notes[0].Body != "Root cause: the scheduler retries a stale attempt." || notes[0].Source != "agent" {
		t.Fatalf("notes = %#v, %v", notes, err)
	}
	select {
	case msg := <-received:
		if msg.Type != "task_note" || msg.TaskID != "task1" {
			t.Fatalf("ipc message = %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("running Legato instances were not told about the note")
	}

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "add_task_note" && tool.Annotations.ReadOnlyHint {
			t.Fatal("add_task_note is marked read-only, but it writes to the task")
		}
	}
}

// A swarm worker's own identity is its sub-task, which has no description or
// notes of its own: its task tools act on the task the swarm is working on.
func TestMCPTaskToolsOfASwarmWorkerActOnTheParentTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	ctx := context.Background()
	seedTask(t, s, "parent", "Doing")
	if err := cli.TaskDescription(s, "parent", "Migrate the schema."); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSubtask(ctx, store.Subtask{ID: "st-worker0001", ParentTaskID: "parent", Title: "Worker", Status: "dispatched"}); err != nil {
		t.Fatal(err)
	}
	parent, subtask := "parent", "st-worker0001"
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: subtask, TmuxSession: "legato-" + subtask, AgentKind: "chimera", Status: "running", Role: "db", ParentTaskID: &parent, SubtaskID: &subtask}); err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: subtask})

	contextResult := callMCP(t, session, "task_context", nil)
	if contextResult.IsError || !strings.Contains(mcpText(contextResult), "Migrate the schema.") {
		t.Fatalf("worker task context = %q (error %v)", mcpText(contextResult), contextResult.IsError)
	}
	if note := callMCP(t, session, "add_task_note", map[string]any{"body": "Schema migrated."}); note.IsError {
		t.Fatalf("worker add_task_note failed: %s", mcpText(note))
	}
	notes, err := s.ListTaskNotes(ctx, "parent")
	if err != nil || len(notes) != 1 || notes[0].Body != "Schema migrated." {
		t.Fatalf("parent notes = %#v, %v", notes, err)
	}
}

// fakePeers stands in for the agent service, which needs tmux to reach agents.
type fakePeers struct {
	peers      []service.AgentPeer
	sent       []string
	broadcasts []string
	result     service.AgentBroadcastResult
}

func (f *fakePeers) ListPeers(_ context.Context, taskID, executionID, directory string) ([]service.AgentPeer, error) {
	return f.peers, nil
}

func (f *fakePeers) SendPeerMessage(_ context.Context, senderTaskID, recipientID, message string) error {
	f.sent = append(f.sent, senderTaskID+" -> "+recipientID+": "+message)
	return nil
}

func (f *fakePeers) BroadcastToPeers(_ context.Context, taskID, executionID, directory, message string) (service.AgentBroadcastResult, error) {
	f.broadcasts = append(f.broadcasts, taskID+"/"+executionID+": "+message)
	return f.result, nil
}

func TestMCPPeerToolsReachOtherAgentsAsTheCallingSession(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	peers := &fakePeers{
		peers:  []service.AgentPeer{{TaskID: "task2", ExecutionID: "ex-2", Title: "Refactor sync", Kind: "codex", Activity: "working", WorkingDir: "/work/legato"}},
		result: service.AgentBroadcastResult{Delivered: []string{"task2"}, Failed: []service.AgentDeliveryFailure{{TaskID: "task3", Error: "session is gone"}}},
	}
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Agents: peers})

	listed := callMCP(t, session, "list_peers", nil)
	if listed.IsError || !strings.Contains(mcpText(listed), "task2") || !strings.Contains(mcpText(listed), "Refactor sync") {
		t.Fatalf("list_peers = %q (error %v)", mcpText(listed), listed.IsError)
	}

	sent := callMCP(t, session, "message_peer", map[string]any{"to": "task2", "text": "I am editing agent.go"})
	if sent.IsError || len(peers.sent) != 1 || peers.sent[0] != "task1 -> task2: I am editing agent.go" {
		t.Fatalf("message_peer = %q, sent %v", mcpText(sent), peers.sent)
	}

	broadcast := callMCP(t, session, "broadcast_peers", map[string]any{"text": "Rebasing now"})
	if broadcast.IsError || len(peers.broadcasts) != 1 || peers.broadcasts[0] != "task1/"+executionID+": Rebasing now" {
		t.Fatalf("broadcast_peers = %q, broadcasts %v", mcpText(broadcast), peers.broadcasts)
	}
	if text := mcpText(broadcast); !strings.Contains(text, "task2") || !strings.Contains(text, "task3") || !strings.Contains(text, "session is gone") {
		t.Fatalf("broadcast result does not report each peer: %q", text)
	}

	blank := callMCP(t, session, "message_peer", map[string]any{"to": "task2", "text": "  "})
	if !blank.IsError || len(peers.sent) != 1 {
		t.Fatalf("a blank message was sent: %q, sent %v", mcpText(blank), peers.sent)
	}
}

func TestMCPPeerToolsAreNotOfferedWithoutAWayToReachAgents(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	for _, peerTool := range []string{"list_peers", "message_peer", "broadcast_peers"} {
		if names[peerTool] {
			t.Fatalf("%s is offered with nothing behind it", peerTool)
		}
	}
	for _, tool := range []string{"show_artifact", "show_card", "show_components", "session_info", "task_context", "add_task_note"} {
		if !names[tool] {
			t.Fatalf("%s is missing from %v", tool, names)
		}
	}
}

// Agent tools that ask before each MCP call are told which of Legato's tools
// to run without asking. That list must be every tool the server can offer.
func TestMCPToolNamesListsEveryToolTheServerOffers(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Agents: &fakePeers{}, Credentials: &fakeCredentials{}})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	offered := map[string]bool{}
	for _, tool := range listed.Tools {
		offered[tool.Name] = true
	}

	names := cli.MCPToolNames()

	if len(names) != len(offered) || len(names) == 0 {
		t.Fatalf("names = %v, offered = %v", names, offered)
	}
	for _, name := range names {
		if !offered[name] {
			t.Fatalf("%s is named but not offered (offered %v)", name, offered)
		}
	}
}

// An agent should reach for these tools by itself. What tells it so must
// reach its model whichever agent tool it runs in: the server's instructions
// where the client reads them, and each tool's own description everywhere.
func TestMCPGuidanceTellsAnAgentToUseTheToolsWithoutBeingAsked(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Agents: &fakePeers{}})

	instructions := session.InitializeResult().Instructions
	if instructions != cli.MCPGuidance {
		t.Fatalf("the server's instructions are not the shared guidance:\n%s", instructions)
	}
	for _, want := range []string{"without being asked", "show_artifact", "show_card", "show_components", "task_context", "add_task_note", "list_peers"} {
		if !strings.Contains(cli.MCPGuidance, want) {
			t.Errorf("guidance does not mention %q", want)
		}
	}
	// The guidance names tools by their own names: each agent tool adds a
	// prefix of its own, which the guidance cannot know.
	for _, prefixed := range []string{"mcp__", "mcp_legato", "legato."} {
		if strings.Contains(cli.MCPGuidance, prefixed) {
			t.Errorf("guidance uses an agent-specific tool name (%q)", prefixed)
		}
	}

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if strings.HasPrefix(tool.Name, "show_") && !strings.Contains(tool.Description, "without being asked") {
			t.Errorf("%s does not tell the agent it may be used without being asked: %q", tool.Name, tool.Description)
		}
	}
}

// What a display tool returns is read by the model, and by a person watching
// a terminal. It must leave room for one sentence of reply, so somebody who
// cannot see the card still learns what happened.
func TestMCPShowResultAsksForAShortReplyInsteadOfSilence(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID})

	result := callMCP(t, session, "show_card", map[string]any{"html": "<b>27 C</b>", "fallback_text": "27 C"})

	text := mcpText(result)
	if !strings.Contains(text, "one sentence") || !strings.Contains(text, "do not repeat") {
		t.Fatalf("result = %q", text)
	}
}

// mcpDescriptions returns each tool's description as a server with the given
// content hosts offers it.
func mcpDescriptions(t *testing.T, resourceDomains []string) map[string]string {
	t.Helper()
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, ResourceDomains: resourceDomains})
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]string{}
	for _, tool := range listed.Tools {
		descriptions[tool.Name] = tool.Description
	}
	return descriptions
}

// An agent that knows only what the frame forbids writes boxes with arrows
// typed as text. The guidance has to say both things: the ready-made
// components are the simple way, and custom HTML is there to be used fully
// when it gives the person something better.
func TestMCPGuidancePrefersComponentsWhenSimplerAndEncouragesCustomHTML(t *testing.T) {
	for _, want := range []string{"simplest", "CSS", "SVG", "JavaScript", "Mermaid", "better experience"} {
		if !strings.Contains(cli.MCPGuidance, want) {
			t.Errorf("the guidance does not mention %q", want)
		}
	}

	descriptions := mcpDescriptions(t, []string{"cdn.jsdelivr.net"})

	// Each card tool points to the other for what it does better.
	if d := descriptions["show_components"]; !strings.Contains(d, "simplest") || !strings.Contains(d, "show_card") {
		t.Errorf("show_components does not say it is the simple way, with show_card beyond it: %q", d)
	}
	if d := descriptions["show_card"]; !strings.Contains(d, "show_components") || !strings.Contains(d, "SVG") {
		t.Errorf("show_card does not say when to use it over show_components: %q", d)
	}
	// An artifact is where a diagram belongs, and Markdown can hold one too.
	artifact := descriptions["show_artifact"]
	for _, want := range []string{"diagram", "SVG", "Mermaid", "```mermaid", "tabs"} {
		if !strings.Contains(artifact, want) {
			t.Errorf("show_artifact does not mention %q: %q", want, artifact)
		}
	}
}

// What the frame allows was measured in a browser: scripts, SVG and
// libraries from the allowed hosts work; storage and workers do not. An
// agent that is not told writes pages that fail in ways it cannot see.
func TestMCPHTMLToolsSayWhatTheFrameAllows(t *testing.T) {
	withHosts := mcpDescriptions(t, []string{"cdn.jsdelivr.net", "unpkg.com"})
	for _, tool := range []string{"show_artifact", "show_card"} {
		d := withHosts[tool]
		for _, want := range []string{"<script>", "SVG", "cdn.jsdelivr.net", "unpkg.com", "pinned version", "localStorage", "workers", "fetch"} {
			if !strings.Contains(d, want) {
				t.Errorf("%s does not mention %q: %q", tool, want, d)
			}
		}
	}

	withoutHosts := mcpDescriptions(t, nil)
	for _, tool := range []string{"show_artifact", "show_card"} {
		d := withoutHosts[tool]
		if !strings.Contains(d, "inline") {
			t.Errorf("%s does not say everything must be inline when no host is allowed: %q", tool, d)
		}
		// With no host allowed, a library cannot be loaded. Markdown's own
		// diagrams still work, so Mermaid itself may be named.
		if strings.Contains(d, "may load from") || strings.Contains(d, "D3") {
			t.Errorf("%s offers libraries that cannot be loaded: %q", tool, d)
		}
	}
}

// An agent told only that Mermaid exists wrote a Markdown document with a
// diagram in it: correct, and dull. The guidance asks for a page that is
// designed and can be explored, and keeps Markdown for plain prose.
func TestMCPGuidanceAsksForADesignedPageNotADocument(t *testing.T) {
	for _, want := range []string{"design", "Mermaid", "click", "plain prose"} {
		if !strings.Contains(cli.MCPGuidance, want) {
			t.Errorf("the guidance does not mention %q", want)
		}
	}
	artifact := mcpDescriptions(t, []string{"cdn.jsdelivr.net"})["show_artifact"]
	for _, want := range []string{"not a document", "click", "narrow", "plain prose"} {
		if !strings.Contains(artifact, want) {
			t.Errorf("show_artifact does not mention %q: %q", want, artifact)
		}
	}
}

// Legato draws Mermaid for the page: it loads the library, applies the app's
// theme, draws inside hidden tabs and reports a click by the node's own
// name. Each of those went wrong when an agent did it by hand, so the HTML
// tools teach Legato's way and give an example that was run in the frame.
func TestMCPHTMLToolsTeachDiagramsThroughLegato(t *testing.T) {
	cases := map[string]struct {
		domains   []string
		available bool
	}{
		"jsdelivr allowed":      {[]string{"cdnjs.cloudflare.com", "cdn.jsdelivr.net", "unpkg.com"}, true},
		"only unpkg allowed":    {[]string{"unpkg.com"}, true},
		"no library host known": {[]string{"assets.example.com"}, false},
		"no host at all":        {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for tool, description := range mcpDescriptions(t, tc.domains) {
				if tool != "show_artifact" && tool != "show_card" {
					continue
				}
				// Never the library itself: a page that loads and runs its own
				// Mermaid gets none of what Legato does for it.
				for _, unwanted := range []string{"mermaid.min.js", "mermaid.run()", "mermaid.initialize"} {
					if strings.Contains(description, unwanted) {
						t.Errorf("%s teaches %q: %q", tool, unwanted, description)
					}
				}
				if !tc.available {
					if strings.Contains(description, "legato.diagrams") || strings.Contains(description, `class="mermaid"`) {
						t.Errorf("%s offers diagrams the frame cannot draw: %q", tool, description)
					}
					continue
				}
				for _, want := range []string{`<pre class="mermaid">`, "legato.diagrams({", "onNode", "legato.selectNode(", "hidden"} {
					if !strings.Contains(description, want) {
						t.Errorf("%s does not mention %q: %q", tool, want, description)
					}
				}
				// The colors a node can be given by name are the ones the frame styles.
				for _, hue := range []string{"blue", "cyan", "green", "amber", "red", "pink", "purple", "gray"} {
					if !strings.Contains(description, ":::"+hue) {
						t.Errorf("%s does not offer the color %q: %q", tool, hue, description)
					}
				}
			}
		})
	}
}
