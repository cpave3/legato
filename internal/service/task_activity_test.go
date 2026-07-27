package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestTaskActivityReturnsRelatedReviewsPlansAndWorkflowArtifacts(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureReviewTour(ctx, "task-1", "implementation"); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertPlanRevision(ctx, store.Plan{ID: "pl-1", TaskID: "task-1", Name: "implementation", Title: "Plan", Status: "approved", LatestRevision: 1}, store.PlanRevision{ID: "pr-1", PlanID: "pl-1", Revision: 1, Markdown: "# Plan"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTask(ctx, store.Task{ID: "workflow-task", Title: "Workflow", Status: "Done", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	run := store.WorkflowRun{ID: "wr-1", WorkflowName: "triage", WorkflowTitle: "Triage", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: "{}", InputsJSON: "{}", Status: "succeeded", TaskID: "workflow-task", RelatedTaskID: "task-1"}
	if err := db.CreateWorkflowRun(ctx, run, []store.WorkflowStepRun{{RunID: "wr-1", StepID: "triage", Title: "Triage", ExecutorType: "agent.run", DefinitionJSON: "{}", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: "wa-1", RunID: "wr-1", StepID: "triage", AttemptNumber: 1, ExecutorType: "agent.run", TaskID: "workflow-task", Status: "running"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertWorkflowArtifact(ctx, store.WorkflowArtifact{ID: "wfa-1", RunID: "wr-1", StepID: "triage", AttemptID: "wa-1", TaskID: "workflow-task", OutputKey: "report", ManagedPath: "/tmp/report.md", OriginalPath: "report.md", MediaType: "text/markdown", Digest: "sha"}); err != nil {
		t.Fatal(err)
	}
	activity, err := NewBoardService(db, nil).(TaskActivityService).TaskActivity(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Reviews) != 1 || activity.Reviews[0].ID != "rt-task-1-implementation" {
		t.Fatalf("reviews=%#v", activity.Reviews)
	}
	if len(activity.Plans) != 1 || activity.Plans[0].ID != "pl-1" {
		t.Fatalf("plans=%#v", activity.Plans)
	}
	if len(activity.WorkflowRuns) != 1 || len(activity.WorkflowRuns[0].Artifacts) != 1 || activity.WorkflowRuns[0].Artifacts[0].ID != "wfa-1" {
		t.Fatalf("workflows=%#v", activity.WorkflowRuns)
	}
}
