package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestBuildWorkflowServiceRegistersChimeraSessionRemove(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "subject-1", Title: "Subject", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	workflows, err := buildWorkflowService(db, &config.Config{}, workflowRuntimeOptions{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("remove.yaml", []byte(`
schema_version: 1
name: remove
title: Remove
inputs: {task: {type: task, required: true}}
steps:
  - id: remove
    title: Remove
    type: chimera.session.remove
    with: {task: "${{ workflow.inputs.task }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, definition, map[string]string{"task": "subject-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "succeeded" {
		t.Fatalf("run = %+v", view.Run)
	}
}
