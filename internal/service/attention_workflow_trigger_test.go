package service

import (
	"context"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestWorkflowTriggerAttentionClassifiesBlockingAndFailedStates(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().ExecContext(ctx, `INSERT INTO task_transitions (id, task_id, source, from_column, to_column, task_json)
		VALUES ('transition-1', 'task-1', 'test', 'Backlog', 'Doing', '{}')`); err != nil {
		t.Fatal(err)
	}
	for _, request := range []store.WorkflowTriggerRequest{
		{ID: "input", TransitionID: "transition-1", WorkflowName: "Input", DefinitionSource: "test", DefinitionDigest: "input", DefinitionJSON: `{}`, InputsJSON: `{}`, MissingInputsJSON: `["repo"]`, Status: "waiting_input"},
		{ID: "confirm", TransitionID: "transition-1", WorkflowName: "Confirm", DefinitionSource: "test", DefinitionDigest: "confirm", DefinitionJSON: `{}`, InputsJSON: `{}`, MissingInputsJSON: `[]`, Status: "waiting_confirmation"},
		{ID: "failed", TransitionID: "transition-1", WorkflowName: "Failed", DefinitionSource: "test", DefinitionDigest: "failed", DefinitionJSON: `{}`, InputsJSON: `{}`, MissingInputsJSON: `[]`, Status: "failed"},
	} {
		if _, err := db.UpsertWorkflowTriggerRequest(ctx, request); err != nil {
			t.Fatal(err)
		}
	}

	items, err := NewWorkflowTriggerAttentionAdapter(nil, db).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]AttentionInput{}
	for _, item := range items {
		byKey[item.SourceKey] = item
	}
	for key, want := range map[string]struct {
		kind    string
		urgency string
	}{
		"input":   {AttentionSignalWorkflowTriggerInput, "normal"},
		"confirm": {AttentionSignalWorkflowTriggerConfirmation, "normal"},
		"failed":  {AttentionSignalWorkflowTriggerFailed, "high"},
	} {
		item := byKey[key]
		if item.SignalKind != want.kind || item.Urgency != want.urgency {
			t.Fatalf("%s item = %+v", key, item)
		}
	}
}
