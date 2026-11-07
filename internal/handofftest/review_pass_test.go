package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reviewed is a worker with review passes and one make-a-change handoff
// whose author has opened a pull request from its branch.
type reviewed struct {
	worker *Worker
	card   string
	branch string
}

func newReviewed(t *testing.T, opts ReviewPassOptions) reviewed {
	t.Helper()
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &opts})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID,
		Purpose: service.HandoffMakeChange, Repo: "acme/widget", AgentKind: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	branch := "legato/" + strings.ToLower(taskID)
	worker.Checkout.Set(worker.Tmux.Spawned()[0].WorkDir, branch, "aaa111")
	worker.Checkout.File("login.go", 40)
	worker.ReviewGitHub.OpenPR(7, branch, "aaa111")
	return reviewed{worker: worker, card: away.WorkerTaskID, branch: branch}
}

// finish moves the card to Done, as the author does, and lets the loop see.
func (r reviewed) finish(t *testing.T) {
	t.Helper()
	if err := r.worker.Board.MoveCard(context.Background(), r.card, "Done"); err != nil {
		t.Fatal(err)
	}
	r.tick()
}

func (r reviewed) tick() {
	r.worker.Reviews.Tick(context.Background())
	r.worker.Settle()
}

func (r reviewed) loop(t *testing.T) store.HandoffReview {
	t.Helper()
	loop, err := r.worker.Store.GetHandoffReview(context.Background(), r.card)
	if err != nil || loop == nil {
		t.Fatalf("review loop = %+v, %v", loop, err)
	}
	return *loop
}

func (r reviewed) column(t *testing.T) string {
	t.Helper()
	task, err := r.worker.Store.GetTask(context.Background(), r.card)
	if err != nil {
		t.Fatal(err)
	}
	return task.Status
}

func (r reviewed) reviewer(t *testing.T) *store.AgentSession {
	t.Helper()
	session, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), r.card, "reviewer")
	if err != nil {
		t.Fatalf("no reviewer runs: %v", err)
	}
	return session
}

func (r reviewed) submit(t *testing.T, sub service.ReviewSubmission) (service.ReviewSubmitResult, error) {
	t.Helper()
	result, err := r.worker.Reviews.Submit(context.Background(), r.card, r.reviewer(t).ExecutionID, sub)
	r.worker.Settle()
	return result, err
}

func (r reviewed) handoffState(t *testing.T) string {
	t.Helper()
	if err := r.worker.Handoffs.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	h, err := r.worker.Store.OpenHandoffForTask(context.Background(), r.card)
	if err != nil {
		t.Fatal(err)
	}
	return h.State
}

func TestAFinishedAuthorsPullRequestsAreReviewedAndACleanPassApprovesThem(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})

	r.finish(t)

	if r.loop(t).Phase != service.ReviewPhaseReviewing || r.column(t) != "Review" || r.handoffState(t) != store.HandoffInProgress {
		t.Fatalf("phase %q, column %q, handoff %q", r.loop(t).Phase, r.column(t), r.handoffState(t))
	}
	if r.worker.Checkout.Pins()["refs/legato/review/"+r.card+"/1/7"] != "aaa111" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}
	reviewer := r.reviewer(t)
	launch := r.worker.typedTo(reviewer.TmuxSession)
	if reviewer.TmuxSession != "legato-"+r.card+"_reviewer" || reviewer.AgentKind != "codex" || !reviewer.Sandboxed || !strings.Contains(launch, "--codex-prompts-off") {
		t.Fatalf("reviewer %+v, launch %q", reviewer, launch)
	}
	if readOnly, _ := r.worker.Store.ExecutionReadOnly(context.Background(), reviewer.ExecutionID); !readOnly {
		t.Fatal("the reviewer's execution may write")
	}

	result, err := r.submit(t, service.ReviewSubmission{Summary: "Solid fix.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "nit", Title: "Name the retry", Body: "Call it maxRetries.", Place: &service.ReviewPlace{Path: "login.go", Line: 12}},
	}})
	if err != nil || result.Verdict != service.VerdictClean {
		t.Fatalf("submit = %+v, %v", result, err)
	}

	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].Event != service.ReviewApprove || posted[0].CommitID != "aaa111" || posted[0].Number != 7 ||
		len(posted[0].Findings) != 1 || !strings.Contains(posted[0].Footer, "codex (gpt-5.5)") || !strings.Contains(posted[0].Footer, "3-tier-review") {
		t.Fatalf("posted = %+v", posted)
	}
	if r.loop(t).Phase != service.ReviewPhaseClean || r.column(t) != "Done" || r.handoffState(t) != store.HandoffDone {
		t.Fatalf("after a clean pass: phase %q, column %q", r.loop(t).Phase, r.column(t))
	}
	if _, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), r.card, "reviewer"); err == nil {
		t.Fatal("the reviewer still runs after posting")
	}
}

func TestAPassWithChangesIsAComment(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)

	result, err := r.submit(t, service.ReviewSubmission{Summary: "One bug.", Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "blocking", Title: "Retries never stop", Body: "The loop has no bound.", Place: &service.ReviewPlace{Path: "login.go", Line: 20}},
	}})
	if err != nil || result.Verdict != service.VerdictChanges {
		t.Fatalf("submit = %+v, %v", result, err)
	}
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].Event != service.ReviewComment {
		t.Fatalf("posted = %+v", posted)
	}
	if r.loop(t).Phase != service.ReviewPhaseWaitingForAuthor || r.column(t) != "Doing" || r.handoffState(t) != store.HandoffInProgress {
		t.Fatalf("phase %q, column %q", r.loop(t).Phase, r.column(t))
	}
	findings, _ := r.worker.Store.ListHandoffReviewFindings(context.Background(), r.card)
	if len(findings) != 1 || findings[0].CommentID == 0 || findings[0].ID != result.Findings[0] {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestADirtyCheckoutIsToldOnceThenTheReviewIsStuck(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.Checkout.Dirty(true)

	r.finish(t)

	if typed := r.worker.typedTo("legato-" + r.card); !strings.Contains(typed, "[legato review pass] Push or discard before review") {
		t.Fatalf("typed to the author: %q", typed)
	}
	if r.loop(t).Phase != service.ReviewPhaseReady || len(r.worker.ReviewGitHub.Posted()) != 0 {
		t.Fatalf("phase %q", r.loop(t).Phase)
	}
	// The author did nothing about it.
	r.worker.Clock.Advance(11 * time.Minute)
	r.tick()

	loop := r.loop(t)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "not committed") {
		t.Fatalf("loop = %+v", loop)
	}
}

func TestAResultWhoseCheckoutMovedIsDiscarded(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	r.worker.Checkout.Commit(r.branch, "bbb222")

	result, err := r.submit(t, service.ReviewSubmission{Summary: "Fine."})

	if err != nil || !strings.Contains(result.Message, "HEAD moved") {
		t.Fatalf("submit = %+v, %v", result, err)
	}
	if len(r.worker.ReviewGitHub.Posted()) != 0 || r.loop(t).Phase != service.ReviewPhaseReady {
		t.Fatalf("posted %d, phase %q", len(r.worker.ReviewGitHub.Posted()), r.loop(t).Phase)
	}
	passes, _ := r.worker.Store.ListHandoffReviewPasses(context.Background(), r.card)
	if len(passes) != 1 || passes[0].State != service.ReviewPassDiscarded {
		t.Fatalf("passes = %+v", passes)
	}
}

func TestAReviewerPastItsTimeIsUnableAndTheReviewStuck(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)

	r.worker.Clock.Advance(46 * time.Minute)
	r.tick()

	loop := r.loop(t)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "ran past 45m") || len(r.worker.ReviewGitHub.Posted()) != 0 {
		t.Fatalf("loop = %+v", loop)
	}
	if _, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), r.card, "reviewer"); err == nil {
		t.Fatal("the reviewer still runs")
	}
}

func TestAMalformedSubmissionIsRefusedAndThePassGoesOn(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)

	_, err := r.submit(t, service.ReviewSubmission{Findings: []service.SubmittedFinding{
		{PR: 7, Severity: "major", Title: "x", Body: "y"},
		{PR: 9, Severity: "nit", Title: "x", Body: "y"},
		{PR: 7, Severity: "nit", Title: "x", Body: "y", Place: &service.ReviewPlace{Path: "missing.go", Line: 1}},
		{PR: 7, Severity: "nit", Title: "x", Body: "y", Place: &service.ReviewPlace{Path: "login.go", Line: 99}},
	}})

	var refused *service.ReviewSubmissionError
	if !errors.As(err, &refused) || len(refused.Problems) != 5 {
		t.Fatalf("err = %v", err)
	}
	if r.loop(t).Phase != service.ReviewPhaseReviewing || len(r.worker.ReviewGitHub.Posted()) != 0 {
		t.Fatalf("phase %q", r.loop(t).Phase)
	}
	// Only the card's reviewer may submit.
	if _, err := r.worker.Reviews.Submit(context.Background(), r.card, "ex-someone-else", service.ReviewSubmission{Summary: "x"}); !errors.Is(err, service.ErrNotTheReviewer) {
		t.Fatalf("a submission from another session: %v", err)
	}
}

func TestPassesBeyondTheCapacityWaitForAReviewer(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{MaxConcurrent: 1})
	other := r.worker.Store
	// A second reviewing card takes the only reviewer.
	if err := r.worker.Store.CreateTask(context.Background(), store.Task{ID: "busy", Title: "busy", Status: "Review"}); err != nil {
		t.Fatal(err)
	}
	if err := other.SaveHandoffReview(context.Background(), store.HandoffReview{TaskID: "busy", Phase: service.ReviewPhaseReviewing, UpdatedAt: "x"}); err != nil {
		t.Fatal(err)
	}

	r.finish(t)

	if r.loop(t).Phase != service.ReviewPhaseWaitingForReviewer || r.loop(t).QueuedAt == "" {
		t.Fatalf("phase %q", r.loop(t).Phase)
	}
	// The other reviewer finishes: this pass starts.
	if err := other.SaveHandoffReview(context.Background(), store.HandoffReview{TaskID: "busy", Phase: service.ReviewPhaseClean, UpdatedAt: "x"}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if r.loop(t).Phase != service.ReviewPhaseReviewing {
		t.Fatalf("phase %q after a reviewer freed up", r.loop(t).Phase)
	}
}
