package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/chimera"
)

// lockedBuffer is a log destination tests can read while the server writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog sends the standard logger to a buffer for the test's life.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	out := &lockedBuffer{}
	previous, flags := log.Writer(), log.Flags()
	log.SetOutput(out)
	t.Cleanup(func() {
		log.SetOutput(previous)
		log.SetFlags(flags)
	})
	return out
}

// LEG-151: a message the agent never got must be in Legato's log, not only
// in the page that sent it.
func TestAgentMessageThatFailsIsLogged(t *testing.T) {
	logged := captureLog(t)
	agents := &promptAgentService{queueErr: errors.New("the bridge is gone")}
	s := New(nil, agents, nil, "")
	client := &wsClient{ctx: context.Background(), sendFn: func(WSMessage) {}}

	s.handleWSMessage(client, WSMessage{Type: MsgAgentMessage, AgentID: "task-1", Content: "Follow this up", Queue: true, RequestID: "m-1"})

	if got := logged.String(); !strings.Contains(got, "task-1") || !strings.Contains(got, "the bridge is gone") {
		t.Fatalf("log = %q, want the task and the reason", got)
	}
}

// fakeAgentEvents stands for the approval watcher, which follows every
// running native agent whether or not a page does.
type fakeAgentEvents struct {
	listeners []func(taskID string, event chimera.EventEnvelope)
}

func (f *fakeAgentEvents) AddListener(fn func(taskID string, event chimera.EventEnvelope)) {
	f.listeners = append(f.listeners, fn)
}

func (f *fakeAgentEvents) emit(taskID string, event chimera.EventEnvelope) {
	for _, listener := range f.listeners {
		listener(taskID, event)
	}
}

func messageFailed(t *testing.T, content, reason string) chimera.EventEnvelope {
	t.Helper()
	data, err := json.Marshal(map[string]string{"eventId": "failed-1", "type": "message_failed", "content": content, "error": reason})
	if err != nil {
		t.Fatal(err)
	}
	return chimera.EventEnvelope{EventID: "failed-1", Type: "message_failed", Data: data}
}

// The Claude Code mod reports a prompt its session would not take as a live
// message_failed event. Legato logs it from the agents' own event streams, so
// it is logged with no page open on the agent.
func TestNativeMessageFailedIsLoggedWithNoPageOpen(t *testing.T) {
	logged := captureLog(t)
	s := New(nil, nil, nil, "")
	events := &fakeAgentEvents{}
	s.UseAgentEvents(events)

	events.emit("task-1", messageFailed(t, "/tmp/a.png what is this?", "a text beginning with / would run a command as the user"))
	events.emit("task-1", chimera.EventEnvelope{EventID: "said-1", Type: "user_message"})

	got := logged.String()
	if !strings.Contains(got, "task-1") || !strings.Contains(got, "would run a command") || !strings.Contains(got, "/tmp/a.png") {
		t.Fatalf("log = %q, want the task, the session's reason and the start of the message", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("log = %q, want one line, for the failure only", got)
	}
}

// What the agent's side says is quoted and cut, so it can neither write lines
// of its own into the log nor fill it; the message is cut at a character.
func TestNativeMessageFailedLogLineIsQuotedAndCut(t *testing.T) {
	logged := captureLog(t)
	s := New(nil, nil, nil, "")
	events := &fakeAgentEvents{}
	s.UseAgentEvents(events)

	words := strings.Repeat("é", 300)
	reason := "refused\n2026/11/13 12:00:00 forged line" + strings.Repeat("x", 2000)
	events.emit("task-1", messageFailed(t, words, reason))

	got := logged.String()
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("log = %q, want one line", got)
	}
	if len(got) > 1000 {
		t.Fatalf("log line is %d bytes, want it cut", len(got))
	}
	if strings.Contains(got, "\\x") {
		t.Fatalf("log = %q, want the message cut between characters", got)
	}
	if !strings.Contains(got, "éé…") {
		t.Fatalf("log = %q, want the cut message to end in an ellipsis", got)
	}
}

// Without the approval watcher the context watcher holds the server's own
// stream of each native agent, and it logs a refused message too.
func TestNativeMessageFailedIsLoggedFromTheContextWatchersOwnStream(t *testing.T) {
	logged := captureLog(t)
	h := newContextHarness(t)
	h.watcher.Reconcile([]string{"task-a"})
	h.waitFor(func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.handlers["http://task-a"] != nil
	})
	h.mu.Lock()
	handler := h.handlers["http://task-a"]
	h.mu.Unlock()

	handler(messageFailed(t, "/tmp/a.png what is this?", "a text beginning with / would run a command as the user"))

	if got := logged.String(); !strings.Contains(got, "task-a") || !strings.Contains(got, "would run a command") {
		t.Fatalf("log = %q, want the task and the session's reason", got)
	}
}
