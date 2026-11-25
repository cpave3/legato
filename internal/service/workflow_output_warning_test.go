package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// An agent step that finished with no output lost what its agent found when
// the agent was removed: the run view, the run list and `legato workflow
// show` warn (LEG-532).
func TestAgentStepThatFinishedWithNoOutputIsWarnedOf(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := NewWorkflowService(db, nil)

	cases := []struct {
		name, executor, resolved, outputs string
		artifact                          bool
		want                              string // "" for no warning
	}{
		{name: "no outputs", executor: "agent.run", resolved: `{"prompt":"x"}`, outputs: `{}`, want: "the agent was removed"},
		{name: "outputs declared but empty", executor: "agent.run", resolved: `{"prompt":"x"}`, outputs: `{"digest":""}`, want: "the agent was removed"},
		{name: "keep_agent", executor: "agent.run", resolved: `{"prompt":"x","keep_agent":true}`, outputs: `{}`, want: "Its agent was kept"},
		{name: "keep", executor: "agent.run", resolved: `{"prompt":"x","keep":true}`, outputs: `{}`, want: "Its agent was kept"},
		{name: "on a task", executor: "agent.run", resolved: `{"prompt":"x","on":{"id":"LEG-7"}}`, outputs: `{}`, want: "Its agent was kept"},
		{name: "dispatch", executor: "agent.dispatch", resolved: `{"message":"x"}`, outputs: `{}`, want: "the task's agent"},
		{name: "with output", executor: "agent.run", resolved: `{"prompt":"x"}`, outputs: `{"digest":"news"}`},
		{name: "with artifact", executor: "agent.run", resolved: `{"prompt":"x"}`, outputs: `{}`, artifact: true},
		{name: "not an agent", executor: "task.note", resolved: `{}`, outputs: `{}`},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runID := "wr-" + strings.ReplaceAll(c.name, " ", "-")
			run := store.WorkflowRun{ID: runID, WorkflowName: "news", WorkflowTitle: "News", DefinitionSource: "test", DefinitionDigest: "sha", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "succeeded"}
			step := store.WorkflowStep{RunID: runID, StepID: "digest", Title: "Write the digest", ExecutorType: c.executor, ResolvedInputsJSON: c.resolved, OutputsJSON: c.outputs, Status: "succeeded"}
			if c.artifact {
				step.Status, run.Status, run.CurrentStepID = "pending", "running", "digest"
			}
			if err := db.CreateWorkflowRun(ctx, run, []store.WorkflowStep{step}); err != nil {
				t.Fatal(err)
			}
			if c.artifact {
				attemptID := fmt.Sprintf("wa-%d", i)
				if err := db.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: attemptID, RunID: runID, StepID: "digest", AttemptNumber: 1, ExecutorType: c.executor, Status: "running"}, c.resolved); err != nil {
					t.Fatal(err)
				}
				if err := db.InsertWorkflowArtifact(ctx, store.WorkflowArtifact{ID: fmt.Sprintf("art-%d", i), RunID: runID, Path: "digest", StepID: "digest", AttemptID: attemptID, OutputKey: "digest", ManagedPath: "/x", OriginalPath: "/y", MediaType: "text/markdown", Digest: "sha"}); err != nil {
					t.Fatal(err)
				}
				if err := db.CompleteWorkflowStep(ctx, runID, "digest", attemptID, c.outputs, "after"); err != nil {
					t.Fatal(err)
				}
			}

			view, err := svc.Show(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			got := view.Steps[0].OutputWarning
			if c.want == "" {
				if got != "" || view.Run.OutputWarning != "" {
					t.Fatalf("warned: step %q, run %q", got, view.Run.OutputWarning)
				}
				return
			}
			if !strings.Contains(got, "finished with no output") || !strings.Contains(got, c.want) {
				t.Fatalf("step warning = %q, want it to say %q", got, c.want)
			}
			if !strings.Contains(view.Run.OutputWarning, "Write the digest") {
				t.Fatalf("run warning = %q", view.Run.OutputWarning)
			}
			runs, err := svc.ListRuns(ctx, store.WorkflowRunFilter{Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			for _, listed := range runs {
				if listed.ID == runID && listed.OutputWarning != view.Run.OutputWarning {
					t.Fatalf("listed warning = %q, want %q", listed.OutputWarning, view.Run.OutputWarning)
				}
			}
		})
	}
}
