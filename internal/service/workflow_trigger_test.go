package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestColumnMoveWithOpenAgentWaitsForConfirmationThenStarts(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, name := range []string{"Doing", "Done"} {
		if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "AB-122", Title: "Finish", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "AB-122", TmuxSession: "agent-122", Command: "chimera", AgentKind: "chimera", Status: "running", Activity: "idle"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "close-agent.yaml"), []byte(`schema_version: 1
name: close-agent
title: Close active agent
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Done
    confirm: true
    inputs: {task: "${{ event.task }}"}
    conditions:
      all:
        - {field: task.has_active_agent, equals: true}
steps:
  - {id: close, title: Close agent, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
	coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	board := NewBoardService(db, events.New())
	if err := board.MoveCard(context.Background(), "AB-122", "Done"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), "AB-122"); err != nil {
		t.Fatal(err)
	}
	requests, err := db.ListWorkflowTriggerRequests(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Status != "waiting_confirmation" || len(executor.starts) != 0 {
		t.Fatalf("requests=%+v starts=%d", requests, len(executor.starts))
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := coordinator.Confirm(context.Background(), requests[0].ID)
			errs <- err
		}()
	}
	successes := 0
	for range 2 {
		if err := <-errs; err == nil {
			successes++
		}
	}
	confirmed, err := coordinator.Request(context.Background(), requests[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if successes != 1 || confirmed.Status != "started" || len(executor.starts) != 1 {
		t.Fatalf("successes=%d request=%+v starts=%d", successes, confirmed, len(executor.starts))
	}
}

func TestConfirmationSkipsWhenAgentSessionClosedAfterMove(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, name := range []string{"Doing", "Done"} {
		if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "AB-120", Title: "Finish", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := db.InsertAgentSessionReturningID(context.Background(), store.AgentSession{TaskID: "AB-120", TmuxSession: "agent-120", Command: "chimera", AgentKind: "chimera", Status: "running", Activity: "waiting"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "close-agent.yaml"), []byte(`schema_version: 1
name: close-agent
title: Close active agent
inputs: {task: {type: task, required: true}}
on:
  task.entered_column:
    column: Done
    confirm: true
    inputs: {task: "${{ event.task }}"}
    conditions: {all: [{field: task.has_active_agent, equals: true}]}
steps:
  - {id: close, title: Close agent, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded"}}
	coordinator := NewWorkflowTriggerCoordinator(db, NewWorkflowService(db, []WorkflowExecutor{executor}), func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	board := NewBoardService(db, events.New())
	if err := board.MoveCard(context.Background(), "AB-120", "Done"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), "AB-120"); err != nil {
		t.Fatal(err)
	}
	requests, _ := db.ListWorkflowTriggerRequests(context.Background(), "waiting_confirmation")
	if len(requests) != 1 {
		t.Fatalf("requests=%+v", requests)
	}
	if err := db.FinishAgentSession(context.Background(), sessionID); err != nil {
		t.Fatal(err)
	}
	confirmed, err := coordinator.Confirm(context.Background(), requests[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Status != "skipped" || len(executor.starts) != 0 {
		t.Fatalf("request=%+v starts=%d", confirmed, len(executor.starts))
	}
}

func TestColumnMoveWithoutOpenAgentSkipsConfirmedWorkflow(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, name := range []string{"Doing", "Done"} {
		if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "AB-121", Title: "Finish", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "close-agent.yaml"), []byte(`schema_version: 1
name: close-agent
title: Close active agent
inputs: {task: {type: task, required: true}}
on:
  task.entered_column:
    column: Done
    confirm: true
    inputs: {task: "${{ event.task }}"}
    conditions: {all: [{field: task.has_active_agent, equals: true}]}
steps:
  - {id: close, title: Close agent, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.stop", result: WorkflowExecutionResult{Status: "succeeded"}}
	coordinator := NewWorkflowTriggerCoordinator(db, NewWorkflowService(db, []WorkflowExecutor{executor}), func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	board := NewBoardService(db, events.New())
	if err := board.MoveCard(context.Background(), "AB-121", "Done"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), "AB-121"); err != nil {
		t.Fatal(err)
	}
	requests, err := db.ListWorkflowTriggerRequests(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Status != "skipped" || len(executor.starts) != 0 {
		t.Fatalf("requests=%+v starts=%d", requests, len(executor.starts))
	}
}

func TestColumnMoveStartsWorkflowOnceAndHonorsMetadataCondition(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: "Backlog", RemoteStatuses: `[]`, SortOrder: 0}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: "Triaged", RemoteStatuses: `[]`, SortOrder: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "AB-123", Title: "Triage", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "triage.yaml")
	if err := os.WriteFile(workflowPath, []byte(`
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
  repository: {type: repository, required: true}
on:
  task.entered_column:
    column: Triaged
    inputs:
      task: ${{ event.task }}
      repository: ${{ task.metadata.repository }}
    conditions:
      all:
        - field: task.metadata.triaged
          not_equals: true
steps:
  - id: run
    title: Run
    type: agent.run
    with: {prompt: triage}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
	workflows.SetRepositoryPresets([]workflowengine.RepositoryPreset{{Name: "legato", Repo: "cpave3/legato", PrimaryDir: "/projects/legato"}})
	coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	board := NewBoardService(db, events.New())
	if err := board.MoveCard(context.Background(), "AB-123", "Triaged"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), "AB-123"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), "AB-123"); err != nil {
		t.Fatal(err)
	}
	requests, _ := db.ListWorkflowTriggerRequests(context.Background(), "")
	if len(requests) != 1 || requests[0].Status != "started" || requests[0].RunID == "" || len(executor.starts) != 1 {
		t.Fatalf("requests=%+v starts=%d", requests, len(executor.starts))
	}
}

func TestColumnMoveWaitsForRepositoryAndCanResume(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, name := range []string{"Backlog", "Triaged"} {
		if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: name, RemoteStatuses: `[]`, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateTask(context.Background(), store.Task{ID: "AB-124", Title: "Triage", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "triage.yaml"), []byte(`schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
  repository: {type: repository, required: true}
on:
  task.entered_column:
    column: Triaged
    inputs: {task: "${{ event.task }}", repository: "${{ task.metadata.repository }}"}
steps:
  - {id: run, title: Run, type: agent.run, with: {prompt: triage}}
`), 0o600)
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
	workflows.SetRepositoryPresets([]workflowengine.RepositoryPreset{{Name: "one", Repo: "o/one", PrimaryDir: "/one"}, {Name: "two", Repo: "o/two", PrimaryDir: "/two"}})
	coordinator := NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
	board := NewBoardService(db, events.New())
	if err := board.MoveCard(context.Background(), "AB-124", "Triaged"); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.CaptureLatestForTask(context.Background(), "AB-124"); err != nil {
		t.Fatal(err)
	}
	requests, _ := db.ListWorkflowTriggerRequests(context.Background(), "waiting_input")
	if len(requests) != 1 || len(executor.starts) != 0 {
		t.Fatalf("requests=%+v starts=%d", requests, len(executor.starts))
	}
	resumed, err := coordinator.SupplyInputs(context.Background(), requests[0].ID, map[string]string{"repository": "two"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != "started" || len(executor.starts) != 1 {
		t.Fatalf("request=%+v starts=%d", resumed, len(executor.starts))
	}
	task, _ := db.GetTask(context.Background(), "AB-124")
	metadata, _ := store.ParseTaskMetadata(task.Metadata)
	if metadata["repository"] != "two" {
		t.Fatalf("metadata=%#v", metadata)
	}
}
