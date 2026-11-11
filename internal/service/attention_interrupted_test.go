package service

import (
	"context"
	"strings"
	"testing"
)

func TestInterruptedAgentsAreOneAttentionItemThatResumesThemAll(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	adapter := NewInterruptedAttentionAdapter(f.interrupted())
	if items, err := adapter.Snapshot(ctx); err != nil || len(items) != 0 {
		t.Fatalf("before a crash: %+v, %v", items, err)
	}
	f.interruptedAgent(t, "task-a", t.TempDir(), true)
	createTask(t, f.store, "task-b")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-b", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportConversation(t, "task-b", cwd)
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}

	items, err := adapter.Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("after a crash: %+v, %v", items, err)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalAgentsInterrupted || !strings.Contains(item.Title, "2 agents") || item.SourceRevision == "" {
		t.Fatalf("item = %+v", item)
	}
	if item.Navigation.Kind != "agents" || item.Navigation.Target != "interrupted" {
		t.Fatalf("navigation = %+v", item.Navigation)
	}
	var resumeAll bool
	for _, action := range item.Actions {
		resumeAll = resumeAll || (action.ID == "resume_all" && action.Kind == "execute")
	}
	if !resumeAll {
		t.Fatalf("actions = %+v, want resume_all", item.Actions)
	}

	if err := adapter.Execute(ctx, AttentionActionRequest{ActionID: "resume_all", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if items, _ := adapter.Snapshot(ctx); len(items) != 0 {
		t.Fatalf("after resume all: %+v, want none", items)
	}
}

func TestInterruptedAgentsAreInTheDefaultAttentionPolicy(t *testing.T) {
	for _, s := range defaultAttentionSignals {
		if s == AttentionSignalAgentsInterrupted {
			return
		}
	}
	t.Fatal("agents_interrupted is not a default signal")
}
