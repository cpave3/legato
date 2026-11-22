package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestShowSaysWhichTriggerStartedARun(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	request, err := db.InsertWorkflowTriggerEvent(ctx, store.WorkflowTriggerRequest{ID: "wtr-1", WorkflowName: "nightly", DefinitionSource: "test", DefinitionJSON: `{}`, InputsJSON: `{}`, MissingInputsJSON: `[]`, Status: "started", TriggerKind: "schedule", Item: "2026-11-21T09:00:00Z", EventKey: "2026-11-21T09:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	step := []store.WorkflowStep{{RunID: "", StepID: "s", Title: "S", ExecutorType: "task.note", DefinitionJSON: `{}`, Status: "pending"}}
	for _, run := range []store.WorkflowRun{
		{ID: "wr-scheduled", WorkflowName: "nightly", WorkflowTitle: "Nightly", DefinitionSource: "test", DefinitionDigest: "d", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "s", TriggerRequestID: request.ID},
		{ID: "wr-manual", WorkflowName: "nightly", WorkflowTitle: "Nightly", DefinitionSource: "test", DefinitionDigest: "d", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "s"},
	} {
		step[0].RunID = run.ID
		if err := db.CreateWorkflowRun(ctx, run, step); err != nil {
			t.Fatal(err)
		}
	}
	workflows := NewWorkflowService(db, nil)
	for id, want := range map[string]string{"wr-scheduled": "schedule", "wr-manual": ""} {
		view, err := workflows.Show(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if view.TriggerKind != want {
			t.Fatalf("%s trigger kind = %q, want %q", id, view.TriggerKind, want)
		}
	}
}
