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
