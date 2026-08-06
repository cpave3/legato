package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestWorkflowAttentionSnapshotReportsManualStep(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Simulate a run with a waiting_manual step by inserting minimal records.
	// The adapter reads ListRuns (which sets NeedsAttention) and Show (which
	// returns step status). We use a fake source to avoid full workflow setup.
	fake := &fakeWorkflowAttentionSource{
		runs: []store.WorkflowRun{
			{ID: "wr-1", WorkflowTitle: "Deploy", TaskID: "task-1", Status: "waiting",
				CurrentStepID: "approve", NeedsAttention: true, AttentionReason: "manual action required: Approve"},
		},
		view: &store.WorkflowRunView{
			Run: store.WorkflowRun{ID: "wr-1", Status: "waiting", CurrentStepID: "approve"},
			Steps: []store.WorkflowStepRun{
				{RunID: "wr-1", StepID: "approve", Status: "waiting_manual", Title: "Approve"},
			},
		},
	}

	adapter := NewWorkflowAttentionAdapter(fake)
	items, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SignalKind != AttentionSignalWorkflowManualGate || items[0].SourceKey != "wr-1:approve" || items[0].Reason != "Manual action required: Approve" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Actions) != 2 || items[0].Actions[1].ID != "trigger" {
		t.Fatalf("actions = %+v", items[0].Actions)
	}
}

func TestWorkflowAttentionSnapshotClassifiesFailureAsHighUrgency(t *testing.T) {
	fake := &fakeWorkflowAttentionSource{
		runs: []store.WorkflowRun{{ID: "wr-1", WorkflowTitle: "Deploy", Status: "failed"}},
		view: &store.WorkflowRunView{
			Run:   store.WorkflowRun{ID: "wr-1", Status: "failed"},
			Steps: []store.WorkflowStepRun{{RunID: "wr-1", StepID: "deploy", Status: "failed", Title: "Deploy"}},
		},
	}
	items, err := NewWorkflowAttentionAdapter(fake).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SignalKind != AttentionSignalWorkflowFailed || items[0].Urgency != "high" || items[0].Actions[1].ID != "retry" {
		t.Fatalf("failure item = %+v", items)
	}
}

func TestWorkflowAttentionSnapshotFailsRatherThanResolvingOnShowError(t *testing.T) {
	fake := &fakeWorkflowAttentionSource{
		runs:    []store.WorkflowRun{{ID: "wr-1", Status: "failed"}},
		showErr: errors.New("temporary read failure"),
	}
	if _, err := NewWorkflowAttentionAdapter(fake).Snapshot(context.Background()); err == nil {
		t.Fatal("snapshot succeeded after a run failed to load")
	}
}
