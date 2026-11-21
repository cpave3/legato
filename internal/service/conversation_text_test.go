package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// capturedAgent is a running Claude Code agent of TASK-1 whose conversation
// conv-1 is recorded and whose endpoint serves snapshot, counting reads.
func capturedAgent(t *testing.T, snapshot string) (*ConversationTextCapture, *store.Store, *atomic.Int32) {
	t.Helper()
	ctx := context.Background()
	svc, s, _ := newDiscoveryTestService(t)
	createTask(t, s, "TASK-1")
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "TASK-1", TmuxSession: "legato-TASK-1", AgentKind: "claude-code", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "TASK-1", ExecutionTaskID: "TASK-1", AgentKind: "claude-code", AgentSessionID: &id, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, "TASK-1", id, "ex-1", "claude-code", "conv-1", "/repo", ""); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/TASK-1", func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		fmt.Fprint(w, snapshot)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	if err := s.UpdateChimeraDiscovery(ctx, "TASK-1", "TASK-1", server.URL); err != nil {
		t.Fatal(err)
	}
	capture := NewConversationTextCapture(s, svc.(AgentConversationReader), ConversationTextOptions{Delay: 20 * time.Millisecond})
	return capture, s, &reads
}

func conversationText(t *testing.T, s *store.Store) store.AgentConversation {
	t.Helper()
	got, err := s.ListTaskConversations(context.Background(), "TASK-1")
	if err != nil || len(got) != 1 {
		t.Fatalf("conversations = %#v, %v", got, err)
	}
	return got[0]
}

const turnSnapshot = `{
	"id": "TASK-1", "status": "idle",
	"messages": [
		{"role": "system", "content": ""},
		{"role": "user", "content": "[legato brief] Read your brief."},
		{"role": "user", "content": "add a --json flag to yg list"},
		{"role": "assistant", "content": [{"type": "text", "text": "Let me look."}, {"type": "tool-call"}]},
		{"role": "assistant", "content": [{"type": "text", "text": "Done: \u001b[1mthe flag\u001b[0m is in.\u0007"}]}
	],
	"toolCalls": [{"callId": "c1", "name": "read", "args": {"path": "/repo/list.ts"}, "result": {}}]
}`

func TestATurnEndKeepsTheFirstPromptAndTheLastMessage(t *testing.T) {
	capture, s, _ := capturedAgent(t, turnSnapshot)
	if err := capture.Capture(context.Background(), "TASK-1"); err != nil {
		t.Fatal(err)
	}
	c := conversationText(t, s)
	if c.FirstMessage != "add a --json flag to yg list" {
		t.Fatalf("first message = %q, want the operator's, not the tagged brief", c.FirstMessage)
	}
	if c.LastMessage != "Done: the flag is in." || c.LastMessageAt == nil {
		t.Fatalf("last message = %q at %v, want it clean", c.LastMessage, c.LastMessageAt)
	}
	for _, query := range []string{`"json"`, `"flag"`} {
		if got, err := s.MatchAgentConversations(context.Background(), query, 5); err != nil || len(got) != 1 {
			t.Fatalf("search %s = %#v, %v", query, got, err)
		}
	}
}

func TestTheFirstPromptIsSetOnce(t *testing.T) {
	capture, s, _ := capturedAgent(t, turnSnapshot)
	ctx := context.Background()
	if err := capture.Capture(ctx, "TASK-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordConversationText(ctx, "ex-1", "something said later", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if c := conversationText(t, s); c.FirstMessage != "add a --json flag to yg list" || c.LastMessage != "Done: the flag is in." {
		t.Fatalf("after a later capture = %q / %q", c.FirstMessage, c.LastMessage)
	}
}

func TestOnlyATaggedPromptIsKeptUntilTheOperatorSpeaks(t *testing.T) {
	ctx := context.Background()
	capture, s, _ := capturedAgent(t, `{"id": "TASK-1", "status": "idle", "messages": [
		{"role": "system", "content": ""},
		{"role": "user", "content": "[legato brief] Read your brief."},
		{"role": "assistant", "content": "Reading it."}], "toolCalls": []}`)
	if err := capture.Capture(ctx, "TASK-1"); err != nil {
		t.Fatal(err)
	}
	if c := conversationText(t, s); c.FirstMessage != "[legato brief] Read your brief." {
		t.Fatalf("first message = %q, want the tagged one for want of another", c.FirstMessage)
	}
	if err := s.RecordConversationText(ctx, "ex-1", "now fix the tests", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if c := conversationText(t, s); c.FirstMessage != "now fix the tests" {
		t.Fatalf("first message = %q, want the operator's once there is one", c.FirstMessage)
	}
}

func TestABurstOfTurnEndsMakesOneRead(t *testing.T) {
	capture, s, reads := capturedAgent(t, turnSnapshot)
	for range 5 {
		capture.TurnEnded("TASK-1")
	}
	deadline := time.Now().Add(2 * time.Second)
	for conversationText(t, s).LastMessage == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	if got := reads.Load(); got != 1 {
		t.Fatalf("endpoint read %d times, want once", got)
	}
}

func TestAMessageIsClippedAndCleaned(t *testing.T) {
	text := "\u001b[31m" + strings.Repeat("é", 3000) + "\x00"
	got := cleanConversationText(text)
	if strings.ContainsAny(got, "\u001b\x00") || len([]rune(got)) != conversationTextLimit {
		t.Fatalf("clean text = %d runes, escape kept %v", len([]rune(got)), strings.Contains(got, "\u001b"))
	}
}

func TestAnAgentWithNoEndpointRecordsNothing(t *testing.T) {
	svc, s, _ := newDiscoveryTestService(t)
	runningAgent(t, s, "TASK-1", "claude-code")
	capture := NewConversationTextCapture(s, svc.(AgentConversationReader), ConversationTextOptions{})
	if err := capture.Capture(context.Background(), "TASK-1"); err != nil {
		t.Fatalf("capture without an endpoint = %v, want nothing to do", err)
	}
}
