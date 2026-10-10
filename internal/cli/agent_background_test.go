package cli_test

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

// A turn that ended with a shell still running: the agent is neither working
// nor finished, and nobody needs telling.
func TestAgentState_BackgroundWorkIsShownWithoutClaimingTimeOrAttention(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, "abc123")
	if err := s.UpdateTaskNotifyEnabled(ctx, "abc123", true); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	report := func(activity string) {
		t.Helper()
		if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: executionID, Activity: activity, Event: "Stop"}, notifier, notifier); err != nil {
			t.Fatalf("AgentState %q: %v", activity, err)
		}
	}

	report(store.ActivityWorking)
	report(store.ActivityBackground)

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != store.ActivityBackground {
		t.Fatalf("activity = %q, want background", session.Activity)
	}
	// Reports count the agent's own turns; waiting on a shell is not one.
	var open int
	if err := s.DB().Get(&open, `SELECT COUNT(*) FROM execution_activity WHERE execution_id=? AND ended_at IS NULL`, executionID); err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatalf("open activity spans = %d, want the working span closed and none opened", open)
	}
	// The agent has not finished and does not need the person.
	if len(notifier.calls) != 0 {
		t.Fatalf("notifications = %+v, want none", notifier.calls)
	}

	// The shell returns, the agent works, then really stops: that is news.
	report(store.ActivityWorking)
	report("")
	if len(notifier.calls) == 0 {
		t.Fatal("finishing after the background work said nothing")
	}
}
