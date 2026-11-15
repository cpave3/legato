package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

func mqttAssertStates(t *testing.T, transport *mqttTestTransport, want map[string]string) {
	t.Helper()
	mqttAwait(t, func() bool {
		for key, value := range want {
			if transport.get("legato/test/"+key+"/state") != value {
				return false
			}
		}
		return true
	})
}

func TestMQTTPublisherRealSourcesMapStartupStateAndPublicMutations(t *testing.T) {
	approvals, db, bus := newApprovalFixture(t)
	ctx := context.Background()
	tmux := newMockTmux()
	agents := NewAgentService(db, tmux, t.TempDir(), AgentServiceOptions{EventBus: AgentDiedPublisher{Bus: bus}, DisablePeerAwareness: true})
	attention := NewAttentionService(db, bus)
	if err := attention.RegisterProducerWithEvents(NewApprovalAttentionAdapter(approvals), events.EventApprovalChanged); err != nil {
		t.Fatal(err)
	}
	stopAttention := attention.StartEventReconciliation(ctx)
	t.Cleanup(stopAttention)

	// Persisted sessions are read at startup, including slots and automation.
	// Spawn the interactive agent through the same service used by callers.
	createTask(t, db, "main")
	if err := agents.SpawnAgent(ctx, "main", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := agents.SetAgentActivity(ctx, "main", "working", ""); err != nil {
		t.Fatal(err)
	}
	for _, seed := range []struct{ id, slot, status, activity, role string }{
		{"main", "reviewer", "running", "waiting", ""},
		{"idle", "main", "running", "", ""},
		{"workflow", "main", "running", "background", ""},
		{"swarm", "main", "running", "error", "backend"},
		{"stopped", "main", "stopped", "waiting", ""},
		{"dead", "main", "dead", "working", ""},
		{"interrupted", "main", "interrupted", "error", ""},
	} {
		if seed.id != "main" {
			createTask(t, db, seed.id)
		}
		id, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: seed.id, Slot: seed.slot, TmuxSession: AgentTmuxSession(seed.id, seed.slot), Status: seed.status, Role: seed.role})
		if err != nil {
			t.Fatal(err)
		}
		if seed.id == "workflow" || seed.id == "swarm" {
			kind := store.ExecutionWorkflowAgent
			if seed.id == "swarm" {
				kind = store.ExecutionSwarmWorker
			}
			if err := db.CreateExecution(ctx, store.Execution{ID: "ex-mqtt-" + seed.id, Kind: kind, Status: store.ExecutionRunning, TaskID: seed.id, ExecutionTaskID: seed.id, AgentSessionID: &id, Role: seed.role, StartedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
		}
		if seed.status == "running" {
			if err := db.UpdateAgentActivityForSession(ctx, id, seed.activity); err != nil {
				t.Fatal(err)
			}
		}
	}
	reeve, err := db.CreateReeveTask(ctx, "Private Reeve title")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: reeve, TmuxSession: AgentTmuxSession(reeve, "main"), Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentActivity(ctx, reeve, "waiting"); err != nil {
		t.Fatal(err)
	}

	// Completion history is deliberately excluded by Attention's default policy.
	if _, err := attention.Upsert(ctx, AttentionInput{Producer: "mqtt-test", SourceKey: "history", SourceRevision: "1", SignalKind: AttentionSignalWorkflowCompletion, Title: "Completed", Reason: "History", Category: "workflow", Urgency: "normal", Navigation: AttentionNavigation{Kind: "task", Target: "workflow"}}); err != nil {
		t.Fatal(err)
	}
	transport := mqttNewTestTransport()
	p := NewMQTTPublisher(mqttTestOptions(), agents, attention, approvals, bus, func(context.Context) (MQTTConnection, error) { return transport, nil })
	mqttRunTestPublisher(t, p)
	mqttAwait(t, func() bool { return transport.get("legato/test/availability") == "online" })
	mqttAssertStates(t, transport, map[string]string{"agents_running": "5", "agents_working": "1", "agents_waiting": "1", "agents_idle": "1", "agents_background": "1", "agents_error": "1", "pending_approvals": "0", "attention_items": "0", "needs_attention": "OFF", "agent_waiting_on_you": "ON"})

	if err := agents.SetAgentActivity(ctx, "main", "waiting", ""); err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"agents_working": "0", "agents_waiting": "2"})

	// Submission and decision must update MQTT via ApprovalService's event path;
	// Attention independently projects the approval using its startup wiring.
	approvals.RegisterResolver("test", &recordingResolver{})
	request := commandRequest("mqtt-real")
	request.TaskID = reeve
	approval, _, err := approvals.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"pending_approvals": "1", "attention_items": "1", "needs_attention": "ON"})
	items, err := attention.List(ctx, AttentionFilter{})
	if err != nil || len(items) != 1 {
		t.Fatalf("attention=%v err=%v", items, err)
	}
	if err := attention.Dismiss(ctx, items[0].ID, items[0].SourceRevision); err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"pending_approvals": "1", "attention_items": "0", "needs_attention": "OFF"})
	if _, err := approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", By: "web"}); err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"pending_approvals": "0", "attention_items": "0"})

	// Attention's own public mutation path also invalidates the publisher.
	item, err := attention.Upsert(ctx, AttentionInput{Producer: "mqtt-test", SourceKey: "plan", SourceRevision: "1", SignalKind: AttentionSignalPlanDecision, Title: "Do not export /private/plan", Reason: "Decision", Category: "plan", Urgency: "normal", Navigation: AttentionNavigation{Kind: "task", Target: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"attention_items": "1", "needs_attention": "ON"})
	if err := attention.Dismiss(ctx, item.ID, item.SourceRevision); err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"attention_items": "0", "needs_attention": "OFF"})

	if err := agents.KillAgent(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"agents_running": "4", "agents_waiting": "1"})
	if err := agents.(SlotAgents).KillAgentInSlot(ctx, "main", "reviewer"); err != nil {
		t.Fatal(err)
	}
	mqttAssertStates(t, transport, map[string]string{"agents_running": "3", "agents_waiting": "0", "agent_waiting_on_you": "OFF"})
}
