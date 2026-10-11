package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestAddTaskNoteRecordsADurableNoteOnTheTask(t *testing.T) {
	s := newTestStoreForReport(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task1", Title: "Task", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	note, err := service.AddTaskNote(ctx, s, "task1", "  The retry bug is in the callback path.\n", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(note.ID, "tn-") || note.TaskID != "task1" || note.Source != "agent" || note.Body != "The retry bug is in the callback path." {
		t.Fatalf("note = %#v", note)
	}
	notes, err := s.ListTaskNotes(ctx, "task1")
	if err != nil || len(notes) != 1 || notes[0].ID != note.ID || notes[0].Body != note.Body {
		t.Fatalf("stored notes = %#v, %v", notes, err)
	}

	if _, err := service.AddTaskNote(ctx, s, "task1", " \n ", "agent"); err == nil {
		t.Fatal("a blank note was accepted")
	}
	if _, err := service.AddTaskNote(ctx, s, "missing", "text", "agent"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("note on a missing task = %v, want ErrNotFound", err)
	}
	if notes, _ := s.ListTaskNotes(ctx, "task1"); len(notes) != 1 {
		t.Fatalf("rejected notes were stored: %#v", notes)
	}
}
