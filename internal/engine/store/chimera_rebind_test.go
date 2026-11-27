package store

import (
	"context"
	"testing"
)

// An automatic binding refuses every row it does not own outright, and a
// named slot's agent never counts against the task's main agent.
func TestAutomaticChimeraBindingRefusesRowsItDoesNotOwn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task")
	main, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "task", Status: "running", TmuxSession: "legato-task", StartedAt: "2026-01-01 00:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	// A reviewer started later in its own slot.
	reviewer, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "task", Slot: "reviewer", Status: "running", TmuxSession: "legato-task_reviewer", StartedAt: "2026-01-02 00:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	state := func(id int, sessionID, endpoint string) string {
		t.Helper()
		got, err := s.ChimeraBindingState(ctx, id, sessionID, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	const ep = "http://127.0.0.1:1/x"

	if got := state(main, "s", ep); got != NativeBindingUnbound {
		t.Fatalf("main with a newer reviewer = %s", got)
	}
	if bound, err := s.BindChimeraDiscoveryIfUnbound(ctx, main, "s", ep); err != nil || !bound {
		t.Fatalf("bind main = %v, %v", bound, err)
	}
	if got := state(main, "s", ep); got != NativeBindingBound {
		t.Fatalf("bound main = %s", got)
	}
	if got := state(main, "other", ep); got != NativeBindingElsewhere {
		t.Fatalf("main asked for another session = %s", got)
	}
	if bound, _ := s.BindChimeraDiscoveryIfUnbound(ctx, main, "other", ep); bound {
		t.Fatal("replaced a binding")
	}

	if ok, err := s.KeepAgentSessionOnTmux(ctx, main, "s", ep); err != nil || !ok {
		t.Fatalf("downgrade = %v, %v", ok, err)
	}
	if got := state(main, "s", ep); got != NativeBindingOff {
		t.Fatalf("switched main = %s", got)
	}
	if bound, _ := s.BindChimeraDiscoveryIfUnbound(ctx, main, "s", ep); bound {
		t.Fatal("an automatic binding undid the switch to the terminal")
	}
	if bound, _ := s.BindNativeEndpointUnlessOff(ctx, main, "s", ep); bound {
		t.Fatal("a bridge binding undid the switch to the terminal")
	}
	if bound, err := s.UpgradeChimeraDiscoveryIfUnbound(ctx, main, "s", ep); err != nil || !bound {
		t.Fatalf("operator upgrade = %v, %v", bound, err)
	}

	if err := s.FinishAgentSession(ctx, reviewer); err != nil {
		t.Fatal(err)
	}
	if got := state(reviewer, "s", ep); got != NativeBindingStale {
		t.Fatalf("ended reviewer = %s", got)
	}
	if got := state(9999, "s", ep); got != NativeBindingStale {
		t.Fatalf("missing row = %s", got)
	}
}
