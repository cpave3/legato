package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// Agent waits (LEG-485, spec LEG-482 §5.3–§5.6): a step ends on the first
// of its wait_for signals, found again from the tables by any host, so a
// wait survives a restart.

func reportStage(t *testing.T, s *store.Store, taskID, stage, note string) {
	t.Helper()
	session, err := s.GetAgentSessionInSlot(context.Background(), taskID, store.SlotMain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkStageService(s, nil).Report(context.Background(), SessionPlace(*session), session.ExecutionID, stage, note); err != nil {
		t.Fatal(err)
	}
}

func stepByPath(view *store.WorkflowRunView, path string) store.WorkflowStep {
	for _, step := range view.Steps {
		if step.Path == path {
			return step
		}
	}
	return store.WorkflowStep{}
}

func waitOutputs(t *testing.T, view *store.WorkflowRunView, path string) map[string]any {
	t.Helper()
	var outputs map[string]any
	if err := json.Unmarshal([]byte(stepByPath(view, path).OutputsJSON), &outputs); err != nil {
		t.Fatal(err)
	}
	return outputs
}

func (f placeFixture) check(t *testing.T, workflows *WorkflowService, runID string) *store.WorkflowRunView {
	t.Helper()
	if err := workflows.CheckAgentWaits(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, err := f.store.GetWorkflowRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// The acceptance test: an agent on the card reports Finished, the next
// step messages the same agent and waits for Finished again, Legato
// restarts between the two reports, and the second report ends the run.
// The agent reports the same note both times: each report is an event.
func TestAnAgentThatReportsFinishedTwiceMovesTheRunTwiceAcrossARestart(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - id: build
   title: Build
   type: agent.run
   with: {on: "${{ workflow.inputs.task }}", prompt: Build it, agent: claude-code}
   wait_for: [finished]
 - id: push
   title: Push
   type: agent.message
   with: {agent: {from: build}, prompt: Push it}
   wait_for: [finished]
`)
	if view.Steps[0].Status != "waiting_agent" {
		t.Fatalf("build = %+v", view.Steps[0])
	}
	if view = f.check(t, f.workflows, view.Run.ID); view.Steps[0].Status != "waiting_agent" {
		t.Fatalf("build ended with no signal: %+v", view.Steps[0])
	}

	reportStage(t, f.store, "LEG-7", "finished", "done")
	view = f.check(t, f.workflows, view.Run.ID)
	if view.Steps[0].Status != "succeeded" || view.Steps[1].Status != "waiting_agent" {
		t.Fatalf("steps = %+v", view.Steps)
	}
	built := waitOutputs(t, view, "build")
	if built["signal"] != "finished" || built["stage"] != "finished" || built["stage_reason"] != "done" || built["task"] != "LEG-7" {
		t.Fatalf("build's outputs = %v", built)
	}
	if !slices.ContainsFunc(f.sentTo("legato-LEG-7"), func(text string) bool {
		return strings.HasPrefix(text, "[legato workflow] Push it") && strings.Contains(text, "report your stage as finished")
	}) {
		t.Fatalf("sent = %q", f.sentTo("legato-LEG-7"))
	}
	// The first Finished is below push's watermark: the place was reset.
	if stage, _ := f.store.GetWorkStage(ctx, TaskPlace("LEG-7", "")); stage.Stage != store.WorkStageImplementing || stage.Source != store.WorkStageSourceWorkflow {
		t.Fatalf("stage = %+v", stage)
	}

	// Legato restarts: a new service on the same database.
	runner := NewWorkflowAgentRunner(f.agents, f.store)
	run := NewAgentRunExecutor(runner)
	restarted := NewWorkflowService(f.store, []WorkflowExecutor{run, NewAgentMessageExecutor(runner, f.store), NewAgentStopExecutor(runner)})
	if view = f.check(t, restarted, view.Run.ID); view.Steps[1].Status != "waiting_agent" {
		t.Fatalf("push ended on the first report: %+v", view.Steps[1])
	}

	reportStage(t, f.store, "LEG-7", "finished", "done")
	view = f.check(t, restarted, view.Run.ID)
	if view.Run.Status != "succeeded" || view.Steps[1].Status != "succeeded" {
		t.Fatalf("run = %s, steps = %+v", view.Run.Status, view.Steps)
	}
	if pushed := waitOutputs(t, view, "push"); pushed["signal"] != "finished" {
		t.Fatalf("push's outputs = %v", pushed)
	}
	if f.cardAgent(t) == nil {
		t.Fatal("the card's agent was stopped")
	}
}

// agent.stop with wait_for: [ended] waits for the agent to end (the
// operator closes it), then the run goes on.
func TestAgentStopWaitsForTheAgentToEnd(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, implementWith("")+" - {id: stop, title: Stop, type: agent.stop, with: {agent: {from: implement}}, wait_for: [ended]}\n")
	done, err := f.workflows.Complete(ctx, view.Run.ID, "implement", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stepByPath(done, "stop").Status != "waiting_agent" || f.cardAgent(t) == nil {
		t.Fatalf("stop = %+v; want it waiting on the running agent", stepByPath(done, "stop"))
	}
	if done = f.check(t, f.workflows, view.Run.ID); done.Run.Status != "running" {
		t.Fatalf("run = %s before the agent ended", done.Run.Status)
	}
	if err := f.agents.KillAgent(ctx, "LEG-7"); err != nil {
		t.Fatal(err)
	}
	if done = f.check(t, f.workflows, view.Run.ID); done.Run.Status != "succeeded" {
		t.Fatalf("run = %s, steps = %+v", done.Run.Status, done.Steps)
	}
}

// An agent on a task that ends while its step waits for something else
// fails the step, so Retry can start it again.
func TestAnAgentOnATaskThatEndsFailsAStepThatDoesNotWaitForEnded(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - {id: build, title: Build, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Build it}, wait_for: [finished]}
`)
	if err := f.agents.KillAgent(ctx, "LEG-7"); err != nil {
		t.Fatal(err)
	}
	view = f.check(t, f.workflows, view.Run.ID)
	if view.Run.Status != "failed" || !strings.Contains(view.Steps[0].Error, "ended before") {
		t.Fatalf("run = %s, step = %+v", view.Run.Status, view.Steps[0])
	}
}

// An agent on a task with no outputs waits for its agent to end.
func TestAnAgentOnATaskWithNoOutputsWaitsForItToEnd(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	if err := f.agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	view := f.start(t, `
schema_version: 2
name: chat
title: Chat
inputs:
  task: {type: task, required: true}
steps:
 - {id: talk, title: Talk, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Help the operator, reuse: true}}
`)
	if view.Steps[0].Status != "waiting_agent" {
		t.Fatalf("talk = %+v", view.Steps[0])
	}
	if err := f.agents.KillAgent(ctx, "LEG-7"); err != nil {
		t.Fatal(err)
	}
	if view = f.check(t, f.workflows, view.Run.ID); view.Run.Status != "succeeded" || waitOutputs(t, view, "talk")["signal"] != "ended" {
		t.Fatalf("run = %s, outputs = %s", view.Run.Status, view.Steps[0].OutputsJSON)
	}
}

// A timeout is absolute and swept with a claim: it fails the step, stops a
// workflow's own agent and leaves a task's agent alone.
func TestATimeoutFailsTheStepAndStopsOnlyAWorkflowsOwnAgent(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	own := f.start(t, `
schema_version: 2
name: own
title: Own
inputs:
  task: {type: task}
steps:
 - {id: build, title: Build, type: agent.run, with: {prompt: Build it}, timeout: 1h}
`)
	onTask := f.start(t, `
schema_version: 2
name: card
title: Card
inputs:
  task: {type: task, required: true}
steps:
 - {id: build, title: Build, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Build it}, wait_for: [finished], timeout: 2h}
`)
	if deadline := own.Steps[0].DeadlineAt; deadline == nil {
		t.Fatalf("no deadline: %+v", own.Steps[0])
	}
	ownAgent, err := f.store.GetAgentSessionByTaskID(ctx, own.Run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := f.workflows.SweepTimeouts(ctx, time.Now().Add(30*time.Minute)); err != nil || n != 0 {
		t.Fatalf("swept %d, %v before any deadline", n, err)
	}
	if n, err := f.workflows.SweepTimeouts(ctx, time.Now().Add(3*time.Hour)); err != nil || n != 2 {
		t.Fatalf("swept %d, %v; want both", n, err)
	}
	for _, runID := range []string{own.Run.ID, onTask.Run.ID} {
		view, _ := f.store.GetWorkflowRun(ctx, runID)
		if view.Run.Status != "failed" || !strings.Contains(view.Steps[0].Error, "timed out after") {
			t.Fatalf("run %s = %s, step = %+v", runID, view.Run.Status, view.Steps[0])
		}
	}
	if f.tmux.sessionAlive(ownAgent.TmuxSession) {
		t.Fatal("the workflow's own agent still runs")
	}
	if f.cardAgent(t) == nil {
		t.Fatal("the task's agent was stopped")
	}
	if n, _ := f.workflows.SweepTimeouts(ctx, time.Now().Add(3*time.Hour)); n != 0 {
		t.Fatalf("swept %d again", n)
	}
}

// A timeout fails through on_failure like any other failure: continued, the
// step's outputs carry error_code timeout and the run goes on.
func TestATimedOutStepFollowsItsOnFailureWithErrorCodeTimeout(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, `
schema_version: 2
name: own
title: Own
inputs:
  task: {type: task}
steps:
 - {id: build, title: Build, type: agent.run, with: {prompt: Build it}, timeout: 1h, on_failure: continue}
 - {id: after, title: After, type: gate, with: {title: "Timed out: ${{ steps.build.outputs.error_code }}"}}
`)
	if n, err := f.workflows.SweepTimeouts(ctx, time.Now().Add(2*time.Hour)); err != nil || n != 1 {
		t.Fatalf("swept %d, %v", n, err)
	}
	view, err := f.store.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	outputs := waitOutputs(t, view, "build")
	if outputs["status"] != "failed" || outputs["error_code"] != WorkflowErrorTimeout {
		t.Fatalf("outputs = %v", outputs)
	}
	if view.Run.Status == "failed" {
		t.Fatalf("the run failed; on_failure: continue should go on: %+v", view.Run)
	}
}

// fileCloner makes a clone folder with a git HEAD on the branch asked for
// and records it, as the repository service does.
type fileCloner struct {
	root     string
	db       *store.Store
	requests []JobCloneRequest
}

func (c *fileCloner) JobClone(ctx context.Context, req JobCloneRequest) (store.JobClone, error) {
	c.requests = append(c.requests, req)
	if req.CloneID != 0 {
		clone, err := c.db.JobCloneByID(ctx, req.CloneID)
		if err != nil {
			return store.JobClone{}, err
		}
		if req.Branch != "" {
			if err := os.WriteFile(filepath.Join(clone.Path, ".git", "HEAD"), []byte("ref: refs/heads/"+req.Branch+"\n"), 0o600); err != nil {
				return store.JobClone{}, err
			}
		}
		return *clone, nil
	}
	branch := req.Branch
	if branch == "" {
		branch = req.FreshBranch
	}
	path := filepath.Join(c.root, req.ExecutionID, "widget")
	if err := os.MkdirAll(filepath.Join(path, ".git", "refs", "heads"), 0o700); err != nil {
		return store.JobClone{}, err
	}
	if err := os.WriteFile(filepath.Join(path, ".git", "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o600); err != nil {
		return store.JobClone{}, err
	}
	clone := store.JobClone{ExecutionID: req.ExecutionID, TaskID: req.TaskID, Repo: req.Repo, Branch: branch, Path: path,
		CreatedAt: time.Now(), LastUsedAt: time.Now(), TaskIDs: []string{req.TaskID}, Slot: store.SlotMain}
	id, err := c.db.CreateJobClone(ctx, clone)
	clone.ID = id
	return clone, err
}

type recordedGrant struct{ grants []BranchGrant }

func (r *recordedGrant) GrantBranches(_ context.Context, g BranchGrant) (int, error) {
	r.grants = append(r.grants, g)
	return 1, nil
}

// fakeStackPRs records a stack for each clone and finds a PR for each of
// a task's branches, or fails as GitHub would.
type fakeStackPRs struct {
	db       *store.Store
	stack    []TaskStackBranch
	err      error
	tracked  map[string][]string
	repoSeen string
}

func (f *fakeStackPRs) Record(ctx context.Context, clone store.JobClone) error {
	data, _ := json.Marshal(f.stack)
	return f.db.PutCloneStack(ctx, store.CloneStack{CloneID: clone.ID, Root: "main", Branches: string(data), ReadAt: time.Now()})
}

func (f *fakeStackPRs) AddTaskBranches(_ context.Context, taskID, repo string, branches []string) ([]string, error) {
	if f.tracked == nil {
		f.tracked = map[string][]string{}
	}
	f.repoSeen = repo
	f.tracked[taskID] = append(f.tracked[taskID], branches...)
	return branches, nil
}

func (f *fakeStackPRs) TrackedBranches(_ context.Context, taskID string) ([]store.TaskBranch, error) {
	var out []store.TaskBranch
	for i, branch := range f.tracked[taskID] {
		out = append(out, store.TaskBranch{TaskID: taskID, Repo: f.repoSeen, Branch: branch, Position: i,
			PR: &store.PRMeta{PRNumber: 40 + i, PRURL: "https://github.com/acme/widget/pull/" + string(rune('0'+i)), State: "open", IsDraft: true}})
	}
	return out, nil
}

func (f *fakeStackPRs) DiscoverPRs(context.Context, string) error { return f.err }

func newCloneFixture(t *testing.T) (placeFixture, *fileCloner, *recordedGrant, *fakeStackPRs) {
	t.Helper()
	f := newPlaceFixture(t)
	cloner := &fileCloner{root: t.TempDir(), db: f.store}
	f.agents.(*agentService).SetRepositories(cloner)
	grants := &recordedGrant{}
	f.workflows.Executor("agent.run").(*AgentRunExecutor).SetBranchGrants(grants)
	prs := &fakeStackPRs{db: f.store, stack: []TaskStackBranch{
		{Name: "legato/leg-7-one", Parent: "main", State: "pushed", HeadSHA: "aaa"},
		{Name: "legato/leg-7-two", Parent: "legato/leg-7-one", State: "pushed", HeadSHA: "bbb"},
	}}
	f.workflows.SetAgentOutputs(AgentOutputSources{Stacks: prs, PRs: prs})
	return f, cloner, grants, prs
}

// A version 2 step with repo gets a fresh clone, never one its task has; a
// branch it names is made with the legato/ prefix and granted for pushes.
// At the signal Legato fills the clone, branch, stack and PRs.
func TestAVersion2StepGetsAFreshCloneOnAGrantedBranchAndFillsItsOutputs(t *testing.T) {
	ctx := context.Background()
	f, cloner, grants, _ := newCloneFixture(t)
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - {id: build, title: Build, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget, branch: one}, wait_for: [finished]}
 - {id: next, title: Next, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Next, repo: acme/widget, branch: two, clone: {from: build}, reuse: true}, wait_for: [finished]}
`)
	if len(cloner.requests) != 1 {
		t.Fatalf("clones asked for = %+v", cloner.requests)
	}
	if req := cloner.requests[0]; !req.Own || req.Branch != "legato/leg-7-one" || req.CloneID != 0 {
		t.Fatalf("clone request = %+v; want a fresh clone on legato/leg-7-one", req)
	}
	session := f.cardAgent(t)
	if len(grants.grants) != 1 || grants.grants[0].Repo != "acme/widget" || !slices.Equal(grants.grants[0].Branches, []string{"legato/leg-7-one"}) || grants.grants[0].ExecutionID != session.ExecutionID {
		t.Fatalf("grants = %+v", grants.grants)
	}
	reportStage(t, f.store, "LEG-7", "finished", "built")
	view = f.check(t, f.workflows, view.Run.ID)
	built := waitOutputs(t, view, "build")
	clone, err := f.store.JobCloneByPath(ctx, session.WorkingDir)
	if err != nil {
		t.Fatal(err)
	}
	if built["clone_id"] != float64(clone.ID) || built["branch"] != "legato/leg-7-one" || built["top_branch"] != "legato/leg-7-two" || built["working_dir"] != clone.Path {
		t.Fatalf("outputs = %v", built)
	}
	stack, _ := json.Marshal(built["stack"])
	if string(stack) != `[{"head_sha":"aaa","name":"legato/leg-7-one","parent":"main","state":"pushed"},{"head_sha":"bbb","name":"legato/leg-7-two","parent":"legato/leg-7-one","state":"pushed"}]` {
		t.Fatalf("stack = %s", stack)
	}
	prs, _ := json.Marshal(built["prs"])
	if !strings.Contains(string(prs), `"repo":"acme/widget"`) || !strings.Contains(string(prs), `"number":40`) || !strings.Contains(string(prs), `"draft":true`) || !strings.Contains(string(prs), `"branch":"legato/leg-7-one"`) {
		t.Fatalf("prs = %s", prs)
	}
	// The next step's agent is the same one: it was messaged, not cloned
	// again. A spawn with clone: {from} joins the clone (below).
	if len(cloner.requests) != 1 {
		t.Fatalf("clones asked for = %+v", cloner.requests)
	}
}

func TestCloneFromAnEarlierStepJoinsItsClone(t *testing.T) {
	f, cloner, grants, _ := newCloneFixture(t)
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - {id: build, title: Build, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget, stop: true}, wait_for: [finished]}
 - {id: next, title: Next, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Next, repo: acme/widget, branch: two, clone: {from: build}}, wait_for: [finished]}
`)
	reportStage(t, f.store, "LEG-7", "finished", "built")
	view = f.check(t, f.workflows, view.Run.ID)
	if stepByPath(view, "next").Status != "waiting_agent" {
		t.Fatalf("steps = %+v", view.Steps)
	}
	first, _ := waitOutputs(t, view, "build")["clone_id"].(float64)
	if len(cloner.requests) != 2 || cloner.requests[1].CloneID != int64(first) || cloner.requests[1].Branch != "legato/leg-7-two" {
		t.Fatalf("clone requests = %+v; want the second to join clone %v", cloner.requests, first)
	}
	if last := grants.grants[len(grants.grants)-1]; !slices.Equal(last.Branches, []string{"legato/leg-7-two"}) {
		t.Fatalf("grants = %+v", grants.grants)
	}
}

// A GitHub error at the signal fails the step: stale PRs would let a
// later step skip a review.
func TestAGitHubErrorAtTheSignalFailsTheStep(t *testing.T) {
	f, _, _, prs := newCloneFixture(t)
	prs.err = errors.New("github: 502")
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - {id: build, title: Build, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget}, wait_for: [finished]}
`)
	reportStage(t, f.store, "LEG-7", "finished", "built")
	view = f.check(t, f.workflows, view.Run.ID)
	if view.Run.Status != "failed" || !strings.Contains(view.Steps[0].Error, "github: 502") || view.Steps[0].ErrorCode != WorkflowErrorOutputsUnavailable {
		t.Fatalf("run = %s, step = %+v", view.Run.Status, view.Steps[0])
	}
}

// The contract names the signals that end the step.
func TestTheContractNamesTheAwaitedSignals(t *testing.T) {
	for signals, want := range map[string][]string{
		"finished": {"report your stage as finished", "workflow step fail"},
		"ended":    {"when your session ends"},
		"complete": {"When all outputs are ready, run `legato workflow step complete`"},
	} {
		contract := workflowContract(ResolvedWorkflowStep{RunID: "wr-1", StepID: "build", AttemptID: "wa-1", WaitFor: []string{signals}}, "")
		for _, text := range want {
			if !strings.Contains(contract, text) {
				t.Errorf("%s: contract lacks %q:\n%s", signals, text, contract)
			}
		}
		if signals != "complete" && strings.Contains(contract, "When all outputs are ready") {
			t.Errorf("%s: contract asks for complete:\n%s", signals, contract)
		}
	}
}

// An agent that completes its step fills what it declared; the host fills
// the rest before it moves the run on.
func TestACompletionGetsLegatosOutputsBesideTheDeclaredOnes(t *testing.T) {
	f, _, _, _ := newCloneFixture(t)
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - id: build
   title: Build
   type: agent.run
   with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget, branch: one}
   outputs:
     pr_number: {type: integer}
`)
	done, err := f.workflows.Complete(context.Background(), view.Run.ID, "build", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs := waitOutputs(t, done, "build")
	if outputs["pr_number"] != float64(12) || outputs["signal"] != "complete" || outputs["branch"] != "legato/leg-7-one" || outputs["clone_id"] == float64(0) {
		t.Fatalf("outputs = %v", outputs)
	}
}

// A message that declares outputs waits for the agent to complete it, on
// the message's own attempt, which its contract names.
func TestAMessageWithOutputsWaitsForTheAgentToCompleteIt(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, implementWith("")+` - id: ask
   title: Ask
   type: agent.message
   with: {agent: {from: implement}, prompt: "How did it go?"}
   outputs:
     verdict: {type: string}
`)
	done, err := f.workflows.Complete(ctx, view.Run.ID, "implement", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(done.Attempts, func(a store.WorkflowStepAttempt) bool { return a.Path == "ask" })
	if i < 0 || done.Attempts[i].Status != "waiting_agent" {
		t.Fatalf("attempts = %+v", done.Attempts)
	}
	ask := done.Attempts[i]
	if !slices.ContainsFunc(f.sentTo("legato-LEG-7"), func(text string) bool {
		return strings.HasPrefix(text, "[legato workflow] How did it go?") && strings.Contains(text, "--step ask --attempt "+ask.ID)
	}) {
		t.Fatalf("sent = %q", f.sentTo("legato-LEG-7"))
	}
	done, err = f.workflows.Complete(ctx, view.Run.ID, "ask", ask.ID, map[string]string{"verdict": "fine"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if done.Run.Status != "succeeded" || waitOutputs(t, done, "ask")["verdict"] != "fine" {
		t.Fatalf("run = %s, outputs = %s", done.Run.Status, stepByPath(done, "ask").OutputsJSON)
	}
}

// An interrupted agent on a task does not fail its step: the wait is the
// place's, so it binds to the agent that is resumed there.
func TestAWaitBindsAgainToTheAgentResumedInItsPlace(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - {id: build, title: Build, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Build it}, wait_for: [finished]}
`)
	first := f.cardAgent(t)
	if err := f.store.UpdateAgentSessionStatusInSlot(ctx, "LEG-7", store.SlotMain, store.AgentSessionInterrupted); err != nil {
		t.Fatal(err)
	}
	if view = f.check(t, f.workflows, view.Run.ID); view.Steps[0].Status != "waiting_agent" {
		t.Fatalf("build = %+v after an interruption", view.Steps[0])
	}
	f.tmux.sessions["legato-LEG-7"] = false
	if err := f.agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	resumed := f.cardAgent(t)
	if resumed == nil || resumed.ID == first.ID {
		t.Fatalf("resumed = %+v", resumed)
	}
	reportStage(t, f.store, "LEG-7", "finished", "done")
	view = f.check(t, f.workflows, view.Run.ID)
	if view.Run.Status != "succeeded" || waitOutputs(t, view, "build")["session_id"] != float64(resumed.ID) {
		t.Fatalf("run = %s, outputs = %s", view.Run.Status, view.Steps[0].OutputsJSON)
	}
}

// A GitHub error while the host fills a completed step's outputs fails the
// step too, with error code outputs_unavailable: a later if: on prs would
// otherwise skip a review on stale pull requests. Continued, the run goes on
// with the failure as the step's outputs.
func TestAGitHubErrorFillingACompletedStepFailsItAndFollowsOnFailure(t *testing.T) {
	ctx := context.Background()
	f, _, _, prs := newCloneFixture(t)
	prs.err = errors.New("github: 502")
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - id: build
   title: Build
   type: agent.run
   with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget, branch: one}
   on_failure: continue
   outputs:
     pr_number: {type: integer}
 - {id: after, title: After, type: gate, with: {title: "Build: ${{ steps.build.outputs.error_code }}"}}
`)
	if _, err := f.workflows.Complete(ctx, view.Run.ID, "build", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil); err != nil {
		t.Fatal(err)
	}
	view, err := f.store.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	build := stepByPath(view, "build")
	outputs := waitOutputs(t, view, "build")
	if build.Status != "failed" || outputs["error_code"] != WorkflowErrorOutputsUnavailable || !strings.Contains(build.Error, "github: 502") {
		t.Fatalf("build = %+v, outputs = %v", build, outputs)
	}
	if view.Run.Status == "failed" || stepByPath(view, "after").Status != WorkflowStepWaiting {
		t.Fatalf("run = %s, after = %+v; on_failure: continue should go on", view.Run.Status, stepByPath(view, "after"))
	}
}

// The host that advances a step its agent recorded from the pane fails it
// when GitHub errors (the default on_failure fails the run), and a Retry
// once GitHub answers completes it with its pull requests. The step is
// Ship's implement step: a workflow's own agent on the task, kept.
func TestAStepFailedFillingItsOutputsSucceedsOnRetry(t *testing.T) {
	ctx := context.Background()
	f, _, _, prs := newCloneFixture(t)
	pane := NewWorkflowService(f.store, nil)
	pane.SetRecordOnly()
	prs.err = errors.New("github: 502")
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - id: build
   title: Build
   type: agent.run
   with: {task: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget, branch: one, keep_agent: true}
   outputs:
     pr_number: {type: integer}
 - {id: after, title: After, type: gate, with: {title: Next}}
`)
	if _, err := pane.Complete(ctx, view.Run.ID, "build", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workflows.AdvanceDue(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := f.store.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	build := stepByPath(view, "build")
	if view.Run.Status != "failed" || view.Run.CurrentStepID != "build" || build.Status != "failed" || build.ErrorCode != WorkflowErrorOutputsUnavailable {
		t.Fatalf("run = %+v, build = %+v", view.Run, build)
	}
	if after := stepByPath(view, "after"); after.Status != "pending" {
		t.Fatalf("after = %+v; the next step must not start", after)
	}
	// A second host finds nothing left to do.
	if moved, err := f.workflows.AdvanceDue(ctx); err != nil || len(moved) != 0 {
		t.Fatalf("moved %v, %v", moved, err)
	}

	prs.err = nil
	view, err = f.workflows.Retry(ctx, view.Run.ID, "build", false)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[len(view.Attempts)-1]
	if attempt.Path != "build" || attempt.Status != "waiting_agent" {
		t.Fatalf("attempts = %+v", view.Attempts)
	}
	view, err = f.workflows.Complete(ctx, view.Run.ID, "build", attempt.ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs := waitOutputs(t, view, "build")
	if stepByPath(view, "build").Status != "succeeded" || outputs["signal"] != "complete" || len(outputs["prs"].([]any)) == 0 {
		t.Fatalf("build = %+v, outputs = %v", stepByPath(view, "build"), outputs)
	}
	if stepByPath(view, "after").Status != WorkflowStepWaiting {
		t.Fatalf("after = %+v", stepByPath(view, "after"))
	}
}

// When the agent service cannot read what a step waits for as its agent
// ends, it leaves the step to the workflow host, which reads the tables
// again, rather than failing a step that may wait for exactly that end.
func TestAnUnreadableWaitIsLeftToTheHostWhenTheAgentEnds(t *testing.T) {
	ctx := context.Background()
	f := newPlaceFixture(t)
	if err := f.agents.SpawnAgent(ctx, "LEG-7", 0, 0); err != nil {
		t.Fatal(err)
	}
	view := f.start(t, `
schema_version: 2
name: chat
title: Chat
inputs:
  task: {type: task, required: true}
steps:
 - {id: talk, title: Talk, type: agent.run, with: {on: "${{ workflow.inputs.task }}", prompt: Help the operator, reuse: true}}
`)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE workflow_steps SET wait_signals = '["ended"' WHERE run_id = ?`, view.Run.ID); err != nil {
		t.Fatal(err)
	}
	f.tmux.mu.Lock()
	delete(f.tmux.sessions, "legato-LEG-7")
	f.tmux.mu.Unlock()
	if err := f.agents.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := f.store.GetWorkflowRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Steps[0].Status != "waiting_agent" {
		t.Fatalf("talk = %+v; want it left waiting for the host", view.Steps[0])
	}
}

// The last step's completion has ended its run, and nothing reads its
// outputs: a GitHub error leaves its prs out rather than undo the end.
func TestAGitHubErrorFillingTheLastStepLeavesItsPRsOut(t *testing.T) {
	ctx := context.Background()
	f, _, _, prs := newCloneFixture(t)
	prs.err = errors.New("github: 502")
	view := f.start(t, `
schema_version: 2
name: build
title: Build
inputs:
  task: {type: task, required: true}
steps:
 - id: build
   title: Build
   type: agent.run
   with: {on: "${{ workflow.inputs.task }}", prompt: Build it, repo: acme/widget, branch: one}
   outputs:
     pr_number: {type: integer}
`)
	done, err := f.workflows.Complete(ctx, view.Run.ID, "build", view.Attempts[0].ID, map[string]string{"pr_number": "12"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputs := waitOutputs(t, done, "build")
	if _, has := outputs["prs"]; has || done.Run.Status != "succeeded" || outputs["signal"] != "complete" {
		t.Fatalf("run = %s, outputs = %v", done.Run.Status, outputs)
	}
}
