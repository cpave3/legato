package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// fakePRStates is GitHub's pull requests, as the wait step reads them.
type fakePRStates struct {
	mu     sync.Mutex
	states map[string]PRState
	reads  int
}

func (f *fakePRStates) set(state PRState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states["acme/widget#12"] = state
}

func (f *fakePRStates) PullRequestState(_ context.Context, repo string, number int) (PRState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.states[repo+"#"+itoa(number)], nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// stepOutputs are a step's recorded outputs.
func stepOutputs(t *testing.T, view *store.WorkflowRunView, step string) map[string]any {
	t.Helper()
	for _, s := range view.Steps {
		if s.StepID == step {
			var outputs map[string]any
			if err := json.Unmarshal([]byte(s.OutputsJSON), &outputs); err != nil {
				t.Fatal(err)
			}
			return outputs
		}
	}
	t.Fatalf("no step %s", step)
	return nil
}

const prWaitWorkflow = `
schema_version: 1
name: wait
title: Wait
steps:
 - id: merged
   title: Wait for the merge
   type: github.pr.wait
   with: {repo: acme/widget, pr_number: 12, until: UNTIL}
 - id: after
   title: After
   type: task.context
   with: {task: "${{ workflow.task }}"}
`

func startPRWait(t *testing.T, until string, states *fakePRStates) (*WorkflowService, *store.WorkflowRunView) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewPRWaitExecutor(states), NewTaskContextExecutor(NewBoardService(db, nil))})
	def, err := workflowengine.Parse("wait.yaml", []byte(strings.ReplaceAll(prWaitWorkflow, "UNTIL", until)))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(context.Background(), def, nil)
	if err != nil {
		t.Fatal(err)
	}
	return workflows, view
}

func stepStatus(t *testing.T, workflows *WorkflowService, runID, step string) (string, *store.WorkflowRunView) {
	t.Helper()
	view, err := workflows.Show(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range view.Steps {
		if s.StepID == step {
			return s.Status, view
		}
	}
	t.Fatalf("no step %s", step)
	return "", nil
}

func TestAPRWaitStepWaitsUntilThePullRequestMerges(t *testing.T) {
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": {State: PRStateOpen}}}
	workflows, view := startPRWait(t, "merged", states)
	ctx := context.Background()
	if status, _ := stepStatus(t, workflows, view.Run.ID, "merged"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s; want waiting on an open pull request", status)
	}
	// Still open: a check leaves it waiting.
	if err := workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	if status, _ := stepStatus(t, workflows, view.Run.ID, "merged"); status != WorkflowStepWaiting {
		t.Fatalf("step = %s after a check of an open PR", status)
	}
	states.set(PRState{State: PRStateMerged, MergedAt: "2026-11-23T10:00:00Z", URL: "https://github.com/acme/widget/pull/12"})
	if err := workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	status, after := stepStatus(t, workflows, view.Run.ID, "merged")
	if status != "succeeded" || after.Run.Status != "succeeded" {
		t.Fatalf("step = %s, run = %s; want the merge to complete it and run the next step", status, after.Run.Status)
	}
	outputs := stepOutputs(t, after, "merged")
	if outputs["state"] != "merged" || outputs["merged"] != true || outputs["merged_at"] != "2026-11-23T10:00:00Z" || outputs["url"] != "https://github.com/acme/widget/pull/12" {
		t.Fatalf("outputs = %v", outputs)
	}
}

func TestAPRThatMergedAlreadyGoesStraightOn(t *testing.T) {
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": {State: PRStateMerged}}}
	workflows, view := startPRWait(t, "merged", states)
	if status, after := stepStatus(t, workflows, view.Run.ID, "merged"); status != "succeeded" || after.Run.Status != "succeeded" {
		t.Fatalf("step = %s, run = %s", status, after.Run.Status)
	}
}

func TestAPRClosedWithoutMergingFailsAWaitForTheMerge(t *testing.T) {
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": {State: PRStateOpen}}}
	workflows, view := startPRWait(t, "merged", states)
	states.set(PRState{State: PRStateClosed})
	if err := workflows.CheckPRWaits(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, after := stepStatus(t, workflows, view.Run.ID, "merged")
	if status != "failed" || after.Run.Status != "failed" || !strings.Contains(after.Attempts[0].Error, "closed without merging") {
		t.Fatalf("step = %s, run = %s, attempt = %+v", status, after.Run.Status, after.Attempts[0])
	}
}

func TestAWaitUntilClosedTakesACloseOrAMerge(t *testing.T) {
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": {State: PRStateOpen}}}
	workflows, view := startPRWait(t, "closed", states)
	states.set(PRState{State: PRStateClosed})
	if err := workflows.CheckPRWaits(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, after := stepStatus(t, workflows, view.Run.ID, "merged")
	if status != "succeeded" {
		t.Fatalf("step = %s", status)
	}
	if outputs := stepOutputs(t, after, "merged"); outputs["state"] != "closed" || outputs["merged"] != false {
		t.Fatalf("outputs = %v", outputs)
	}
}

// The loop checks at once, after each refresh of a card's pull requests,
// and on its own clock.
func TestThePRWaitLoopChecksOnAPRRefresh(t *testing.T) {
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": {State: PRStateOpen}}}
	workflows, view := startPRWait(t, "merged", states)
	bus := events.New()
	stop := workflows.StartPRWaitLoop(context.Background(), bus, time.Hour)
	defer stop()
	states.set(PRState{State: PRStateMerged})
	bus.Publish(events.Event{Type: events.EventPRStatusUpdated, At: time.Now()})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if status, _ := stepStatus(t, workflows, view.Run.ID, "merged"); status == "succeeded" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a PR refresh did not move the step on")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
