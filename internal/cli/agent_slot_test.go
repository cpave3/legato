package cli_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

// seedReviewer starts a second agent on the task, in its reviewer slot, and
// returns its execution.
func seedReviewer(t *testing.T, s *store.Store, taskID string) string {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, Slot: "reviewer", TmuxSession: "legato-" + taskID + "_reviewer", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	execution := "exec-" + taskID + "-reviewer"
	if err := s.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: "Test", AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return execution
}

func TestAgentState_FromAReviewerLandsOnItsOwnSlot(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	linkTestExecution(t, s, "abc123")
	reviewer := seedReviewer(t, s, "abc123")

	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: reviewer, Activity: "working"}, nil, nil); err != nil {
		t.Fatalf("the reviewer's state hook: %v", err)
	}

	main, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	review, _ := s.GetAgentSessionInSlot(ctx, "abc123", "reviewer")
	if main.Activity != "" || review.Activity != "working" {
		t.Fatalf("activity: main %q, reviewer %q; want only the reviewer working", main.Activity, review.Activity)
	}
}

func TestChimeraSessionCreatedByAReviewerIsKeptInItsSlot(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	reviewer := seedReviewer(t, s, "abc123")
	payload := `{"event":"SessionCreated","session_id":"chimera-r","session_name":"abc123_reviewer","cwd":"/repo"}`

	registered, err := cli.AgentSessionCreatedFromHookFor(s, "abc123", reviewer, "chimera-r", strings.NewReader(payload))
	if err != nil || !registered {
		t.Fatalf("registered = %v, %v", registered, err)
	}

	if main, _ := s.TaskChimeraSessionID(context.Background(), "abc123"); main != "" {
		t.Fatalf("the reviewer's session became the task's: %q", main)
	}
	saved, err := s.GetTaskNativeSessionInSlot(context.Background(), "abc123", "reviewer", "chimera")
	if err != nil || saved == nil || saved.ProviderSessionID != "chimera-r" {
		t.Fatalf("reviewer's Chimera session = %+v, %v", saved, err)
	}
}
