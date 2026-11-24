package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

// workflowMessages connects a web client to srv and gives what it receives.
func workflowMessages(srv *Server) <-chan WSMessage {
	messages := make(chan WSMessage, 32)
	srv.hub.add(&wsClient{ctx: context.Background(), sendFn: func(message WSMessage) {
		if message.Type == MsgWorkflowChanged {
			messages <- message
		}
	}})
	return messages
}

func nextWorkflowMessage(t *testing.T, messages <-chan WSMessage) WSMessage {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("no workflow_changed reached the web client")
		return WSMessage{}
	}
}

func noMoreWorkflowMessages(t *testing.T, messages <-chan WSMessage) {
	t.Helper()
	select {
	case message := <-messages:
		t.Fatalf("one more workflow_changed: %+v", message)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestEveryWorkflowChangeReachesAnOpenRunPage(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, ":0", nil, bus, "")
	messages := workflowMessages(srv)
	srv.StartWorkflowEvents()
	// advanced and "" are the advance and agent-ended loops' own; the rest
	// are the service's writes and a review loop's.
	for _, kind := range []string{"advanced", "", "step_completed", "run_started", ReviewLoopChanged} {
		bus.Publish(events.Event{Type: events.EventWorkflowChanged, Payload: events.WorkflowChangedPayload{RunID: "wr-1", StepID: "implement", Kind: kind}})
		got := nextWorkflowMessage(t, messages)
		if got.WorkflowRunID != "wr-1" || got.WorkflowStepID != "implement" || got.Kind != kind {
			t.Fatalf("kind %q: got %+v", kind, got)
		}
	}
}

func TestATriggerChangeReachesTheWorkflowsTab(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, ":0", nil, bus, "")
	messages := workflowMessages(srv)
	srv.StartWorkflowEvents()
	// A trigger request moved on, or a schedule fired: its state lives in
	// the definitions' trigger_states.
	bus.Publish(events.Event{Type: events.EventWorkflowTriggerChanged, Payload: "req-1"})
	if got := nextWorkflowMessage(t, messages); got.Kind != WorkflowTriggerChanged || got.WorkflowRunID != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestNotifyingAWorkflowChangeSendsOneMessage(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, ":0", nil, bus, "")
	messages := workflowMessages(srv)
	onBus := bus.Subscribe(events.EventWorkflowChanged)
	srv.StartWorkflowEvents()
	srv.NotifyWorkflowChangedKind("wr-1", "implement", "completed_step")
	if got := nextWorkflowMessage(t, messages); got.WorkflowRunID != "wr-1" || got.Kind != "completed_step" {
		t.Fatalf("got %+v", got)
	}
	noMoreWorkflowMessages(t, messages)
	// The advance loop still hears it, once.
	select {
	case event := <-onBus:
		if payload := event.Payload.(events.WorkflowChangedPayload); payload.Kind != "completed_step" {
			t.Fatalf("bus payload %+v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("not published on the bus")
	}
	select {
	case event := <-onBus:
		t.Fatalf("published twice: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNotifyingWithoutTheForwarderStillTellsTheWebClient(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	messages := workflowMessages(srv)
	srv.NotifyWorkflowChanged("wr-1", "")
	if got := nextWorkflowMessage(t, messages); got.WorkflowRunID != "wr-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestCreatingAWorkflowFileRefreshesTheDefinitions(t *testing.T) {
	f := newFilesFixture(t)
	messages := workflowMessages(f.s)
	if code, body := f.call(t, http.MethodPost, "/api/workflow-files", map[string]string{"yaml": reviewFile, "folder": "project"}); code != http.StatusCreated {
		t.Fatalf("%d %v", code, body)
	}
	if got := nextWorkflowMessage(t, messages); got.Kind != WorkflowDefinitionsChanged {
		t.Fatalf("got %+v", got)
	}
}
