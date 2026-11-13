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

// The Claude Code mod reports a prompt its session would not take as a live
// message_failed event; Legato logs it as it passes it on.
func TestNativeMessageFailedIsLogged(t *testing.T) {
	logged := captureLog(t)
	manager, emit := newTestChimeraStreamManager(t)
	client := newRecordingWSClient()
	manager.subscribe(client.wsClient, "task-1", "")
	data, _ := json.Marshal(map[string]string{
		"eventId": "failed-1",
		"type":    "message_failed",
		"content": "/tmp/a.png what is this?",
		"error":   "a text beginning with / would run a command as the user",
	})

	emit(chimera.EventEnvelope{EventID: "failed-1", Type: "message_failed", Data: data})
	emit(chimera.EventEnvelope{EventID: "said-1", Type: "user_message"})

	got := logged.String()
	if !strings.Contains(got, "task-1") || !strings.Contains(got, "would run a command") {
		t.Fatalf("log = %q, want the task and the session's reason", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("log = %q, want one line, for the failure only", got)
	}
}
