package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func TestBuildWorkflowServiceRegistersTaskMove(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Ready", RemoteStatuses: "[]", SortOrder: 0}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTask(ctx, store.Task{ID: "subject-1", Title: "Subject", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	board := service.NewBoardService(db, bus)
	workflows, err := buildWorkflowService(db, &config.Config{}, workflowRuntimeOptions{Bus: bus, Board: board, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := workflowengine.Parse("move.yaml", []byte(`
schema_version: 1
name: move
title: Move
inputs:
  task: {type: task, required: true}
steps:
  - id: move
    title: Move
    type: task.move
    with:
      task: ${{ workflow.inputs.task }}
      column: Ready
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
	task, _ := db.GetTask(ctx, "subject-1")
	if task.Status != "Ready" {
		t.Fatalf("status = %q", task.Status)
	}
}
