package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// contextHarness drives a contextWatcher with a pretend endpoint: what its
// /context answers, and the events its stream delivers.
type contextHarness struct {
	t       *testing.T
	watcher *contextWatcher

	mu        sync.Mutex
	sent      []WSMessage
	handlers  map[string]func(chimera.EventEnvelope)
	stops     map[string]int
	readings  map[string]*chimera.SessionContext
	subscribe map[string]error
}

func newContextHarness(t *testing.T) *contextHarness {
	t.Helper()
	h := &contextHarness{t: t, handlers: map[string]func(chimera.EventEnvelope){}, stops: map[string]int{}, readings: map[string]*chimera.SessionContext{}, subscribe: map[string]error{}}
	h.watcher = newContextWatcher(func(message WSMessage) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.sent = append(h.sent, message)
	})
	h.watcher.sessions = func(_ context.Context, taskID string) (*store.AgentSession, error) {
		endpoint, sessionID := "http://"+taskID, "session-"+taskID
		return &store.AgentSession{TaskID: taskID, ChimeraEndpoint: &endpoint, ChimeraSessionID: &sessionID}, nil
	}
	h.watcher.read = func(_ context.Context, endpoint, _ string) (*chimera.SessionContext, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		reading, ok := h.readings[endpoint]
		if !ok {
			return nil, errors.New("no reading")
		}
		return reading, nil
	}
	h.watcher.subscribeEvents = func(ctx context.Context, endpoint, _, _ string, onEvent func(chimera.EventEnvelope)) error {
		h.mu.Lock()
		if err := h.subscribe[endpoint]; err != nil {
			h.mu.Unlock()
			return err
		}
		h.handlers[endpoint] = onEvent
		h.mu.Unlock()
		<-ctx.Done()
		h.mu.Lock()
		h.stops[endpoint]++
		h.mu.Unlock()
		return ctx.Err()
	}
	h.watcher.retryDelay = time.Millisecond
	t.Cleanup(h.watcher.Stop)
	return h
}

func (h *contextHarness) emitUsage(taskID string, used, window int) {
	h.t.Helper()
	data, _ := json.Marshal(map[string]any{"type": "usage_updated", "usedContextTokens": used, "contextWindow": window})
	h.waitFor(func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.handlers["http://"+taskID] != nil
	})
	h.mu.Lock()
	handler := h.handlers["http://"+taskID]
	h.mu.Unlock()
	handler(chimera.EventEnvelope{Type: "usage_updated", Data: data})
}

func (h *contextHarness) contextMessages() []WSMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []WSMessage
	for _, message := range h.sent {
		if message.Type == MsgAgentContext {
			out = append(out, message)
		}
	}
	return out
}

func (h *contextHarness) waitFor(ok func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			h.t.Fatal("condition not met in time")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestContextWatcherSendsTheFirstReadingOfAWatchedAgent(t *testing.T) {
	h := newContextHarness(t)
	used := 90_400
	h.readings["http://task-a"] = &chimera.SessionContext{ContextWindow: 1_000_000, LastPromptTokens: &used}

	h.watcher.Reconcile([]string{"task-a"})

	h.waitFor(func() bool { return len(h.contextMessages()) == 1 })
	got := h.contextMessages()[0]
	if got.AgentID != "task-a" || got.ContextUsed != 90_400 || got.ContextWindow != 1_000_000 {
		t.Fatalf("message = %+v", got)
	}
	if reading, ok := h.watcher.Context("task-a"); !ok || reading.Used != 90_400 {
		t.Fatalf("Context(task-a) = %+v, %v", reading, ok)
	}
}

func TestContextWatcherPushesAChangeAndNothingWhenTheFillStaysTheSame(t *testing.T) {
	h := newContextHarness(t)
	h.watcher.Reconcile([]string{"task-a"})

	h.emitUsage("task-a", 1000, 200_000)
	h.emitUsage("task-a", 1000, 200_000)
	h.emitUsage("task-a", 5000, 200_000)

	h.waitFor(func() bool { return len(h.contextMessages()) == 2 })
	if got := h.contextMessages(); got[0].ContextUsed != 1000 || got[1].ContextUsed != 5000 {
		t.Fatalf("messages = %+v", got)
	}
}

func TestContextWatcherIgnoresOtherEvents(t *testing.T) {
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
	handler(chimera.EventEnvelope{Type: "assistant_text_delta", Data: json.RawMessage(`{"type":"assistant_text_delta","delta":"hi"}`)})

	if got := h.contextMessages(); len(got) != 0 {
		t.Fatalf("messages = %+v", got)
	}
}

func TestContextWatcherStopsWatchingAnAgentNoLongerListed(t *testing.T) {
	h := newContextHarness(t)
	h.watcher.Reconcile([]string{"task-a", "task-b"})
	h.emitUsage("task-a", 10, 100)

	h.watcher.Reconcile([]string{"task-b"})

	h.waitFor(func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.stops["http://task-a"] == 1
	})
	if _, ok := h.watcher.Context("task-a"); ok {
		t.Fatal("an agent no longer watched still has a reading")
	}
}

func TestContextWatcherGivesUpOnAnEndpointThatKeepsFailing(t *testing.T) {
	h := newContextHarness(t)
	h.subscribe["http://task-a"] = errors.New("connection refused")

	h.watcher.Reconcile([]string{"task-a"})

	h.waitFor(func() bool { return !h.watcher.watching("task-a") })
}

func TestAgentsEndpointCarriesTheLastContextReading(t *testing.T) {
	agents := &promptAgentService{agent: service.AgentSession{TaskID: "task-a", AgentKind: "claude-code", Status: "running"}, chimeraEndpoint: "http://bridge", chimeraSessionID: "session"}
	srv := New(&mockBoardService{}, agents, nil, ":0")
	t.Cleanup(srv.agentContexts.Stop)
	srv.agentContexts.read = func(context.Context, string, string) (*chimera.SessionContext, error) {
		used := 4200
		return &chimera.SessionContext{ContextWindow: 200_000, LastPromptTokens: &used}, nil
	}
	srv.agentContexts.subscribeEvents = func(ctx context.Context, _, _, _ string, _ func(chimera.EventEnvelope)) error {
		<-ctx.Done()
		return ctx.Err()
	}
	srv.agentContexts.sessions = func(_ context.Context, taskID string) (*store.AgentSession, error) {
		endpoint, sessionID := "http://bridge", "session"
		return &store.AgentSession{TaskID: taskID, ChimeraEndpoint: &endpoint, ChimeraSessionID: &sessionID}, nil
	}

	get := func() AgentResponse {
		req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		var resp []AgentResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp) != 1 {
			t.Fatalf("body = %s, err = %v", w.Body.String(), err)
		}
		return resp[0]
	}
	get()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := get(); got.ContextUsed == 4200 && got.ContextWindow == 200_000 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent = %+v", get())
		}
		time.Sleep(time.Millisecond)
	}
}
