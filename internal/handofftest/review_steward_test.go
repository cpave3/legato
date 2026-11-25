package handofftest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The steward (spec LEG-559 § The steward; LEG-590): review-loop with
// steward on carries on after approval, watching the pull requests for its
// author agent.

var stewardInputs = map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true", "steward": "true"}

// stewarded runs review-loop with inputs to its first approval (pass 1
// clean, confirmed at GitHub's heads), so the steward watches. The board
// task board-1's agent is the author.
func (r reviewRun) stewarded(t *testing.T, inputs map[string]string) *store.WorkflowRunView {
	t.Helper()
	view, err := r.runs.StartReviewLoop(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if view.Run.Status != "running" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right."})
	r.tick()
	r.tick()
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "running" || !r.watching(t, view) {
		t.Fatalf("run %q (%s) is not watching: %s", view.Run.Status, view.Run.Error, stepStates(view))
	}
	return view
}

// watching says the steward's newest watch step waits.
func (r reviewRun) watching(t *testing.T, view *store.WorkflowRunView) bool {
	t.Helper()
	watches := stepsOf(view, "watch")
	return len(watches) > 0 && watches[len(watches)-1].Status == service.WorkflowStepWaiting
}

// stewardSteps are the steward's steps of an ID that ran, oldest first.
func stewardSteps(view *store.WorkflowRunView, id string) []store.WorkflowStep {
	var out []store.WorkflowStep
	for _, s := range stepsOf(view, id) {
		if strings.HasPrefix(s.Path, "steward/") && s.Status != "pending" {
			out = append(out, s)
		}
	}
	return out
}

func stepStates(view *store.WorkflowRunView) string {
	var states []string
	for _, s := range view.Steps {
		states = append(states, s.Path+"="+s.Status)
	}
	return strings.Join(states, " ")
}

// watchTick moves the clock past the steward's next watch, then ticks.
func (r reviewRun) watchTick() {
	r.worker.Clock.Advance(90 * time.Second)
	r.tick()
}

// record is the run's loop record.
func (r reviewRun) record(t *testing.T, view *store.WorkflowRunView) *store.ReviewLoop {
	t.Helper()
	rec, err := r.worker.Store.CurrentReviewLoop(context.Background(), view.Run.TaskID)
	if err != nil || rec == nil {
		t.Fatalf("loop of %s: %v", view.Run.TaskID, err)
	}
	return rec
}

func TestAFailingCheckAfterApprovalReachesTheAuthorOnceTaggedSteward(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.SetChecks("bbb222", failingGo)
	r.watchTick()
	r.worker.Clock.Advance(3 * time.Minute) // the quiet time
	r.tick()
	r.watchTick()
	r.watchTick()

	typed := r.worker.typedTo("legato-board-1")
	if n := strings.Count(typed, "[legato steward] 1 failing check"); n != 1 {
		t.Fatalf("the author was told %d times; typed %q", n, typed)
	}
	if strings.Contains(typed, "[legato review pass] 1 failing check") {
		t.Fatalf("told as a review pass: %q", typed)
	}
	feedback, err := service.ReviewFeedback(context.Background(), r.worker.Store, "board-1")
	if err != nil || len(feedback.Items) != 1 || feedback.Items[0].ID != "k-12-go" {
		t.Fatalf("feedback %+v, %v", feedback, err)
	}
	operation := feedback.Items[0].Operation
	for _, want := range []string{"git.push", "actions.write", "re-run"} {
		if !strings.Contains(operation, want) {
			t.Fatalf("the item names no %s: %q", want, operation)
		}
	}
	// Routing starts no pass, and the approval stands.
	if rec := r.record(t, r.show(t, view.Run.ID)); rec.Pass != 1 || rec.Reentries != 0 {
		t.Fatalf("loop at pass %d with %d re-entries", rec.Pass, rec.Reentries)
	}
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Phase != service.ReviewPhaseClean {
		t.Fatalf("loop %+v", loop)
	}
	if got := r.show(t, view.Run.ID); !r.watching(t, got) {
		t.Fatalf("not watching: %s", stepStates(got))
	}
}

// stewardStack is board-1's stack of three pull requests on acme/widget,
// #21 on main, #22 on #21, #23 on #22, board-1 tracking the top.
func stewardStack(t *testing.T) reviewRun {
	t.Helper()
	r := newStackRun(t)
	ctx := context.Background()
	if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "board-1", Title: "Greeting", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := r.worker.Store.SetCodeContext(ctx, "board-1", &store.CodeContext{Repo: "acme/widget", Branch: "stack/three", WorkingDir: dir, RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Agents.SpawnAgent(ctx, "board-1", 0, 0, service.AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: dir}); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	return r
}

func TestAPushReEntersReviewAndIsApprovedAgainAtTheNewHeads(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)
	task := view.Run.TaskID

	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.watchTick()
	r.tick()

	loop := r.loopPhase(t, task)
	if loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("after the push: loop %+v", loop)
	}
	rec := r.record(t, view)
	if rec.Reentries != 1 || rec.LastWake != "pushed" {
		t.Fatalf("re-entries %d, last wake %q", rec.Reentries, rec.LastWake)
	}
	spawns := r.worker.Tmux.Spawned()
	if brief := briefOf(t, spawns[len(spawns)-1]); !strings.Contains(brief, "review what changed since pass 1") {
		t.Fatalf("pass 2 is not incremental: %q", brief)
	}
	view = r.show(t, view.Run.ID)
	if watches := stewardSteps(view, "watch"); len(watches) != 1 || stepOutputs(t, watches[0])["woke"] != "pushed" {
		t.Fatalf("watch steps %+v", watches)
	}

	r.submit(t, task, service.ReviewSubmission{Summary: "Still right."})
	r.tick()
	r.tick()

	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 2 || posted[1].Event != service.ReviewApprove || posted[1].CommitID != "ccc333" {
		t.Fatalf("posted %+v; want a fresh APPROVE at ccc333", posted)
	}
	view = r.show(t, view.Run.ID)
	confirms := stewardSteps(view, "confirm")
	if len(confirms) != 1 || stepOutputs(t, confirms[0])["state"] != "approved" {
		t.Fatalf("confirm steps %+v", confirms)
	}
	if rec := r.record(t, view); rec.CleanPass != 2 || rec.Reentries != 1 {
		t.Fatalf("clean pass %d, re-entries %d", rec.CleanPass, rec.Reentries)
	}
	if view.Run.Status != "running" || !r.watching(t, view) {
		t.Fatalf("run %q not watching again: %s", view.Run.Status, stepStates(view))
	}
}

func TestARestackAcrossThreeLayersIsOneReEntry(t *testing.T) {
	r := stewardStack(t)
	view := r.stewarded(t, map[string]string{"repo": "acme/widget", "pr_number": "23", "agent_author": "true", "steward": "true"})
	task := view.Run.TaskID

	// The author fixes the bottom and restacks: every head moves, the
	// bottom first.
	r.worker.ReviewGitHub.Push(21, "sha21-b")
	r.watchTick()
	r.worker.ReviewGitHub.Push(22, "sha22-b")
	r.worker.ReviewGitHub.Push(23, "sha23-b")
	r.watchTick()
	r.watchTick()
	if pass := r.loopPhase(t, task).Pass; pass < 2 {
		t.Fatalf("no pass after the restack: pass %d", pass)
	}
	// Pass 2 reviews every head the restack moved.
	r.submit(t, task, service.ReviewSubmission{Summary: "All good."})
	r.tick()
	r.watchTick()
	if loop := r.loopPhase(t, task); loop.Phase == service.ReviewPhaseReviewing {
		r.submit(t, task, service.ReviewSubmission{Summary: "All good."})
		r.tick()
		r.watchTick()
	}

	view = r.show(t, view.Run.ID)
	rec := r.record(t, view)
	if rec.Reentries != 1 || rec.CleanPass != rec.Pass {
		t.Fatalf("re-entries %d, clean pass %d of %d: %s", rec.Reentries, rec.CleanPass, rec.Pass, stepStates(view))
	}
	layers, _ := r.worker.Store.ListReviewLoopPRs(context.Background(), rec.ID)
	for _, layer := range layers {
		if layer.CleanHead != fmt.Sprintf("sha%d-b", layer.Number) {
			t.Fatalf("#%d approved at %s", layer.Number, layer.CleanHead)
		}
	}
	if !r.watching(t, view) {
		t.Fatalf("not watching again: %s", stepStates(view))
	}
}

func TestAReplyWithoutAPushStartsNoPassAndADisputedBlockingFindingStartsOne(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	view := r.stewarded(t, stewardInputs)
	task := view.Run.TaskID

	// A member asks for a change after approval: the author is told, and
	// answers with a reply and no push.
	r.worker.ReviewGitHub.SetComments(12, service.PRComment{Kind: "issue", ID: 501, Author: "octocat", Association: "MEMBER", Body: "Should this greet in French too?"})
	r.watchTick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato steward] 1 new comment") {
		t.Fatalf("typed to the author: %q", typed)
	}
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", "c-c-501", service.AnswerReplied, "Not in this change.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.watchTick()
	r.watchTick()
	if loop := r.loopPhase(t, task); loop.Pass != 1 || loop.Phase != service.ReviewPhaseClean {
		t.Fatalf("a reply started a pass: loop %+v", loop)
	}
	if comments := r.worker.ReviewGitHub.Comments(); len(comments) != 1 || !strings.Contains(comments[0], "author: ") {
		t.Fatalf("comments posted %v; want the author's reply", comments)
	}

	// A push re-enters, and pass 2 finds a blocking problem, which the
	// author disputes without pushing: that starts pass 3.
	r.worker.ReviewGitHub.Push(12, "ccc333")
	r.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.watchTick()
	r.tick()
	result := r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	r.tick()
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato steward] Pass 2 has 1 blocking finding") {
		t.Fatalf("typed to the author: %q", typed)
	}
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", result.Findings[0], service.AnswerReplied, "An empty name is refused upstream.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()
	if loop := r.loopPhase(t, task); loop.Pass != 3 {
		t.Fatalf("the dispute started no pass: loop %+v", loop)
	}
	view = r.show(t, view.Run.ID)
	watches := stewardSteps(view, "watch")
	if len(watches) != 2 || stepOutputs(t, watches[1])["woke"] != "disputed" {
		t.Fatalf("watch steps: %s", stepStates(view))
	}
	if rec := r.record(t, view); rec.Reentries != 1 {
		t.Fatalf("re-entries %d; a dispute is not one", rec.Reentries)
	}
}

func TestAPullRequestClosedUnmergedWhileWatchedEndsTheLoopClosed(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view := r.stewarded(t, stewardInputs)

	r.worker.ReviewGitHub.ClosePR(12)
	r.watchTick()
	r.tick()

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || runOutputs(t, view)["outcome"] != "closed" {
		t.Fatalf("run %q (%s), outputs %s: %s", view.Run.Status, view.Run.Error, view.Run.OutputsJSON, stepStates(view))
	}
	if watches := stewardSteps(view, "watch"); len(watches) != 1 || stepOutputs(t, watches[0])["woke"] != "closed" {
		t.Fatalf("watch steps: %s", stepStates(view))
	}
	if held, _ := r.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", 12); held != nil {
		t.Fatalf("the closed loop still holds #12: %s", held.ID)
	}
}

// A workflow file's own watch needs a loop opened with steward.
func TestAWatchOnALoopWithoutAStewardIsRefused(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.startFile(t, `schema_version: 2
name: watch-it
title: Watch it
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with: {repo: acme/widget, pr_number: 12}
  - id: watch
    title: Watch
    type: github.pr.review.wait
    with: {loop: "${{ steps.open.outputs.loop_id }}", mode: watch}
`, nil)
	r.tick()
	view = r.show(t, view.Run.ID)
	if watch := stepOf(t, view, "watch"); watch.Status != "failed" || watch.ErrorCode != service.ReviewRefusalInvalidSettings || !strings.Contains(watch.Error, "steward") {
		t.Fatalf("watch %q (%s): %s", watch.Status, watch.ErrorCode, watch.Error)
	}
}
