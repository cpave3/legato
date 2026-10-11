package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// conversationEndpoint stands in for a native endpoint that serves one
// session's snapshot and, when given, how full its context is.
func conversationEndpoint(t *testing.T, sessionID, snapshot, sessionContext string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/"+sessionID, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, snapshot)
	})
	if sessionContext != "" {
		mux.HandleFunc("GET /v1/sessions/"+sessionID+"/context", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, sessionContext)
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

// conversationOf binds a running agent to an endpoint that serves snapshot,
// and returns the service to ask.
func conversationOf(t *testing.T, snapshot, sessionContext string) AgentConversationReader {
	t.Helper()
	svc, s, _ := newDiscoveryTestService(t)
	runningAgent(t, s, "TASK-1", "claude-code")
	endpoint := conversationEndpoint(t, "TASK-1", snapshot, sessionContext)
	if err := s.UpdateChimeraDiscovery(context.Background(), "TASK-1", "TASK-1", endpoint); err != nil {
		t.Fatal(err)
	}
	reader, ok := svc.(AgentConversationReader)
	if !ok {
		t.Fatal("the agent service should read native conversations")
	}
	return reader
}

const workedSnapshot = `{
	"id": "TASK-1",
	"status": "running",
	"messages": [
		{"role": "user", "content": "You are an agent."},
		{"role": "user", "content": "add a --json flag to yg list"},
		{"role": "assistant", "content": [{"type": "text", "text": "Let me look."}, {"type": "tool-call"}, {"type": "tool-call"}]},
		{"role": "assistant", "content": [{"type": "text", "text": "The flag is in. Running the tests."}, {"type": "tool-call"}, {"type": "tool-call"}]}
	],
	"toolCalls": [
		{"callId": "c1", "name": "read", "args": {"path": "/repo/src/yg/commands/list.ts"}, "result": {"content": "..."}},
		{"callId": "c2", "name": "edit", "args": {"path": "src/yg/commands/list.ts", "old_string": "a", "new_string": "b"}, "result": {"replacements": 1}},
		{"callId": "c3", "name": "bash", "args": {"command": "pnpm vitest run yg/list\necho done", "description": "Run the list tests"}, "result": {"stderr": "1 failed", "exit_code": 1}, "display": {"summary": "pnpm vitest run yg/list"}},
		{"callId": "c4", "name": "bash", "args": {"command": "pnpm build"}, "display": {"summary": "pnpm build"}}
	],
	"pendingPermissions": [],
	"pendingQuestions": []
}`

func TestConversationTailReadsWhatANativeAgentSaidAndDid(t *testing.T) {
	reader := conversationOf(t, workedSnapshot, `{"contextWindow": 200000, "lastPromptTokens": 138000}`)

	tail, err := reader.ConversationTail(context.Background(), "TASK-1", 20)

	if err != nil {
		t.Fatal(err)
	}
	// The first message is the session's own instructions, not the person's.
	want := []ConversationEntry{
		{Kind: EntryUser, Text: "add a --json flag to yg list"},
		{Kind: EntryAssistant, Text: "Let me look."},
		{Kind: EntryTool, Tool: "read", Text: "list.ts", State: ToolDone},
		{Kind: EntryTool, Tool: "edit", Text: "list.ts", State: ToolDone},
		{Kind: EntryAssistant, Text: "The flag is in. Running the tests."},
		{Kind: EntryTool, Tool: "bash", Text: "Run the list tests", Command: "pnpm vitest run yg/list", State: ToolFailed},
		{Kind: EntryTool, Tool: "bash", Text: "pnpm build", Command: "pnpm build", State: ToolRunning},
	}
	if len(tail.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(tail.Entries), len(want), tail.Entries)
	}
	for i := range want {
		if tail.Entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, tail.Entries[i], want[i])
		}
	}
	if tail.Status != "running" {
		t.Errorf("status = %q, want running", tail.Status)
	}
	if tail.ContextUsed != 138000 || tail.ContextWindow != 200000 {
		t.Errorf("context = %d of %d, want 138000 of 200000", tail.ContextUsed, tail.ContextWindow)
	}
}

func TestConversationTailKeepsOnlyTheNewestEntries(t *testing.T) {
	reader := conversationOf(t, workedSnapshot, "")

	tail, err := reader.ConversationTail(context.Background(), "TASK-1", 3)

	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 3 || tail.Entries[0].Text != "The flag is in. Running the tests." || tail.Entries[2].Text != "pnpm build" {
		t.Fatalf("entries = %+v, want the last three", tail.Entries)
	}
}

func TestConversationTailIsStillReadWhenTheEndpointDoesNotSayHowFullItsContextIs(t *testing.T) {
	reader := conversationOf(t, workedSnapshot, "")

	tail, err := reader.ConversationTail(context.Background(), "TASK-1", 20)

	if err != nil || len(tail.Entries) == 0 {
		t.Fatalf("tail = %+v, err = %v; want the conversation", tail, err)
	}
	if tail.ContextWindow != 0 {
		t.Errorf("context window = %d, want unknown", tail.ContextWindow)
	}
}

func TestConversationTailSaysWhatTheAgentIsWaitingToBeAllowed(t *testing.T) {
	reader := conversationOf(t, `{
		"status": "waiting_for_permission",
		"messages": [{"role": "user", "content": "system"}, {"role": "user", "content": "clean up"}],
		"toolCalls": [],
		"pendingPermissions": [{"requestId": "r1", "tool": "bash", "command": "rm -rf dist && pnpm build", "reason": "outside the sandbox", "cwd": "/repo"}]
	}`, "")

	tail, err := reader.ConversationTail(context.Background(), "TASK-1", 20)

	if err != nil {
		t.Fatal(err)
	}
	want := ConversationPermission{Tool: "bash", Command: "rm -rf dist && pnpm build", Reason: "outside the sandbox"}
	if tail.Permission == nil || *tail.Permission != want {
		t.Fatalf("permission = %+v, want %+v", tail.Permission, want)
	}
}

func TestConversationTailSaysWhatTheAgentAsks(t *testing.T) {
	reader := conversationOf(t, `{
		"status": "waiting_for_input",
		"messages": [],
		"toolCalls": [],
		"pendingQuestions": [{"requestId": "q1", "questions": [
			{"id": "a", "question": "Which color?", "options": [{"label": "Red"}, {"label": "Blue"}]},
			{"id": "b", "question": "Which size?", "options": []}
		]}]
	}`, "")

	tail, err := reader.ConversationTail(context.Background(), "TASK-1", 20)

	if err != nil {
		t.Fatal(err)
	}
	if tail.Question == nil || tail.Question.Text != "Which color?" || strings.Join(tail.Question.Options, ",") != "Red,Blue" {
		t.Fatalf("question = %+v, want the first one with its options", tail.Question)
	}
}

func TestAnAgentDrivenThroughItsTerminalHasNoConversationToRead(t *testing.T) {
	svc, s, _ := newDiscoveryTestService(t)
	runningAgent(t, s, "SHELL-1", "shell")

	_, err := svc.(AgentConversationReader).ConversationTail(context.Background(), "SHELL-1", 20)

	if !errors.Is(err, ErrNoNativeEndpoint) {
		t.Fatalf("err = %v, want ErrNoNativeEndpoint", err)
	}
}

func TestAnEndpointThatIsGoneIsAnError(t *testing.T) {
	svc, s, _ := newDiscoveryTestService(t)
	runningAgent(t, s, "TASK-1", "claude-code")
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	if err := s.UpdateChimeraDiscovery(context.Background(), "TASK-1", "TASK-1", gone.URL); err != nil {
		t.Fatal(err)
	}

	_, err := svc.(AgentConversationReader).ConversationTail(context.Background(), "TASK-1", 20)

	if err == nil || errors.Is(err, ErrNoNativeEndpoint) {
		t.Fatalf("err = %v, want an error from the endpoint", err)
	}
}

func TestALongMessageIsCutBeforeItReachesTheCaller(t *testing.T) {
	long := strings.Repeat("word ", 2000)
	reader := conversationOf(t, `{"messages": [{"role": "user", "content": "system"}, {"role": "assistant", "content": [{"type": "text", "text": "`+long+`"}]}], "toolCalls": []}`, "")

	tail, err := reader.ConversationTail(context.Background(), "TASK-1", 20)

	if err != nil || len(tail.Entries) != 1 {
		t.Fatalf("tail = %+v, err = %v", tail, err)
	}
	if got := len([]rune(tail.Entries[0].Text)); got > conversationTextLimit {
		t.Errorf("the message is %d characters, want at most %d", got, conversationTextLimit)
	}
}
