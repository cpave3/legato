package service

import (
	"context"
	"testing"
)

// conversationRunEnd is why the one run of taskID's one conversation ended,
// "" while it is live.
func (f restartFixture) conversationRunEnd(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	conversations, err := f.store.ListTaskConversations(ctx, taskID)
	if err != nil || len(conversations) != 1 {
		t.Fatalf("conversations = %#v, %v; want one", conversations, err)
	}
	runs, err := f.store.ListConversationRuns(ctx, conversations[0].ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %#v, %v; want one", runs, err)
	}
	if runs[0].EndReason == nil {
		return ""
	}
	return *runs[0].EndReason
}

func (f restartFixture) reportedAgent(t *testing.T, taskID string) {
	t.Helper()
	createTask(t, f.store, taskID)
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(context.Background(), taskID, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportConversation(t, taskID, cwd)
}

func TestARunEndsWithWhyItsAgentEnded(t *testing.T) {
	ctx := context.Background()
	t.Run("stopped by the operator", func(t *testing.T) {
		f := newRestartFixture(t)
		f.reportedAgent(t, "task-s")
		if err := f.svc.KillAgent(ctx, "task-s"); err != nil {
			t.Fatal(err)
		}
		if got := f.conversationRunEnd(t, "task-s"); got != "stopped" {
			t.Fatalf("run end = %q, want stopped", got)
		}
	})
	t.Run("stopped with its task", func(t *testing.T) {
		f := newRestartFixture(t)
		f.reportedAgent(t, "task-d")
		if err := f.svc.(TaskAgentsStopper).StopTaskAgents(ctx, "task-d"); err != nil {
			t.Fatal(err)
		}
		if got := f.conversationRunEnd(t, "task-d"); got != "stopped" {
			t.Fatalf("run end = %q, want stopped", got)
		}
	})
	t.Run("exited on its own", func(t *testing.T) {
		f := newRestartFixture(t)
		f.reportedAgent(t, "task-e")
		f.crash() // the pane is gone, and Legato is still up
		if err := f.svc.ReconcileSessions(ctx); err != nil {
			t.Fatal(err)
		}
		if got := f.conversationRunEnd(t, "task-e"); got != "exited" {
			t.Fatalf("run end = %q, want exited", got)
		}
	})
	t.Run("interrupted, then dismissed", func(t *testing.T) {
		f := newRestartFixture(t)
		f.interruptedAgent(t, "task-i", t.TempDir(), true)
		if got := f.conversationRunEnd(t, "task-i"); got != "interrupted" {
			t.Fatalf("run end = %q, want interrupted", got)
		}
		if err := f.interrupted().DismissInterruptedAgent(ctx, "task-i"); err != nil {
			t.Fatal(err)
		}
		if got := f.conversationRunEnd(t, "task-i"); got != "dismissed" {
			t.Fatalf("run end = %q, want dismissed", got)
		}
	})
}
