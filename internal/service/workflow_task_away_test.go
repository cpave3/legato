package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// awayTaskStore is a database with task T-1 away on worker srv-1.
func awayTaskStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateTask(ctx, store.Task{ID: "T-1", Title: "Fix it", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAwayTask(ctx, store.AwayTask{HandoffID: "ho-1", TaskID: "T-1", WorkerInstanceID: "srv-1", WorkerTaskID: "w-1",
		State: store.AwayAway, Purpose: HandoffMakeChange, Kind: HandoffNewBranch, CreatedAt: "2026-11-12T00:00:00Z", UpdatedAt: "2026-11-12T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTheMetadataStepRefusesATaskThatIsAway(t *testing.T) {
	db := awayTaskStore(t)
	_, err := NewTaskMetadataExecutor(db).Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"task": map[string]any{"id": "T-1"}, "values": map[string]any{"triaged": true}}})
	var away *TaskAwayError
	if !errors.As(err, &away) {
		t.Fatalf("err = %v, want a TaskAwayError", err)
	}
	task, _ := db.GetTask(context.Background(), "T-1")
	if task.Metadata != nil && strings.Contains(*task.Metadata, "triaged") {
		t.Fatalf("metadata changed: %s", *task.Metadata)
	}
}

func TestTheNoteStepSaysTheWorkersAgentWillNotSeeANoteOnAnAwayTask(t *testing.T) {
	db := awayTaskStore(t)
	result, err := NewTaskNoteExecutor(db).Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"task": map[string]any{"id": "T-1"}, "body": "Triaged: low risk."}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outputs["away_on"] != "srv-1" || !strings.Contains(result.Outputs["warning"].(string), "won't see it") {
		t.Fatalf("outputs %+v", result.Outputs)
	}
	if notes, _ := db.ListTaskNotes(context.Background(), "T-1"); len(notes) != 1 {
		t.Fatalf("notes %+v; want the note kept on the main", notes)
	}
}
