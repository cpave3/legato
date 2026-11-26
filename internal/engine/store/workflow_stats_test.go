package store

import (
	"context"
	"testing"
	"time"
)

func startRunAt(t *testing.T, s *Store, id, workflow, status, createdAt string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateWorkflowRun(ctx, WorkflowRun{ID: id, WorkflowName: workflow, WorkflowTitle: workflow, DefinitionSource: "test", DefinitionDigest: id, DefinitionJSON: `{}`, InputsJSON: `{}`, Status: status, CurrentStepID: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE workflow_runs SET created_at = ? WHERE id = ?`, createdAt, id); err != nil {
		t.Fatal(err)
	}
}

func TestLatestWorkflowRunsIsTheNewestRunOfEachWorkflow(t *testing.T) {
	s := newTestStore(t)
	startRunAt(t, s, "old", "triage", "failed", "2026-11-01 09:00:00")
	startRunAt(t, s, "new", "triage", "succeeded", "2026-11-25 09:00:00")
	startRunAt(t, s, "only", "rotate-tokens", "succeeded", "2026-10-01 09:00:00")
	runs, err := s.LatestWorkflowRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, run := range runs {
		got[run.WorkflowName] = run.ID
	}
	if len(runs) != 2 || got["triage"] != "new" || got["rotate-tokens"] != "only" {
		t.Fatalf("latest = %v", got)
	}
}

func TestWorkflowRunsSinceListsEachRunsWorkflowStatusAndStart(t *testing.T) {
	s := newTestStore(t)
	startRunAt(t, s, "before", "triage", "succeeded", "2026-11-18 23:59:59")
	startRunAt(t, s, "after", "triage", "failed", "2026-11-19 00:00:00")
	marks, err := s.WorkflowRunsSince(context.Background(), time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0] != (WorkflowRunMark{WorkflowName: "triage", Status: "failed", CreatedAt: "2026-11-19T00:00:00Z"}) {
		t.Fatalf("marks = %+v", marks)
	}
}
