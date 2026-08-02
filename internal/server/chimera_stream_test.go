package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

type chimeraStreamAgents struct {
	session store.AgentSession
}

func (a *chimeraStreamAgents) HasChimeraEndpoint(context.Context, string) bool { return true }
func (a *chimeraStreamAgents) GetAgentSession(context.Context, string) (*store.AgentSession, error) {
	return &a.session, nil
}

func TestChimeraStreamReplaysMissedEventsFromCursor(t *testing.T) {
	manager, emit := newTestChimeraStreamManager(t)
	first := newRecordingWSClient()
	manager.subscribe(first.wsClient, "task-1", "")
	emit(chimera.EventEnvelope{EventID: "evt-1", Type: "user_message"})
	manager.unsubscribe(first.wsClient, "task-1")
	emit(chimera.EventEnvelope{EventID: "evt-2", Type: "assistant_text_done"})
	emit(chimera.EventEnvelope{EventID: "evt-3", Type: "run_finished"})

	reconnected := newRecordingWSClient()
	manager.subscribe(reconnected.wsClient, "task-1", "evt-1")

	messages := reconnected.messages(2)
	if len(messages) != 2 {
		t.Fatalf("replayed messages = %+v", messages)
	}
	if got := eventID(t, messages[0]); got != "evt-2" {
		t.Fatalf("first replay event = %q", got)
	}
	if got := eventID(t, messages[1]); got != "evt-3" {
		t.Fatalf("second replay event = %q", got)
	}
}

func TestChimeraStreamRemovesDisconnectedClientAfterGrace(t *testing.T) {
	manager, _ := newTestChimeraStreamManager(t)
	manager.gracePeriod = time.Millisecond
	client := newRecordingWSClient()
	manager.subscribe(client.wsClient, "task-1", "")

	manager.unsubscribeAll(client.wsClient)

	deadline := time.Now().Add(time.Second)
	for {
		manager.mu.Lock()
		_, exists := manager.streams["task-1"]
		manager.mu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle stream was not removed after grace period")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChimeraStreamReplaysRetainedEventsAfterSyncForCursorlessSubscriber(t *testing.T) {
	manager, emit := newTestChimeraStreamManager(t)
	first := newRecordingWSClient()
	manager.subscribe(first.wsClient, "task-1", "")
	emit(chimera.EventEnvelope{EventID: "spawn-call", Type: "tool_call_start"})
	emit(chimera.EventEnvelope{EventID: "spawned", Type: "subagent_spawned"})

	loadedMidRun := newRecordingWSClient()
	manager.subscribe(loadedMidRun.wsClient, "task-1", "")

	messages := loadedMidRun.messages(3)
	if len(messages) != 3 || messages[0].Type != MsgChimeraSyncRequired {
		t.Fatalf("messages = %+v", messages)
	}
	if got := eventID(t, messages[1]); got != "spawn-call" {
		t.Fatalf("first retained event = %q", got)
	}
	if got := eventID(t, messages[2]); got != "spawned" {
		t.Fatalf("second retained event = %q", got)
	}
}

func TestChimeraStreamRequestsSyncForUnknownCursor(t *testing.T) {
	manager, _ := newTestChimeraStreamManager(t)
	client := newRecordingWSClient()

	manager.subscribe(client.wsClient, "task-1", "expired-event")

	messages := client.messages(1)
	if len(messages) != 1 || messages[0].Type != MsgChimeraSyncRequired {
		t.Fatalf("messages = %+v", messages)
	}
}

func newTestChimeraStreamManager(t *testing.T) (*chimeraStreamManager, func(chimera.EventEnvelope)) {
	t.Helper()
	endpoint, sessionID := "http://chimera", "session-1"
	manager := newChimeraStreamManager()
	manager.agents = &chimeraStreamAgents{session: store.AgentSession{ChimeraEndpoint: &endpoint, ChimeraSessionID: &sessionID}}
	manager.gracePeriod = time.Hour
	var mu sync.Mutex
	var handler func(chimera.EventEnvelope)
	manager.subscribeEvents = func(ctx context.Context, _, _, _ string, onEvent func(chimera.EventEnvelope)) error {
		mu.Lock()
		handler = onEvent
		mu.Unlock()
		<-ctx.Done()
		return ctx.Err()
	}
	emit := func(event chimera.EventEnvelope) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			mu.Lock()
			current := handler
			mu.Unlock()
			if current != nil {
				current(event)
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("upstream subscription did not start")
			}
			time.Sleep(time.Millisecond)
		}
	}
	return manager, emit
}

type recordingWSClient struct {
	*wsClient
	mu       sync.Mutex
	received []WSMessage
}

func newRecordingWSClient() *recordingWSClient {
	recording := &recordingWSClient{}
	recording.wsClient = &wsClient{ctx: context.Background(), sendFn: func(message WSMessage) {
		recording.mu.Lock()
		recording.received = append(recording.received, message)
		recording.mu.Unlock()
	}}
	return recording
}

func (c *recordingWSClient) messages(want int) []WSMessage {
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.Lock()
		messages := append([]WSMessage(nil), c.received...)
		c.mu.Unlock()
		if len(messages) >= want || time.Now().After(deadline) {
			return messages
		}
		time.Sleep(time.Millisecond)
	}
}

func eventID(t *testing.T, message WSMessage) string {
	t.Helper()
	var event chimera.EventEnvelope
	if err := json.Unmarshal([]byte(message.Content), &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return event.EventID
}
