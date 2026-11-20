package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// postFor posts an operator item as the running agent of taskID.
func postFor(t *testing.T, s *store.Store, taskID, title string) store.OperatorItem {
	t.Helper()
	session, err := s.GetRunningAgentSession(context.Background(), taskID, "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := PostOperatorItem(context.Background(), s, OperatorItemInput{TaskID: taskID, ExecutionID: session.ExecutionID, Kind: OperatorItemAction, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func itemState(t *testing.T, s *store.Store, id string) store.OperatorItem {
	t.Helper()
	item, err := s.GetOperatorItem(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestAnEndedAgentsOperatorItemsClose(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-e")
	if err := f.svc.SpawnAgent(ctx, "task-e", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	item := postFor(t, f.store, "task-e", "Merge the PR")

	if err := f.svc.KillAgent(ctx, "task-e"); err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, f.store, item.ID); got.Status != store.OperatorItemClosed || got.CloseReason != store.OperatorItemAgentEnded {
		t.Fatalf("after the agent ended, item = %#v", got)
	}
}

func TestAnInterruptedAgentKeepsItsItemsUntilResumedOrDismissed(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-r")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportConversation(t, "task-r", cwd)
	item := postFor(t, f.store, "task-r", "Run /to-tickets")
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, f.store, item.ID); got.Status != store.OperatorItemOpen {
		t.Fatalf("an interrupted agent's item = %#v, want it open", got)
	}

	if err := f.interrupted().ResumeInterruptedAgent(ctx, "task-r", false); err != nil {
		t.Fatal(err)
	}
	resumed, err := f.store.GetRunningAgentSession(ctx, "task-r", "")
	if err != nil {
		t.Fatal(err)
	}
	got := itemState(t, f.store, item.ID)
	if got.Status != store.OperatorItemOpen || got.ExecutionID != resumed.ExecutionID || got.ExecutionID == item.ExecutionID {
		t.Fatalf("after resuming, item = %#v; want it open on execution %s", got, resumed.ExecutionID)
	}

	// Crash again, and dismiss this time.
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.interrupted().DismissInterruptedAgent(ctx, "task-r"); err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, f.store, item.ID); got.Status != store.OperatorItemClosed || got.CloseReason != store.OperatorItemAgentEnded {
		t.Fatalf("after dismissing, item = %#v", got)
	}
}

// An agent may end while no Legato runs to close its items: the startup
// sweep closes them, and leaves an interrupted agent's alone.
func TestTheStartupSweepClosesTheItemsOfAgentsThatEndedWhileLegatoWasDown(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	for _, task := range []string{"task-gone", "task-crashed"} {
		createTask(t, f.store, task)
		if err := f.svc.SpawnAgent(ctx, task, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}
	gone := postFor(t, f.store, "task-gone", "Look at the build")
	crashed := postFor(t, f.store, "task-crashed", "Answer my question")
	// Another process ended task-gone's execution, and left its item.
	if err := f.store.TerminalizeExecution(ctx, gone.ExecutionID, store.ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateAgentSessionStatusInSlot(ctx, "task-gone", store.SlotMain, "dead"); err != nil {
		t.Fatal(err)
	}
	f.crash()

	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, f.store, gone.ID); got.Status != store.OperatorItemClosed || got.CloseReason != store.OperatorItemAgentEnded {
		t.Fatalf("an ended agent's item = %#v", got)
	}
	if got := itemState(t, f.store, crashed.ID); got.Status != store.OperatorItemOpen {
		t.Fatalf("an interrupted agent's item = %#v", got)
	}
}

func TestTheOperatorsOwnMessageClosesTheAgentsItemsButLegatosDoNot(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-m")
	if err := f.svc.SpawnAgent(ctx, "task-m", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	item := postFor(t, f.store, "task-m", "Pick a database")
	recorder := f.svc.(OperatorTurnRecorder)

	for _, tagged := range []string{"[legato peer abc] I changed the schema", "[legato review pass] Pass 2", "  [legato interrupted] go on"} {
		if err := recorder.RecordOperatorMessage(ctx, "task-m", tagged); err != nil {
			t.Fatal(err)
		}
		if got := itemState(t, f.store, item.ID); got.Status != store.OperatorItemOpen {
			t.Fatalf("Legato's %q closed the item: %#v", tagged, got)
		}
	}
	// A macro is the operator's own words, untagged.
	if err := recorder.RecordOperatorMessage(ctx, "task-m", "/to-tickets"); err != nil {
		t.Fatal(err)
	}
	if got := itemState(t, f.store, item.ID); got.Status != store.OperatorItemClosed || got.CloseReason != store.OperatorItemOperatorTurn {
		t.Fatalf("after the operator's message, item = %#v", got)
	}
}

func TestOperatorTextIsWhatLegatoDidNotTag(t *testing.T) {
	for text, want := range map[string]bool{
		"/to-tickets":                     true,
		"Use Postgres":                    true,
		"[legato peer t1] hi":             false,
		"[legato agent] An agent":         false,
		"\n[legato operator] Done: Merge": false,
		"[legatoish] not a tag":           true,
		"":                                false,
		"   ":                             false,
	} {
		if got := IsOperatorText(text); got != want {
			t.Errorf("IsOperatorText(%q) = %v, want %v", text, got, want)
		}
	}
}
