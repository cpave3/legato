package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestWorkflowRepositoryInputAndMetadataExecutor(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateTask(context.Background(), store.Task{ID: "LEG-1", Title: "Triage me", Status: "Triaged"}); err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("triage.yaml", []byte(`
schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
  repository: {type: repository, required: true}
steps:
  - id: mark
    title: Mark triaged
    type: task.metadata.set
    with:
      task: ${{ workflow.inputs.task }}
      values: {triaged: true, repo: "${{ workflow.inputs.repository.repo }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWorkflowService(db, []WorkflowExecutor{NewTaskMetadataExecutor(db)})
	svc.SetRepositoryPresets([]workflowengine.RepositoryPreset{{Name: "legato", Repo: "cpave3/legato", PrimaryDir: "/projects/legato", BaseBranch: "main"}})
	view, err := svc.Start(context.Background(), definition, map[string]string{"task": "LEG-1", "repository": "legato"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run status = %q", view.Run.Status)
	}
	task, err := db.GetTask(context.Background(), "LEG-1")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.ParseTaskMetadata(task.Metadata)
	if err != nil || metadata["triaged"] != true || metadata["repo"] != "cpave3/legato" {
		t.Fatalf("metadata = %#v, err = %v", metadata, err)
	}
}

// A repository added while Legato runs is a preset at once.
func TestWorkflowRepositoryInputFindsARepositoryAddedLater(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, err := workflowengine.Parse("pick.yaml", []byte(`
schema_version: 1
name: pick
title: Pick
inputs:
  repository: {type: repository, required: true}
steps:
  - id: checkout
    title: Checkout
    type: repo.ensure
`))
	if err != nil {
		t.Fatal(err)
	}
	catalog := NewRepositoryPresetService(db, nil, t.TempDir())
	svc := NewWorkflowService(db, []WorkflowExecutor{&fakeWorkflowExecutor{typeName: "repo.ensure", result: WorkflowExecutionResult{Status: "succeeded"}}})
	svc.SetRepositorySource(catalog.WorkflowPresets)
	if _, err := svc.Start(context.Background(), definition, map[string]string{"repository": "widget"}); err == nil {
		t.Fatal("a repository nobody added was found")
	}
	if _, err := catalog.Save(context.Background(), RepositoryPreset{Name: "widget", Repo: "acme/widget"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), definition, map[string]string{"repository": "widget"}); err != nil {
		t.Fatalf("after it was added: %v", err)
	}
}
