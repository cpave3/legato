package main

import (
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
)

type fakeNotifier struct{ called bool }

func (f *fakeNotifier) NotifyAgentsChanged() { f.called = true }

func drainEvent(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	panic("unreachable")
}

func TestIPCMessage_PlanExtensionProposed(t *testing.T) {
	bus := events.New()
	ch := bus.Subscribe(events.EventPlanProposed)
	fn := &fakeNotifier{}

	msg := ipc.Message{
		Type:        "plan_extension_proposed",
		TaskID:      "parent-ext",
		PlanPath:    "/tmp/plan.yaml",
		ReplySocket: "/tmp/reply.sock",
		Mode:        "extension",
	}
	handleIPCMessage(msg, bus, fn)

	ev := drainEvent(t, ch)
	payload, ok := ev.Payload.(events.PlanProposedPayload)
	if !ok {
		t.Fatalf("expected PlanProposedPayload, got %T", ev.Payload)
	}
	if payload.ParentTaskID != "parent-ext" {
		t.Errorf("ParentTaskID = %q, want parent-ext", payload.ParentTaskID)
	}
	if payload.PlanPath != "/tmp/plan.yaml" {
		t.Errorf("PlanPath = %q, want /tmp/plan.yaml", payload.PlanPath)
	}
	if payload.ReplySocket != "/tmp/reply.sock" {
		t.Errorf("ReplySocket = %q, want /tmp/reply.sock", payload.ReplySocket)
	}
	if payload.Mode != "extension" {
		t.Errorf("Mode = %q, want extension", payload.Mode)
	}
	if fn.called {
		t.Error("NotifyAgentsChanged should not be called for plan_proposed events")
	}
}

func TestIPCMessage_PlanProposed(t *testing.T) {
	bus := events.New()
	ch := bus.Subscribe(events.EventPlanProposed)

	msg := ipc.Message{
		Type:        "plan_proposed",
		TaskID:      "parent-1",
		PlanPath:    "/tmp/plan.yaml",
		ReplySocket: "/tmp/reply.sock",
	}
	handleIPCMessage(msg, bus, nil)

	ev := drainEvent(t, ch)
	payload, ok := ev.Payload.(events.PlanProposedPayload)
	if !ok {
		t.Fatalf("expected PlanProposedPayload, got %T", ev.Payload)
	}
	if payload.ParentTaskID != "parent-1" {
		t.Errorf("ParentTaskID = %q, want parent-1", payload.ParentTaskID)
	}
	if payload.ReplySocket != "/tmp/reply.sock" {
		t.Errorf("ReplySocket = %q, want /tmp/reply.sock", payload.ReplySocket)
	}
	if payload.Mode != "" {
		t.Errorf("Mode = %q, want empty", payload.Mode)
	}
}

func TestIPCMessage_PlanChangedPreservesPlanIdentity(t *testing.T) {
	bus := events.New()
	ch := bus.Subscribe(events.EventPlanChanged)
	handleIPCMessage(ipc.Message{Type: "plan_changed", PlanID: "pl-task-search", TaskID: "task-1", RevisionID: "pr-1", Kind: "submitted"}, bus, nil)
	event := drainEvent(t, ch)
	payload, ok := event.Payload.(events.PlanChangedPayload)
	if !ok || payload.PlanID != "pl-task-search" || payload.TaskID != "task-1" || payload.RevisionID != "pr-1" || payload.Kind != "submitted" {
		t.Fatalf("payload = %#v", event.Payload)
	}
}

func TestIPCMessage_ReviewChangedPreservesTourIdentity(t *testing.T) {
	bus := events.New()
	ch := bus.Subscribe(events.EventReviewChanged)

	msg := ipc.Message{
		Type:   "review_changed",
		TourID: "rt-task-1-security",
		StepID: "rs-123",
		Kind:   "answer",
	}
	handleIPCMessage(msg, bus, nil)

	ev := drainEvent(t, ch)
	payload, ok := ev.Payload.(events.ReviewChangedPayload)
	if !ok {
		t.Fatalf("expected ReviewChangedPayload, got %T", ev.Payload)
	}
	if payload.TourID != "rt-task-1-security" || payload.StepID != "rs-123" || payload.Kind != "answer" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestIPCMessage_CardEvents(t *testing.T) {
	for _, messageType := range []string{"task_update", "worktree_changed"} {
		t.Run(messageType, func(t *testing.T) {
			bus := events.New()
			ch := bus.Subscribe(events.EventCardUpdated)
			fn := &fakeNotifier{}

			msg := ipc.Message{Type: messageType, TaskID: "abc123"}
			handleIPCMessage(msg, bus, fn)

			ev := drainEvent(t, ch)
			if ev.Type != events.EventCardUpdated {
				t.Errorf("Type = %v, want EventCardUpdated", ev.Type)
			}
			if !fn.called {
				t.Error("NotifyAgentsChanged should be called")
			}
		})
	}
}

func TestIPCMessage_NilNotifierDoesNotPanic(t *testing.T) {
	bus := events.New()
	ch := bus.Subscribe(events.EventCardUpdated)

	msg := ipc.Message{Type: "task_update", TaskID: "safe"}
	handleIPCMessage(msg, bus, nil)

	ev := drainEvent(t, ch)
	if ev.Type != events.EventCardUpdated {
		t.Errorf("Type = %v, want EventCardUpdated", ev.Type)
	}
}
