package service

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Where a workflow's agent lives (LEG-607): agent.run with on: <task> is a
// plain agent in the task's main slot, kept unless stop: true; agent.message
// and agent.stop reach the agent an earlier step used.

const onTaskImplement = `
schema_version: 2
name: ship
title: Ship
inputs:
  task: {type: task, required: true}
steps:
 - id: implement
   title: Implement
   type: agent.run
   with: {on: "${{ workflow.inputs.task }}", prompt: Build it, agent: claude-code%s}
   outputs:
     pr_number: {type: integer}
`

type placeFixture struct {
	agents    AgentService
	store     *store.Store
	tmux      *mockTmux
	workflows *WorkflowService
}

func newPlaceFixture(t *testing.T) placeFixture {
	t.Helper()
	agents, s, mt := newTestAgentService(t)
	createTask(t, s, "LEG-7")
	runner := NewWorkflowAgentRunner(agents, s)
	run := NewAgentRunExecutor(runner)
	workflows := NewWorkflowService(s, []WorkflowExecutor{run, NewAgentDispatchExecutor(run), NewAgentMessageExecutor(runner, s), NewAgentStopExecutor(runner)})
	return placeFixture{agents, s, mt, workflows}
}

func (f placeFixture) start(t *testing.T, yaml string) *store.WorkflowRunView {
	t.Helper()
	def, err := workflowengine.Parse("ship.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.workflows.Start(context.Background(), def, map[string]string{"task": "LEG-7"})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (f placeFixture) sentTo(name string) []string {
	return append(f.tmux.sentMultilineFor(name), f.tmux.sentLinesFor(name)...)
}

func (f placeFixture) cardAgent(t *testing.T) *store.AgentSession {
	t.Helper()
	session, err := f.store.GetAgentSessionByTaskID(context.Background(), "LEG-7")
	if err != nil {
		return nil
	}
	return session
}

func implementWith(extra string) string {
	return strings.Replace(onTaskImplement, "%s", extra, 1)
}

func TestAnAgentOnATaskIsAPlainAgentOnTheCardThatFillsItsOutputsAndKeepsRunning(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, implementWith(""))
	attempt := view.Attempts[0]
	if attempt.Status != "waiting_agent" || attempt.TaskID != "LEG-7" {
		t.Fatalf("attempt = %+v", attempt)
	}
	session := f.cardAgent(t)
	if session == nil || session.TmuxSession != "legato-LEG-7" {
		t.Fatalf("the card's agent = %+v", session)
	}
	// No WorkflowRunID: its stage is the card's, as an operator's spawn.
	if session.WorkflowRunID != "" || session.ExecutionKind == store.ExecutionWorkflowAgent {
		t.Fatalf("session = %+v; want a plain task agent", session)
	}
	done, err := f.workflows.Complete(ctx, view.Run.ID, "implement", attempt.ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Run.Status != "succeeded" || !strings.Contains(done.Steps[0].OutputsJSON, `"pr_number":12`) {
		t.Fatalf("run = %s, outputs = %s", done.Run.Status, done.Steps[0].OutputsJSON)
	}
	if again := f.cardAgent(t); again == nil || again.ID != session.ID {
		t.Fatalf("the card's agent after the step = %+v; want it kept", again)
	}
}

// The shape ticket-to-merged-pr uses: a later agent.stop on the task, or on
// the agent the step used, stops the task's main-slot agent.
func TestALaterAgentStopStopsTheTasksAgent(t *testing.T) {
	for name, stop := range map[string]string{
		"task": `{task: "${{ workflow.inputs.task }}"}`,
		"from": `{agent: {from: implement}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newPlaceFixture(t)
			view := f.start(t, implementWith("")+" - {id: stop, title: Stop, type: agent.stop, with: "+stop+"}\n")
			done, err := f.workflows.Complete(context.Background(), view.Run.ID, "implement", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if done.Run.Status != "succeeded" {
				t.Fatalf("run = %s", done.Run.Status)
			}
			if f.cardAgent(t) != nil || f.tmux.sessionAlive("legato-LEG-7") {
				t.Fatal("the card's agent still runs")
			}
		})
	}
}

func TestStopTrueStopsTheTasksAgentOnceItsOutputsAreIn(t *testing.T) {
	f := newPlaceFixture(t)
	view := f.start(t, implementWith(", stop: true"))
	if _, err := f.workflows.Complete(context.Background(), view.Run.ID, "implement", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil); err != nil {
		t.Fatal(err)
	}
	if f.cardAgent(t) != nil {
		t.Fatal("the card's agent still runs")
	}
}

// The contract tells the agent which step to complete, since a task's agent
// outlives the step and its environment names the step it started on.
func TestTheContractOfAnAgentOnATaskNamesItsStep(t *testing.T) {
	assignment := buildWorkflowAssignment("Build it", ResolvedWorkflowStep{RunID: "wr-1", StepID: "implement", AttemptID: "wa-1",
		With: map[string]any{"on": map[string]any{"id": "LEG-7"}}}, "")
	for _, want := range []string{"`legato workflow step complete --run wr-1 --step implement --attempt wa-1`", "`legato workflow step fail --run wr-1 --step implement --attempt wa-1 \"reason\"`", "keep running after the step completes"} {
		if !strings.Contains(assignment, want) {
			t.Errorf("assignment lacks %q:\n%s", want, assignment)
		}
	}
}

func TestARunningAgentOnTheTaskIsRefusedUnlessReused(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	if err := f.agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	operators := f.cardAgent(t)
	refused := f.start(t, implementWith(""))
	if refused.Run.Status != "failed" || !strings.Contains(refused.Attempts[0].Error, "task LEG-7 already has an agent running: reuse: true messages it instead") {
		t.Fatalf("run = %s, attempt = %+v", refused.Run.Status, refused.Attempts[0])
	}

	view := f.start(t, implementWith(", reuse: true"))
	attempt := view.Attempts[0]
	if attempt.Status != "waiting_agent" || attempt.AgentSessionID == nil || *attempt.AgentSessionID != operators.ID {
		t.Fatalf("attempt = %+v; want it waiting on the running agent %d", attempt, operators.ID)
	}
	sent := f.sentTo("legato-LEG-7")
	if !slices.ContainsFunc(sent, func(text string) bool {
		return strings.HasPrefix(text, "[legato workflow] Build it") && strings.Contains(text, "--attempt "+attempt.ID)
	}) {
		t.Fatalf("sent = %q", sent)
	}
	// Cancelling never stops a reused agent.
	if _, err := f.workflows.Cancel(ctx, view.Run.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if again := f.cardAgent(t); again == nil || again.ID != operators.ID {
		t.Fatalf("the operator's agent = %+v", again)
	}
}

// The step's placement, not its first session, says the agent is the
// card's: once the operator has replaced the card's agent (restarted it, or
// stopped it and started another), a cancel still leaves the new one.
func TestCancellingAnOnTaskStepLeavesTheCardsReplacementAgent(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, implementWith(""))
	if view.Attempts[0].Status != "waiting_agent" {
		t.Fatalf("attempt = %+v", view.Attempts[0])
	}
	if err := f.agents.KillAgent(ctx, "LEG-7"); err != nil {
		t.Fatal(err)
	}
	if err := f.agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	replacement := f.cardAgent(t)
	if _, err := f.workflows.Cancel(ctx, view.Run.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if again := f.cardAgent(t); again == nil || again.ID != replacement.ID || !f.tmux.sessionAlive("legato-LEG-7") {
		t.Fatalf("the card's agent after the cancel = %+v; want %d kept", again, replacement.ID)
	}
}

func TestAMacroToAReusedAgentIsSentUntaggedAndTheContractTagged(t *testing.T) {
	settings, db := newSettingsFixture(t, "")
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-7", Title: "Fix login", Status: "Ready"}); err != nil {
		t.Fatal(err)
	}
	agents := &fakePlaceRunner{fakeWorkflowAgentService: &fakeWorkflowAgentService{}, running: &store.AgentSession{ID: 3, TaskID: "LEG-7", Status: "running"}}
	run := NewAgentRunExecutor(agents)
	run.SetMacros(NewMacroService(settings))
	result, err := run.Start(ctx, ResolvedWorkflowStep{RunID: "wr-1", TaskID: "wf-task", StepID: "implement", AttemptID: "wa-1",
		With: map[string]any{"on": map[string]any{"id": "LEG-7"}, "reuse": true, "macro": "Implement"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "waiting_agent" || result.AgentSession.ID != 3 || result.TaskID != "LEG-7" {
		t.Fatalf("result = %+v", result)
	}
	if len(agents.prompts) != 2 || !strings.HasPrefix(agents.prompts[0], "Task LEG-7: Fix login") || !strings.HasPrefix(agents.prompts[1], "[legato workflow] ") {
		t.Fatalf("prompts = %q", agents.prompts)
	}
}

func TestAgentMessageReachesTheAgentOfAnEarlierStepTagged(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, implementWith("")+` - {id: tell, title: Tell, type: agent.message, with: {agent: {from: implement}, prompt: "Push #${{ steps.implement.outputs.pr_number }}"}}
`)
	done, err := f.workflows.Complete(ctx, view.Run.ID, "implement", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Run.Status != "succeeded" {
		t.Fatalf("run = %s: %+v", done.Run.Status, done.Attempts)
	}
	if !slices.Contains(f.sentTo("legato-LEG-7"), "[legato workflow] Push #12") {
		t.Fatalf("sent = %q", f.sentTo("legato-LEG-7"))
	}
	if outputs := done.Steps[1].OutputsJSON; !strings.Contains(outputs, `"task":"LEG-7"`) || !strings.Contains(outputs, `"session_id"`) {
		t.Fatalf("outputs = %s", outputs)
	}
}

// A workflow's own agent kept with keep: true can be messaged and stopped
// by a later step; once it has gone, a message fails.
func TestAgentMessageAndStopReachAKeptWorkflowAgent(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, `
schema_version: 2
name: kept
title: Kept
inputs:
  task: {type: task}
steps:
 - {id: build, title: Build, type: agent.run, with: {prompt: Build it, agent: claude-code, keep: true}}
 - {id: tell, title: Tell, type: agent.message, with: {agent: {from: build}, prompt: Push it}}
 - {id: stop, title: Stop, type: agent.stop, with: {agent: {from: build}}}
 - {id: again, title: Again, type: agent.message, with: {agent: {from: tell}, prompt: "Hello?"}}
`)
	session, err := f.store.GetAgentSessionByTaskID(ctx, view.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	done, err := f.workflows.Complete(ctx, view.Run.ID, "build", view.Attempts[0].ID, nil, nil)
	if err == nil || done.Run.Status != "failed" {
		t.Fatalf("run = %+v, err = %v; want the last message to fail", done.Run, err)
	}
	if !slices.Contains(f.sentTo(session.TmuxSession), "[legato workflow] Push it") {
		t.Fatalf("sent = %q", f.sentTo(session.TmuxSession))
	}
	if f.tmux.sessionAlive(session.TmuxSession) {
		t.Fatal("the kept agent still runs after agent.stop")
	}
	i := slices.IndexFunc(done.Attempts, func(attempt store.WorkflowStepAttempt) bool { return attempt.Path == "again" })
	if i < 0 || !strings.Contains(done.Attempts[i].Error, "has ended") {
		t.Fatalf("attempts = %+v", done.Attempts)
	}
	if last := done.Attempts[i]; last.Status != "failed" {
		t.Fatalf("last attempt = %+v", last)
	}
}

func TestAgentMessageToATaskWithNoAgentFails(t *testing.T) {
	f := newPlaceFixture(t)
	view := f.start(t, `
schema_version: 2
name: tell
title: Tell
inputs:
  task: {type: task, required: true}
steps:
 - {id: tell, title: Tell, type: agent.message, with: {task: "${{ workflow.inputs.task }}", prompt: hi}}
`)
	if view.Run.Status != "failed" || !strings.Contains(view.Attempts[0].Error, "task LEG-7 has no agent running") {
		t.Fatalf("run = %s, attempt = %+v", view.Run.Status, view.Attempts[0])
	}
}

// An agent.dispatch step is read as agent.run with on: <task> and reuse:
// true that does not wait: it starts the task's agent, or messages it.
func TestAgentDispatchStillRunsReadAsAgentRun(t *testing.T) {
	f := newPlaceFixture(t)
	const dispatch = `
schema_version: 1
name: send
title: Send
inputs:
  task: {type: task, required: true}
steps:
 - {id: send, title: Send, type: agent.dispatch, with: {task: "${{ workflow.inputs.task }}", prompt: Create a plan, working_dir: /repo}}
`
	first := f.start(t, dispatch)
	if first.Run.Status != "succeeded" || !strings.Contains(first.Steps[0].OutputsJSON, `"created":true`) || !strings.Contains(first.Steps[0].OutputsJSON, `"working_dir":"/repo"`) {
		t.Fatalf("run = %s, outputs = %s", first.Run.Status, first.Steps[0].OutputsJSON)
	}
	session := f.cardAgent(t)
	if session == nil || session.WorkflowRunID != "" {
		t.Fatalf("the card's agent = %+v", session)
	}
	second := f.start(t, dispatch)
	if second.Run.Status != "succeeded" || !strings.Contains(second.Steps[0].OutputsJSON, `"created":false`) {
		t.Fatalf("run = %s, outputs = %s", second.Run.Status, second.Steps[0].OutputsJSON)
	}
	if !slices.Contains(f.sentTo("legato-LEG-7"), "[legato workflow] Create a plan") {
		t.Fatalf("sent = %q", f.sentTo("legato-LEG-7"))
	}
	if again := f.cardAgent(t); again == nil || again.ID != session.ID {
		t.Fatalf("the card's agent = %+v", again)
	}
}

// agent.dispatch's repo names the pull request a macro reads, as it always
// did (address-feedback gives repo with the task's working_dir): it never
// asks for a job clone, so a new agent starts in working_dir, or where agents
// start by default.
func TestAgentDispatchRepoIsThePullRequestsNotAClone(t *testing.T) {
	for name, dir := range map[string]string{"in working_dir": "/repo", "by default": ""} {
		t.Run(name, func(t *testing.T) {
			f := newPlaceFixture(t)
			view := f.start(t, `
schema_version: 1
name: send
title: Send
inputs:
  task: {type: task, required: true}
steps:
 - {id: send, title: Send, type: agent.dispatch, with: {task: "${{ workflow.inputs.task }}", prompt: Address it, repo: acme/app, pr_number: 4, working_dir: "`+dir+`"}}
`)
			if view.Run.Status != "succeeded" {
				t.Fatalf("run = %s, attempt = %+v", view.Run.Status, view.Attempts[0])
			}
			session := f.cardAgent(t)
			if session == nil || session.CloneRepo != "" || (dir != "" && session.WorkingDir != dir) {
				t.Fatalf("the card's agent = %+v; want it in %q, no clone", session, dir)
			}
		})
	}
}

// fakePlaceRunner is a runner whose task already has an agent running.
type fakePlaceRunner struct {
	*fakeWorkflowAgentService
	running *store.AgentSession
	prompts []string
}

func (f *fakePlaceRunner) AgentSessionInSlot(_ context.Context, taskID, slot string) (*store.AgentSession, error) {
	if f.running != nil && f.running.TaskID == taskID && slot == store.SlotMain {
		return f.running, nil
	}
	return nil, store.ErrNotFound
}

func (f *fakePlaceRunner) PromptAgent(_ context.Context, _ string, prompt string) error {
	f.prompts = append(f.prompts, prompt)
	return nil
}
