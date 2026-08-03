package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestTaskWorkflowLauncherListsOnlyUnambiguousTaskWorkflows(t *testing.T) {
	dir := t.TempDir()
	write := func(name, inputs string) {
		t.Helper()
		data := "schema_version: 1\nname: " + name + "\ntitle: " + name + "\ninputs:\n" + inputs + "steps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: test}\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("one", "  card: {type: task, required: true}\n  label: {type: string, default: ready}\n")
	write("zero", "  label: {type: string}\n")
	write("two", "  source: {type: task}\n  target: {type: task}\n")
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Card", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
	launcher := NewTaskWorkflowLauncher(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })

	items, err := launcher.List(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "one" || items[0].TaskInput != "card" {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Values["card"] != "task-1" || items[0].Values["label"] != "ready" {
		t.Fatalf("values = %#v", items[0].Values)
	}
}

func TestTaskWorkflowLauncherPrefillsSingleRepositoryAndStartsRelatedRun(t *testing.T) {
	dir := t.TempDir()
	data := `schema_version: 1
name: triage
title: Triage
inputs:
  subject: {type: task, required: true}
  repository: {type: repository, required: true}
steps:
  - id: run
    title: Run
    type: agent.run
    with: {prompt: test}
`
	if err := os.WriteFile(filepath.Join(dir, "triage.yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Card", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	executor := &fakeWorkflowExecutor{typeName: "agent.run", result: WorkflowExecutionResult{Status: "waiting_agent"}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{executor})
	presets := []workflowengine.RepositoryPreset{{Name: "legato", Repo: "cpave3/legato", PrimaryDir: "/projects/legato"}}
	workflows.SetRepositoryPresets(presets)
	launcher := NewTaskWorkflowLauncher(db, workflows, func() workflowengine.DiscoveryResult {
		result := workflowengine.DiscoverPaths(dir)
		result.Repositories = presets
		return result
	})
	items, err := launcher.List(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Values["repository"] != "legato" {
		t.Fatalf("values = %#v", items[0].Values)
	}
	view, err := launcher.Start(context.Background(), "triage", map[string]string{"subject": "task-1", "repository": "legato"})
	if err != nil {
		t.Fatal(err)
	}
	if view.RelatedTaskID != "task-1" {
		t.Fatalf("related task = %q", view.RelatedTaskID)
	}
}
