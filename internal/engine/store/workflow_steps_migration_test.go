package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A run in flight before the step tree (migration 135) carries on after it:
// its steps, attempts and artifacts are keyed by path = step ID, and its
// waiting agent step can still complete.
func TestAWorkflowRunInFlightCarriesOnAfterTheStepTreeMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legato.db")
	old := newStoreAtMigration(t, path, "134_agent_conversation_scans.sql")
	statements := []string{
		`INSERT INTO tasks (id, title, status, created_at, updated_at) VALUES ('wf-task', 'Run', 'Backlog', datetime('now'), datetime('now'))`,
		`INSERT INTO workflow_runs (id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, status, current_step_id, task_id)
			VALUES ('wr-old', 'old', 'Old', 'old.yaml', 'sha', '{"schema_version":1,"steps":[{"id":"work","trigger":"automatic"},{"id":"stop","trigger":"agent_ended"}]}', 'running', 'work', 'wf-task')`,
		`INSERT INTO workflow_step_runs (run_id, step_id, step_order, title, executor_type, definition_json, status, latest_attempt)
			VALUES ('wr-old', 'work', 0, 'Work', 'agent.run', '{"id":"work","trigger":"automatic"}', 'waiting_agent', 1)`,
		`INSERT INTO workflow_step_runs (run_id, step_id, step_order, title, executor_type, definition_json, status)
			VALUES ('wr-old', 'stop', 1, 'Stop', 'agent.stop', '{"id":"stop","trigger":"agent_ended"}', 'pending')`,
		`INSERT INTO workflow_step_attempts (id, run_id, step_id, attempt_number, executor_type, agent_session_id, task_id, status)
			VALUES ('wa-old', 'wr-old', 'work', 1, 'agent.run', 7, 'wf-task', 'waiting_agent')`,
		`INSERT INTO workflow_artifacts (id, run_id, step_id, attempt_id, output_key, managed_path, original_path, media_type, size_bytes, digest)
			VALUES ('art-old', 'wr-old', 'work', 'wa-old', 'notes', '/managed/notes.md', 'notes.md', 'text/markdown', 4, 'sha256:x')`,
	}
	for _, statement := range statements {
		if _, err := old.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(path)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer s.Close()
	view, err := s.GetWorkflowRun(ctx, "wr-old")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.SchemaVersion != 1 || view.Run.OutputsJSON != "{}" || view.Run.ParentRunID != "" {
		t.Fatalf("run = %+v", view.Run)
	}
	if len(view.Steps) != 2 || view.Steps[0].Path != "work" || view.Steps[0].StepID != "work" || view.Steps[0].ParentPath != "" || view.Steps[0].Pass != nil || view.Steps[1].Path != "stop" {
		t.Fatalf("steps = %+v", view.Steps)
	}
	if view.Steps[0].Status != "waiting_agent" || view.Steps[0].LatestAttempt != 1 {
		t.Fatalf("waiting step = %+v", view.Steps[0])
	}
	if len(view.Attempts) != 1 || view.Attempts[0].Path != "work" || view.Attempts[0].StepID != "work" || view.Attempts[0].AgentSessionID == nil || *view.Attempts[0].AgentSessionID != 7 {
		t.Fatalf("attempts = %+v", view.Attempts)
	}
	if len(view.Artifacts) != 1 || view.Artifacts[0].Path != "work" {
		t.Fatalf("artifacts = %+v", view.Artifacts)
	}
	waiting, err := s.ListWaitingWorkflowAttemptsByAgentSession(ctx, 7)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("waiting attempts by session = %+v, %v", waiting, err)
	}

	// The agent completes its step from its pane: recorded, and due for a
	// host to advance.
	if err := s.RecordWorkflowStepCompletion(ctx, "wr-old", "work", "wa-old", `{"done":true}`, "stop"); err != nil {
		t.Fatalf("complete the migrated step: %v", err)
	}
	claimed, err := s.ClaimWorkflowAdvances(ctx, "", "", time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].Step.Path != "work" || claimed[0].Attempt.ID != "wa-old" {
		t.Fatalf("claimed = %+v, %v", claimed, err)
	}
	// A second host does not take a fresh claim.
	if again, err := s.ClaimWorkflowAdvances(ctx, "", "", time.Minute); err != nil || len(again) != 0 {
		t.Fatalf("claimed twice = %+v, %v", again, err)
	}
	// A claim older than stale is taken again: its process crashed.
	if again, err := s.ClaimWorkflowAdvances(ctx, "", "", -time.Second); err != nil || len(again) != 1 {
		t.Fatalf("stale claim = %+v, %v", again, err)
	}
	if err := s.FinishWorkflowAdvance(ctx, "wr-old", "work"); err != nil {
		t.Fatal(err)
	}
	if again, err := s.ClaimWorkflowAdvances(ctx, "", "", -time.Second); err != nil || len(again) != 0 {
		t.Fatalf("finished advance claimed = %+v, %v", again, err)
	}
	view, err = s.GetWorkflowRun(ctx, "wr-old")
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.CurrentStepID != "stop" || view.Steps[0].Status != "succeeded" {
		t.Fatalf("after completion: run at %q, step %q", view.Run.CurrentStepID, view.Steps[0].Status)
	}
}

func TestNestedStepInstancesAreCreatedOnceAndListedByScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	run := WorkflowRun{ID: "wr-tree", WorkflowName: "tree", WorkflowTitle: "Tree", DefinitionSource: "t", DefinitionDigest: "d", DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "each", SchemaVersion: 2}
	if err := s.CreateWorkflowRun(ctx, run, []WorkflowStep{
		{RunID: "wr-tree", Path: "each", StepID: "each", StepOrder: 0, Title: "Each", Status: "running"},
		{RunID: "wr-tree", Path: "after", StepID: "after", StepOrder: 1, Title: "After", ExecutorType: "task.note", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	pass := 0
	nested := []WorkflowStep{{RunID: "wr-tree", Path: "each/0/build", ParentPath: "each/0", StepID: "build", Pass: &pass, StepOrder: 0, Title: "Build", ExecutorType: "agent.run", Status: "pending"}}
	if err := s.CreateWorkflowSteps(ctx, nested); err != nil {
		t.Fatal(err)
	}
	nested[0].Status = "succeeded"
	if err := s.CreateWorkflowSteps(ctx, nested); err != nil {
		t.Fatalf("creating a reached step again: %v", err)
	}
	view, err := s.GetWorkflowRun(ctx, "wr-tree")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, step := range view.Steps {
		paths = append(paths, step.Path)
	}
	if len(paths) != 3 || paths[0] != "each" || paths[1] != "after" || paths[2] != "each/0/build" {
		t.Fatalf("paths = %v", paths)
	}
	if view.Steps[2].Status != "pending" || view.Steps[2].Pass == nil || *view.Steps[2].Pass != 0 {
		t.Fatalf("nested step = %+v", view.Steps[2])
	}
	runs, err := s.ListWorkflowRuns(ctx, WorkflowRunFilter{WithSteps: true})
	if err != nil || len(runs) != 1 || len(runs[0].Steps) != 2 {
		t.Fatalf("the runs list shows top-level steps only: %+v, %v", runs, err)
	}
}

// An attempt starts the step instance its path names, not a step of the
// same ID at the top of the run.
func TestAnAttemptStartsTheInstanceAtItsPath(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	run := WorkflowRun{ID: "wr-path", WorkflowName: "p", WorkflowTitle: "P", DefinitionSource: "p", DefinitionDigest: "d", DefinitionJSON: "{}", InputsJSON: "{}", Status: "running", CurrentStepID: "each/0/build", SchemaVersion: 2}
	pass := 0
	if err := s.CreateWorkflowRun(ctx, run, []WorkflowStep{
		{RunID: "wr-path", Path: "each", StepID: "each", StepOrder: 0, Status: "running"},
		{RunID: "wr-path", Path: "each/0/build", ParentPath: "each/0", StepID: "build", Pass: &pass, StepOrder: 0, ExecutorType: "agent.run", Status: "pending"},
	}); err != nil {
		t.Fatal(err)
	}
	attempt := WorkflowStepAttempt{ID: "wa-nested", RunID: "wr-path", Path: "each/0/build", StepID: "build", AttemptNumber: 1, ExecutorType: "agent.run", Status: "running"}
	if err := s.StartWorkflowStepAttempt(ctx, attempt, "{}"); err != nil {
		t.Fatalf("start the nested instance: %v", err)
	}
	view, err := s.GetWorkflowRun(ctx, "wr-path")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range view.Steps {
		if step.Path == "each/0/build" && (step.Status != "running" || step.LatestAttempt != 1) {
			t.Fatalf("nested instance = %+v", step)
		}
	}
}
