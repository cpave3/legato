package workflow_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

const draft = `schema_version: 1
name: standup
title: Standup
on:
  schedule:
    every: weekdays
    at: "09:00"
    enabled: true
  task.entered_column: {column: Triage}
steps:
  - id: note
    title: Note
    type: task.note
    with: {task: "${{ workflow.task }}", body: hi}
`

func TestADraftIsSavedWithEveryTriggerPaused(t *testing.T) {
	dir := t.TempDir()
	path, err := workflow.SaveDraft([]byte(draft), dir, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "standup.workflow.yaml") {
		t.Fatalf("path = %s", path)
	}
	data, _ := os.ReadFile(path)
	definition, err := workflow.Parse(path, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range definition.On.List() {
		if trigger.EnabledByDefault {
			t.Fatalf("trigger %s saved on:\n%s", trigger.Kind, data)
		}
	}
	if len(definition.On.List()) != 2 {
		t.Fatalf("triggers = %+v", definition.On.List())
	}
	if _, err := workflow.SaveDraft([]byte(draft), dir, func(string) bool { return false }); !errors.Is(err, workflow.ErrWorkflowNameTaken) {
		t.Fatalf("an existing file = %v", err)
	}
	if _, err := workflow.SaveDraft([]byte(strings.Replace(draft, "name: standup", "name: other", 1)), dir, func(n string) bool { return n == "other" }); !errors.Is(err, workflow.ErrWorkflowNameTaken) {
		t.Fatalf("a taken name = %v", err)
	}
	if _, err := workflow.SaveDraft([]byte(strings.Replace(draft, `at: "09:00"`, `at: "9am"`, 1)), dir, nil); err == nil || !strings.Contains(err.Error(), "at") {
		t.Fatalf("an invalid draft = %v", err)
	}
}
