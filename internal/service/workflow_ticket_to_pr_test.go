package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The bundled ticket-to-merged-pr runs end to end (LEG-540): the card's own
// agent implements in a clone on the card's branch and reports its pull
// request, the review loop runs, the merge is waited for, the Burnlite
// ticket closes, and the card's agent stops.
func TestTicketToMergedPRRunsFromTheCardToTheMerge(t *testing.T) {
	agents, db, mt := newTestAgentService(t)
	ctx := context.Background()
	cloner := &recordingCloner{root: t.TempDir()}
	agents.(*agentService).SetRepositories(cloner)
	config := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", config)
	settings := NewSettingsService(db, nil)
	provider, key := BurnliteIntegration, "LEG-7"
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-7", Title: "Fix login", Status: "Doing", Provider: &provider, RemoteID: &key}); err != nil {
		t.Fatal(err)
	}

	runner := NewWorkflowAgentRunner(agents, db)
	implement := NewAgentRunExecutor(runner)
	implement.SetMacros(NewMacroService(settings))
	review := &fakeWorkflowExecutor{typeName: ReviewPassesStepType, result: WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"outcome": "approved"}}}
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": {State: PRStateOpen}}}
	closer := &fakeBurnliteCloser{}
	workflows := NewWorkflowService(db, []WorkflowExecutor{implement, review, NewPRWaitExecutor(states), NewBurnliteCloseExecutor(db, closer), NewAgentStopExecutor(runner)})

	example, ok := workflowengine.ExampleNamed("ticket-to-merged-pr")
	if !ok {
		t.Fatal("ticket-to-merged-pr is not bundled")
	}
	definition, err := workflowengine.Parse("ticket-to-merged-pr.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, definition, map[string]string{"task": "LEG-7", "repo": "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}

	// The card's own agent, in a clone on the card's branch, on the macro
	// as the card would send it.
	session, err := db.GetAgentSessionByTaskID(ctx, "LEG-7")
	if err != nil {
		t.Fatalf("the card has no agent: %v", err)
	}
	if len(cloner.requests) != 1 || cloner.requests[0].Repo != "acme/widget" || cloner.requests[0].FreshBranch != "legato/leg-7" {
		t.Fatalf("clones = %+v", cloner.requests)
	}
	brief, err := os.ReadFile(mt.envVarsFor(session.TmuxSession)["LEGATO_BRIEF_FILE"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brief), "Task LEG-7: Fix login\n\nImplement this task.") {
		t.Fatalf("brief = %q", brief)
	}

	// The agent reports its pull request; it stays for the review loop,
	// and the run waits for the merge.
	if _, err := workflows.Complete(ctx, view.Run.ID, "implement", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil); err != nil {
		t.Fatal(err)
	}
	if status, _ := stepStatus(t, workflows, view.Run.ID, "merged"); status != WorkflowStepWaiting {
		t.Fatalf("merged = %s", status)
	}
	if _, err := db.GetAgentSessionByTaskID(ctx, "LEG-7"); err != nil {
		t.Fatal("the card's agent stopped before the merge")
	}

	states.set(PRState{State: PRStateMerged, URL: "https://github.com/acme/widget/pull/12"})
	if err := workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	status, done := stepStatus(t, workflows, view.Run.ID, "stop")
	if status != "succeeded" || done.Run.Status != "succeeded" {
		t.Fatalf("stop = %s, run = %s", status, done.Run.Status)
	}
	if len(closer.closed) != 1 || closer.closed[0] != "LEG-7|completed|Merged in https://github.com/acme/widget/pull/12" {
		t.Fatalf("closed = %v", closer.closed)
	}
	if _, err := db.GetAgentSessionByTaskID(ctx, "LEG-7"); err == nil {
		t.Fatal("the card's agent still runs after the merge")
	}
}
