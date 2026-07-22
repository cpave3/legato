package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestPlanCompleteCommandCleansApprovedBundle(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "cleanup-plan")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "plan.json"), []byte(`{"schema_version":1,"title":"Plan"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := service.NewPlanService(s, nil, nil)
	view, err := svc.Submit(ctx, "task-1", "cleanup", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, view.Plan.ID, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	output := captureStdout(t, func() int {
		return runPlanCmd([]string{"complete", "--task", "task-1", "--name", "cleanup"})
	})
	var result service.PlanCompletionResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || !result.CleanedUp {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatalf("bundle still exists: %v", err)
	}
	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	plan, err := s.GetPlan(ctx, view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "completed" || plan.CompletedAt == nil {
		t.Fatalf("plan = %+v", plan)
	}
}
