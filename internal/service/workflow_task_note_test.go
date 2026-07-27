package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type recordingTaskNotePublisher struct{ taskID, body string }

func (p *recordingTaskNotePublisher) AddTaskNote(_ context.Context, taskID, body string) error {
	p.taskID, p.body = taskID, body
	return nil
}

func TestTaskNoteExecutorPersistsAndPublishesJiraNote(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, remoteID := "jira", "LEG-42"
	if err := db.CreateTask(context.Background(), store.Task{ID: "LEG-42", Title: "Timeout", Provider: &provider, RemoteID: &remoteID}); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(t.TempDir(), "triage.md")
	if err := os.WriteFile(artifact, []byte("## Triage\n\nBlocked by missing logs."), 0600); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingTaskNotePublisher{}
	executor := NewTaskNoteExecutor(db, map[string]TaskNotePublisher{"jira": publisher})
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{RunID: "wr-1", StepID: "note", AttemptID: "wa-1", With: map[string]any{
		"task": map[string]any{"id": "LEG-42"}, "body_path": artifact,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %q", result.Status)
	}
	notes, err := db.ListTaskNotes(context.Background(), "LEG-42")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Body != "## Triage\n\nBlocked by missing logs." || notes[0].Source != "workflow" {
		t.Fatalf("notes = %#v", notes)
	}
	if publisher.taskID != "LEG-42" || publisher.body != notes[0].Body {
		t.Fatalf("published %q: %q", publisher.taskID, publisher.body)
	}
	if result.Outputs["note_id"] != notes[0].ID {
		t.Fatalf("outputs = %#v", result.Outputs)
	}
}
