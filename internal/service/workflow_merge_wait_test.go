package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// A run that waits for its card's pull request to merge (LEG-614).
const mergeWaitWorkflow = `
schema_version: 1
name: ship
title: Ship
inputs:
  task: {type: task, required: true}
steps:
 - id: merged
   title: Wait for the merge
   type: github.pr.wait
   with: {repo: acme/widget, pr_number: 12, task: "${{ workflow.inputs.task }}"NOTIFY}
`

func boolRef(v bool) *bool { return &v }

// openPR is pull request 12, open, on branch legato/leg-7.
func openPR() PRState {
	return PRState{State: PRStateOpen, URL: "https://github.com/acme/widget/pull/12", HeadBranch: "legato/leg-7", BaseBranch: "main", Conflicts: boolRef(false), CheckStatus: "pass"}
}

type mergeWaitFixture struct {
	db        *store.Store
	workflows *WorkflowService
	attention *AttentionService
	states    *fakePRStates
	resumer   *fakeResumer
	runID     string
}

// fakeResumer is the macro start Resume agent uses, and the agent
// service it stops a resumed agent with.
type fakeResumer struct {
	db      *store.Store
	starts  []MacroStart
	killed  []string
	running bool // Start finds the card's agent running and queues
	err     error
}

func (f *fakeResumer) Start(ctx context.Context, req MacroStart) (MacroStartResult, error) {
	f.starts = append(f.starts, req)
	if f.err != nil {
		return MacroStartResult{}, f.err
	}
	if f.running {
		return MacroStartResult{TaskID: req.TaskID, Queued: true}, nil
	}
	if err := f.db.InsertAgentSession(ctx, store.AgentSession{TaskID: req.TaskID, TmuxSession: "legato-" + req.TaskID, Command: "claude", AgentKind: "claude-code", Status: "running"}); err != nil {
		return MacroStartResult{}, err
	}
	return MacroStartResult{TaskID: req.TaskID, Started: true, AgentKind: "claude-code"}, nil
}

func (f *fakeResumer) KillAgent(ctx context.Context, taskID string) error {
	f.killed = append(f.killed, taskID)
	session, err := f.db.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	return f.db.FinishAgentSession(ctx, session.ID)
}

func newMergeWait(t *testing.T, notify string) *mergeWaitFixture {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	remote := "LEG-7"
	if err := db.CreateTask(ctx, store.Task{ID: "card0001", Title: "Fix the login", Status: "Done", RemoteID: &remote, CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	states := &fakePRStates{states: map[string]PRState{"acme/widget#12": openPR()}}
	workflows := NewWorkflowService(db, []WorkflowExecutor{NewPRWaitExecutor(states)})
	resumer := &fakeResumer{db: db}
	workflows.SetAgentResumer(resumer, resumer)
	def, err := workflowengine.Parse("ship.yaml", []byte(strings.ReplaceAll(mergeWaitWorkflow, "NOTIFY", notify)))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, def, map[string]string{"task": "card0001"})
	if err != nil {
		t.Fatal(err)
	}
	attention := NewAttentionService(db, nil)
	adapter := NewWorkflowAttentionAdapter(workflows)
	if err := attention.RegisterProducer(adapter); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(adapter.Producer(), adapter); err != nil {
		t.Fatal(err)
	}
	return &mergeWaitFixture{db: db, workflows: workflows, attention: attention, states: states, resumer: resumer, runID: view.Run.ID}
}

// items are the open needs-you items of the run's merge, after a
// reconcile.
func (f *mergeWaitFixture) items(t *testing.T) []AttentionItem {
	t.Helper()
	ctx := context.Background()
	if err := f.attention.Reconcile(ctx, "workflows"); err != nil {
		t.Fatal(err)
	}
	all, err := f.attention.List(ctx, AttentionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var items []AttentionItem
	for _, item := range all {
		if item.SignalKind == AttentionSignalWorkflowMergeWaiting {
			items = append(items, item)
		}
	}
	return items
}

func (f *mergeWaitFixture) item(t *testing.T) AttentionItem {
	t.Helper()
	items := f.items(t)
	if len(items) != 1 {
		t.Fatalf("merge items = %+v; want one", items)
	}
	return items[0]
}

func actionOrder(item AttentionItem) []string {
	var ids []string
	for _, action := range item.Actions {
		ids = append(ids, action.ID)
	}
	return ids
}

func actionByID(item AttentionItem, id string) *AttentionAction {
	for i := range item.Actions {
		if item.Actions[i].ID == id {
			return &item.Actions[i]
		}
	}
	return nil
}

func TestAWaitingMergeIsANeedsYouItemForTheCard(t *testing.T) {
	f := newMergeWait(t, "")
	item := f.item(t)
	if item.Title != "Review and merge PR #12" || item.TaskID != "card0001" {
		t.Fatalf("item = %q for %q; want the merge for the card", item.Title, item.TaskID)
	}
	if got := strings.Join(actionOrder(item), ","); got != "open,open_pr,resume_agent" {
		t.Fatalf("actions = %s", got)
	}
	if pr := actionByID(item, "open_pr"); pr.Kind != "navigate" || pr.Href != "https://github.com/acme/widget/pull/12" {
		t.Fatalf("open PR = %+v", pr)
	}
	if resume := actionByID(item, "resume_agent"); resume.Kind != "execute" || resume.Picks != "macro" || resume.Label != "Resume agent" {
		t.Fatalf("resume = %+v", resume)
	}
	if RailGroupOf(item.SignalKind) != "workflows" {
		t.Fatalf("rail group = %s", RailGroupOf(item.SignalKind))
	}
	count, err := f.workflows.AttentionCount(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("workflows badge = %d, %v; want the waiting merge counted", count, err)
	}
}

func TestTheItemSaysWhenThePRConflictsOrCIFails(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	before := f.item(t)
	if strings.Contains(before.Reason, "conflicts") || strings.Contains(before.Reason, "CI") {
		t.Fatalf("reason = %q for a clean PR", before.Reason)
	}
	if err := f.attention.Dismiss(ctx, before.ID, before.SourceRevision); err != nil {
		t.Fatal(err)
	}
	conflicted := openPR()
	conflicted.Conflicts = boolRef(true)
	f.states.set(conflicted)
	changed, err := f.workflows.checkPRWaits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != f.runID {
		t.Fatalf("changed runs = %v; want the run whose PR changed", changed)
	}
	item := f.item(t)
	if !strings.Contains(item.Reason, "PR #12 conflicts with main") || item.SourceRevision == before.SourceRevision {
		t.Fatalf("reason = %q, revision %q (was %q)", item.Reason, item.SourceRevision, before.SourceRevision)
	}
	if item.Status != "open" {
		t.Fatalf("status = %s; a dismissed item comes back when the PR starts to conflict", item.Status)
	}
	// GitHub working mergeability out again says nothing new.
	unknown := conflicted
	unknown.Conflicts = nil
	unknown.CheckStatus = "fail"
	f.states.set(unknown)
	if _, err := f.workflows.checkPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	item = f.item(t)
	if !strings.Contains(item.Reason, "PR #12 conflicts with main") || !strings.Contains(item.Reason, "CI failing") {
		t.Fatalf("reason = %q; want the conflict kept and CI failing", item.Reason)
	}
	// Nothing changed: no run to tell of.
	if changed, _ := f.workflows.checkPRWaits(ctx); len(changed) != 0 {
		t.Fatalf("changed = %v on a read that changed nothing", changed)
	}
}

func TestTheItemClearsWhenThePRMerges(t *testing.T) {
	f := newMergeWait(t, "")
	f.item(t)
	merged := openPR()
	merged.State = PRStateMerged
	f.states.set(merged)
	if err := f.workflows.CheckPRWaits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := f.items(t); len(items) != 0 {
		t.Fatalf("items = %+v after the merge", items)
	}
	if status, view := stepStatus(t, f.workflows, f.runID, "merged"); status != "succeeded" || view.Run.Status != "succeeded" {
		t.Fatalf("step %s, run %s", status, view.Run.Status)
	}
}

func TestTheItemClearsWhenThePRCloses(t *testing.T) {
	f := newMergeWait(t, "")
	f.item(t)
	closed := openPR()
	closed.State = PRStateClosed
	f.states.set(closed)
	if err := f.workflows.CheckPRWaits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := f.items(t); len(items) != 0 {
		t.Fatalf("items = %+v after the close", items)
	}
}

func TestNotifyFalseMakesNoItem(t *testing.T) {
	f := newMergeWait(t, ", notify: false")
	if items := f.items(t); len(items) != 0 {
		t.Fatalf("items = %+v with notify: false", items)
	}
	if count, _ := f.workflows.AttentionCount(context.Background()); count != 0 {
		t.Fatalf("badge = %d with notify: false", count)
	}
}

func TestTheRunPageSaysWhereTheMergeStands(t *testing.T) {
	f := newMergeWait(t, "")
	view, err := f.workflows.Show(context.Background(), f.runID)
	if err != nil {
		t.Fatal(err)
	}
	wait, ok := view.Details["merged"].(MergeWait)
	if !ok {
		t.Fatalf("details = %#v", view.Details["merged"])
	}
	if wait.Number != 12 || wait.TaskID != "card0001" || wait.HeadBranch != "legato/leg-7" || wait.CheckStatus != "pass" || wait.Conflicts {
		t.Fatalf("wait = %+v", wait)
	}
	if view.ResumeTaskID != "card0001" || !view.CanResumeAgent || view.ResumeAgentState != "" {
		t.Fatalf("resume = %q %v %q", view.ResumeTaskID, view.CanResumeAgent, view.ResumeAgentState)
	}
}

func TestResumeAgentStartsTheCardsAgentWithTheMacroOnThePRsBranch(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	result, err := f.workflows.ResumeAgent(ctx, f.runID, "Rebase and repush")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Started || len(f.resumer.starts) != 1 {
		t.Fatalf("result = %+v, starts = %+v", result, f.resumer.starts)
	}
	start := f.resumer.starts[0]
	if start.Name != "Rebase and repush" || start.TaskID != "card0001" || start.Branch != "legato/leg-7" ||
		start.PR == nil || start.PR.Repo != "acme/widget" || start.PR.Number != 12 || start.PR.URL != "https://github.com/acme/widget/pull/12" {
		t.Fatalf("start = %+v, pr = %+v", start, start.PR)
	}
	// The run still waits for the merge, and the card stays where it is.
	if status, view := stepStatus(t, f.workflows, f.runID, "merged"); status != WorkflowStepWaiting || view.Run.Status != "running" {
		t.Fatalf("step %s, run %s; want it still waiting", status, view.Run.Status)
	}
	card, _ := f.db.GetTask(ctx, "card0001")
	if card.Status != "Done" {
		t.Fatalf("card moved to %s", card.Status)
	}
	// While the agent runs, the item offers to send it a macro and to open it.
	item := f.item(t)
	if got := strings.Join(actionOrder(item), ","); got != "open,open_pr,resume_agent,open_agent" {
		t.Fatalf("actions = %s", got)
	}
	if resume := actionByID(item, "resume_agent"); resume.Label != "Send a macro" {
		t.Fatalf("resume = %+v", resume)
	}
	if !strings.Contains(item.Reason, "agent is running") {
		t.Fatalf("reason = %q; want it to say the macro goes to the running agent", item.Reason)
	}
	if open := actionByID(item, "open_agent"); open.Href != "/agents?task=card0001&workflow="+f.runID {
		t.Fatalf("open agent = %+v", open)
	}
	// The merge completes the run and stops the agent the resume started.
	merged := openPR()
	merged.State = PRStateMerged
	f.states.set(merged)
	if err := f.workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	if status, view := stepStatus(t, f.workflows, f.runID, "merged"); status != "succeeded" || view.Run.Status != "succeeded" {
		t.Fatalf("step %s, run %s after the merge", status, view.Run.Status)
	}
	if strings.Join(f.resumer.killed, ",") != "card0001" {
		t.Fatalf("killed = %v; want the resumed agent stopped", f.resumer.killed)
	}
}

func TestACloseStopsTheResumedAgentToo(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	if _, err := f.workflows.ResumeAgent(ctx, f.runID, "Rebase and repush"); err != nil {
		t.Fatal(err)
	}
	closed := openPR()
	closed.State = PRStateClosed
	f.states.set(closed)
	if err := f.workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.resumer.killed, ",") != "card0001" {
		t.Fatalf("killed = %v", f.resumer.killed)
	}
}

func TestTheMergeLeavesAnAgentTheOperatorStartedAlone(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	if _, err := f.workflows.ResumeAgent(ctx, f.runID, "Rebase and repush"); err != nil {
		t.Fatal(err)
	}
	// The resumed agent ends; the operator starts one of their own.
	session, err := f.db.GetAgentSessionByTaskID(ctx, "card0001")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.FinishAgentSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DeleteDeadAgentSessions(ctx, "card0001"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.InsertAgentSession(ctx, store.AgentSession{TaskID: "card0001", TmuxSession: "legato-card0001", Command: "claude", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	merged := openPR()
	merged.State = PRStateMerged
	f.states.set(merged)
	if err := f.workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.resumer.killed) != 0 {
		t.Fatalf("killed = %v; the operator's own agent must be left alone", f.resumer.killed)
	}
}

func TestResumeAgentSendsTheMacroToARunningAgentAndDoesNotStopIt(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	if err := f.db.InsertAgentSession(ctx, store.AgentSession{TaskID: "card0001", TmuxSession: "legato-card0001", Command: "claude", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	f.resumer.running = true
	result, err := f.workflows.ResumeAgent(ctx, f.runID, "Rebase and repush")
	if err != nil || !result.Queued {
		t.Fatalf("result = %+v, %v; want the macro queued to the running agent", result, err)
	}
	merged := openPR()
	merged.State = PRStateMerged
	f.states.set(merged)
	if err := f.workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.resumer.killed) != 0 {
		t.Fatalf("killed = %v; an agent the resume did not start is not the run's", f.resumer.killed)
	}
}

func TestResumeAgentRefusesAnAgentWaitingOnTheOperator(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	if err := f.db.InsertAgentSession(ctx, store.AgentSession{TaskID: "card0001", TmuxSession: "legato-card0001", Command: "claude", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.UpdateAgentActivity(ctx, "card0001", store.ActivityWaiting); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workflows.ResumeAgent(ctx, f.runID, "Rebase and repush"); !errors.Is(err, ErrResumeAgentWaiting) {
		t.Fatalf("err = %v; want ErrResumeAgentWaiting", err)
	}
	if len(f.resumer.starts) != 0 {
		t.Fatalf("starts = %+v", f.resumer.starts)
	}
	item := f.item(t)
	if got := strings.Join(actionOrder(item), ","); got != "open,open_pr,open_agent" {
		t.Fatalf("actions = %s; want Open agent in place of Resume", got)
	}
	view, _ := f.workflows.Show(ctx, f.runID)
	if view.CanResumeAgent || view.ResumeAgentState != "waiting" {
		t.Fatalf("can resume %v, agent %q", view.CanResumeAgent, view.ResumeAgentState)
	}
}

func TestResumeAgentRefusesAFinishedRun(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	merged := openPR()
	merged.State = PRStateMerged
	f.states.set(merged)
	if err := f.workflows.CheckPRWaits(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workflows.ResumeAgent(ctx, f.runID, "Rebase and repush"); !errors.Is(err, store.ErrWorkflowStateConflict) {
		t.Fatalf("err = %v; want a state conflict", err)
	}
}

func TestTheResumeActionOnTheItemNeedsAMacro(t *testing.T) {
	f := newMergeWait(t, "")
	ctx := context.Background()
	item := f.item(t)
	request := AttentionActionRequest{ActionID: "resume_agent", ExpectedRevision: item.SourceRevision}
	if err := f.attention.Execute(ctx, item.ID, request); err == nil {
		t.Fatal("resume without a macro was taken")
	}
	request.Text = "Rebase and repush"
	if err := f.attention.Execute(ctx, item.ID, request); err != nil {
		t.Fatal(err)
	}
	if len(f.resumer.starts) != 1 || f.resumer.starts[0].Name != "Rebase and repush" {
		t.Fatalf("starts = %+v", f.resumer.starts)
	}
}

// The loop tells Attention when a waiting PR changes.
func TestThePRWaitLoopPublishesAChangedPR(t *testing.T) {
	f := newMergeWait(t, "")
	bus := events.New()
	changed := bus.Subscribe(events.EventWorkflowChanged)
	stop := f.workflows.StartPRWaitLoop(context.Background(), bus, time.Hour)
	defer stop()
	conflicted := openPR()
	conflicted.Conflicts = boolRef(true)
	f.states.set(conflicted)
	bus.Publish(events.Event{Type: events.EventPRStatusUpdated, At: time.Now()})
	select {
	case event := <-changed:
		if payload, _ := event.Payload.(events.WorkflowChangedPayload); payload.RunID != f.runID || payload.Kind != PRWaitChangedKind {
			t.Fatalf("payload = %+v", event.Payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no EventWorkflowChanged for a PR that started to conflict")
	}
}

func TestAMacroOnAGoneFolderStartsInAFreshCloneOfTheBranch(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	ctx := context.Background()
	gone := "/jobs/ex-1/widget"
	spy.gone[gone] = true
	spy.saved = &TaskResumeSession{ID: "conv-1", CWD: gone}
	if err := svc.Save(ctx, "Rebase and repush", macros.Macro{Keys: "Rebase on main and push.\n"}, "web"); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Start(ctx, MacroStart{Name: "Rebase and repush", TaskID: "abc12345", PR: &MacroPR{Repo: "acme/widget", Number: 12}, Branch: "legato/leg-7"})
	if err != nil || !result.Started || len(spy.spawned) != 1 {
		t.Fatalf("result = %+v, %v, spawned %+v", result, err, spy.spawned)
	}
	opts := spy.spawned[0]
	if opts.Repo != "acme/widget" || opts.Branch != "legato/leg-7" || !opts.PushedBranch {
		t.Fatalf("spawn = repo %q branch %q pushed %v; want a fresh clone of the PR's branch", opts.Repo, opts.Branch, opts.PushedBranch)
	}
	if opts.WorkingDir != "" || opts.ResumeSessionID != "" || opts.SessionPolicy == "resume" {
		t.Fatalf("spawn = dir %q resume %q policy %q; want a new conversation", opts.WorkingDir, opts.ResumeSessionID, opts.SessionPolicy)
	}
	if !strings.HasSuffix(opts.Prompt, "Rebase on main and push.") {
		t.Fatalf("prompt = %q", opts.Prompt)
	}
	notes, err := svc.settings.db.ListTaskNotes(ctx, "abc12345")
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0].Body, gone) || !strings.Contains(notes[0].Body, "legato/leg-7") {
		t.Fatalf("notes = %+v, %v; want one saying the folder was gone", notes, err)
	}
}

func TestAMacroResumesInItsLiveFolder(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	ctx := context.Background()
	live := "/jobs/ex-1/widget"
	spy.saved = &TaskResumeSession{ID: "conv-1", CWD: live}
	if err := svc.Save(ctx, "Rebase and repush", macros.Macro{Keys: "Rebase on main and push.\n"}, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, MacroStart{Name: "Rebase and repush", TaskID: "abc12345", PR: &MacroPR{Repo: "acme/widget", Number: 12}, Branch: "legato/leg-7"}); err != nil {
		t.Fatal(err)
	}
	opts := spy.spawned[0]
	if opts.WorkingDir != live || opts.ResumeSessionID != "conv-1" || opts.SessionPolicy != "resume" || opts.Repo != "" {
		t.Fatalf("spawn = %+v; want the saved conversation resumed in its folder", opts)
	}
}

func TestAMacroOnAGoneFolderWithNoRepositoryDoesNotStart(t *testing.T) {
	svc, spy := newMacroStartFixture(t)
	ctx := context.Background()
	gone := filepath.Join(t.TempDir(), "gone")
	svc.folderExists = dirExists // the real check, on a folder that is not there
	spy.saved = &TaskResumeSession{ID: "conv-1", CWD: gone}
	if err := svc.Save(ctx, "Rebase and repush", macros.Macro{Keys: "Rebase.\n"}, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, MacroStart{Name: "Rebase and repush", TaskID: "abc12345"}); !errors.Is(err, ErrMacroFolderGone) {
		t.Fatalf("err = %v; want ErrMacroFolderGone", err)
	}
	if len(spy.spawned) != 0 {
		t.Fatalf("spawned %+v in a folder that is gone", spy.spawned)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatal("the folder was made")
	}
}

// The item goes by itself when the PR merges or closes: the loop announces
// the run's end, and Attention reconciles on that alone, with nothing else
// happening on the instance (review of LEG-614).
func TestAnIdleInstanceClearsTheItemWhenThePRMergesOrCloses(t *testing.T) {
	for _, end := range []string{PRStateMerged, PRStateClosed} {
		t.Run(end, func(t *testing.T) {
			f := newMergeWait(t, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bus := events.New()
			attention := NewAttentionService(f.db, bus)
			adapter := NewWorkflowAttentionAdapter(f.workflows)
			if err := attention.RegisterProducerWithEvents(adapter, WorkflowAttentionEvents...); err != nil {
				t.Fatal(err)
			}
			defer attention.StartEventReconciliation(ctx)()
			if err := attention.Reconcile(ctx, "workflows"); err != nil {
				t.Fatal(err)
			}
			open := func() int {
				items, err := attention.List(ctx, AttentionFilter{})
				if err != nil {
					t.Fatal(err)
				}
				n := 0
				for _, item := range items {
					if item.SignalKind == AttentionSignalWorkflowMergeWaiting && item.Status == "open" {
						n++
					}
				}
				return n
			}
			if open() != 1 {
				t.Fatal("no item while the PR is open")
			}
			stop := f.workflows.StartPRWaitLoop(ctx, bus, time.Hour)
			defer stop()
			ended := openPR()
			ended.State = end
			f.states.set(ended)
			bus.Publish(events.Event{Type: events.EventPRStatusUpdated, At: time.Now()})
			deadline := time.Now().Add(5 * time.Second)
			for open() != 0 {
				if time.Now().After(deadline) {
					t.Fatalf("the item is still open after the PR %s", end)
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}
