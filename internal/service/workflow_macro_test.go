package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestAWorkflowAgentStepResolvesTheMacrosCurrentText(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Fix login", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	svc := NewMacroService(settings)
	if err := svc.Save(ctx, "Review", macros.Macro{Keys: "Review with my skill.\n", AppliesTo: "pr", Agent: "codex", Tier: "smart"}, "web"); err != nil {
		t.Fatal(err)
	}

	agents := &fakeWorkflowAgentService{}
	run := NewAgentRunExecutor(agents)
	run.SetMacros(svc)
	if _, err := run.Start(ctx, ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf-task", StepID: "review", With: map[string]any{"macro": "review"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(agents.opts.Assignment, "Review with my skill.\n\n## Workflow completion contract") {
		t.Fatalf("assignment = %q", agents.opts.Assignment)
	}
	if agents.opts.AgentKind != "codex" || agents.opts.Tier != "smart" {
		t.Fatalf("a step that names no agent takes the macro's: %q %q", agents.opts.AgentKind, agents.opts.Tier)
	}
	if _, err := run.Start(ctx, ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf-task", With: map[string]any{"macro": "Review", "agent": "chimera"}}); err != nil {
		t.Fatal(err)
	}
	if agents.opts.AgentKind != "chimera" || agents.opts.Tier != "" {
		t.Fatalf("the step's agent wins, with its own tier: %q %q", agents.opts.AgentKind, agents.opts.Tier)
	}

	// An edit applies to the next run.
	if err := svc.Save(ctx, "Review", macros.Macro{Keys: "Edited."}, "web"); err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeTaskAgentDispatcher{store: db}
	dispatch := NewAgentDispatchExecutor(dispatcher, db)
	dispatch.SetMacros(svc)
	if _, err := dispatch.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "task-1"}, "macro": "Review", "working_dir": "/repo"}}); err != nil {
		t.Fatal(err)
	}
	if dispatcher.opts.Assignment != "Task task-1: Fix login\n\nEdited." {
		t.Fatalf("dispatch assignment = %q", dispatcher.opts.Assignment)
	}
}

func TestAWorkflowStepWithAnUnknownMacroFails(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	svc := NewMacroService(settings)
	run := NewAgentRunExecutor(&fakeWorkflowAgentService{})
	run.SetMacros(svc)
	_, err := run.Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{"macro": "nope"}})
	if err == nil || !strings.Contains(err.Error(), `macro "nope"`) {
		t.Fatalf("err = %v", err)
	}
	dispatch := NewAgentDispatchExecutor(&fakeTaskAgentDispatcher{store: db}, db)
	if _, err := dispatch.Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "task-1"}, "macro": "Plan", "working_dir": "/r"}}); err == nil || !strings.Contains(err.Error(), "macros") {
		t.Fatalf("no macro service: %v", err)
	}
}

func TestAMacroStepOnAPullRequestNamesIt(t *testing.T) {
	settings, _ := newSettingsFixture(t, "")
	agents := &fakeWorkflowAgentService{}
	run := NewAgentRunExecutor(agents)
	run.SetMacros(NewMacroService(settings))
	if _, err := run.Start(context.Background(), ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf", With: map[string]any{"macro": "Review", "repo": "acme/web", "pr_number": 12}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(agents.opts.Assignment, "Pull request acme/web#12: https://github.com/acme/web/pull/12\n\nReview this pull request.") {
		t.Fatalf("assignment = %q", agents.opts.Assignment)
	}
	if agents.opts.Repo != "acme/web" {
		t.Fatalf("repo = %q", agents.opts.Repo)
	}
}

func TestADispatchedMacroOnAPullRequestNamesTheTaskAndPR(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Fix login", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	dispatcher := &fakeTaskAgentDispatcher{store: db}
	dispatch := NewAgentDispatchExecutor(dispatcher, db)
	dispatch.SetMacros(NewMacroService(settings))
	if _, err := dispatch.Start(ctx, ResolvedWorkflowStep{With: map[string]any{"task": map[string]any{"id": "task-1"}, "macro": "Address feedback", "repo": "acme/web", "pr_number": 12, "working_dir": ""}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dispatcher.opts.Assignment, "Task task-1: Fix login\nPull request acme/web#12: https://github.com/acme/web/pull/12\n\nAddress the review feedback") {
		t.Fatalf("assignment = %q", dispatcher.opts.Assignment)
	}
}
