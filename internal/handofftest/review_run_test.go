package handofftest

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reviewRun is a worker with review passes and workflows, no main and no
// handoff, and an open pull request #12 of acme/widget.
type reviewRun struct {
	worker *Worker
	runs   *ReviewRuns
}

func newReviewRun(t *testing.T, opts ReviewPassOptions) reviewRun {
	t.Helper()
	worker := StartWorker(t, WorkerOptions{Name: "erebus", GitHubRepositories: []string{"acme/*"}, ReviewPasses: &opts})
	worker.ReviewGitHub.OpenPR(12, "feature/greeting", "bbb222")
	worker.Checkout.File("greet.go", 30)
	return reviewRun{worker: worker, runs: StartReviewRuns(t, worker)}
}

func (r reviewRun) start(t *testing.T, inputs map[string]string) *store.WorkflowRunView {
	t.Helper()
	view, err := r.runs.Start(context.Background(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (r reviewRun) tick() {
	r.worker.Reviews.Tick(context.Background())
	r.worker.Settle()
}

func (r reviewRun) show(t *testing.T, runID string) *store.WorkflowRunView {
	t.Helper()
	view, err := r.runs.Workflows.Show(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (r reviewRun) submit(t *testing.T, taskID string, sub service.ReviewSubmission) service.ReviewSubmitResult {
	t.Helper()
	reviewer, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), taskID, "reviewer")
	if err != nil {
		t.Fatalf("no reviewer runs: %v", err)
	}
	result, err := r.worker.Reviews.Submit(context.Background(), taskID, reviewer.ExecutionID, sub)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	r.worker.Settle()
	return result
}

func outputs(t *testing.T, view *store.WorkflowRunView, step string) map[string]any {
	t.Helper()
	for _, s := range view.Steps {
		if s.StepID == step {
			var values map[string]any
			if err := json.Unmarshal([]byte(s.OutputsJSON), &values); err != nil {
				t.Fatal(err)
			}
			return values
		}
	}
	t.Fatalf("no step %s", step)
	return nil
}

var pr12 = map[string]string{"repo": "acme/widget", "pr_number": "12"}

func TestAReviewRunPostsPassOneAsTheReviewerAppAndWaitsForTheAuthor(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	if view.Run.Status != "running" || view.Steps[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("run %q, step %q: %s", view.Run.Status, view.Steps[0].Status, view.Run.Error)
	}
	task := view.Run.TaskID

	r.tick()
	reviewer, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), task, "reviewer")
	if err != nil || reviewer.TmuxSession != "legato-"+task+"_reviewer" {
		t.Fatalf("reviewer = %+v, %v", reviewer, err)
	}
	if r.worker.Checkout.Pins()["refs/legato/review/"+task+"/1/12"] != "bbb222" {
		t.Fatalf("pins = %v", r.worker.Checkout.Pins())
	}

	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{
		{PR: 12, Severity: "blocking", Title: "Greets nobody", Body: "An empty name prints a bare comma.", FailureScenario: "greet(\"\") prints \"Hello, \".",
			Place: &service.ReviewPlace{Path: "greet.go", Line: 4}},
	}})

	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].Event != service.ReviewComment || posted[0].Number != 12 || posted[0].CommitID != "bbb222" || len(posted[0].Findings) != 1 {
		t.Fatalf("posted = %+v", posted)
	}
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "running" || view.Steps[0].Status != service.WorkflowStepWaiting {
		t.Fatalf("after pass 1: run %q, step %q", view.Run.Status, view.Steps[0].Status)
	}
	if loop, _ := r.worker.Store.GetHandoffReview(context.Background(), task); loop == nil || loop.Phase != service.ReviewPhaseWaitingForAuthor {
		t.Fatalf("loop = %+v", loop)
	}
	r.tick()
	if len(r.worker.ReviewGitHub.Posted()) != 1 || r.show(t, view.Run.ID).Run.Status != "running" {
		t.Fatal("the loop moved on with nothing from the author")
	}
}

func TestACleanPassApprovesAndCompletesTheRunWithItsOutputs(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	r.submit(t, task, service.ReviewSubmission{Summary: "Looks right.", Findings: []service.SubmittedFinding{
		{PR: 12, Severity: "nit", Title: "Name it", Body: "Call it name.", Place: &service.ReviewPlace{Path: "greet.go", Line: 2}},
	}})

	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || posted[0].Event != service.ReviewApprove {
		t.Fatalf("posted = %+v", posted)
	}
	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}
	out := outputs(t, view, "review")
	if out["outcome"] != "approved" || out["passes"] != float64(1) || out["findings"] != float64(1) || out["linked_task"] != "" {
		t.Fatalf("outputs = %+v", out)
	}
	reviews, _ := out["reviews"].([]any)
	if len(reviews) != 1 || !strings.Contains(reviews[0].(map[string]any)["url"].(string), "github.com/review/1") {
		t.Fatalf("reviews = %+v", out["reviews"])
	}
	summary, err := os.ReadFile(out["summary_path"].(string))
	if err != nil || !strings.Contains(string(summary), "acme/widget#12: approved after 1 pass") {
		t.Fatalf("summary = %q, %v", summary, err)
	}
	if note := outputs(t, view, "note"); note["skipped"] != true {
		t.Fatalf("note = %+v", note)
	}
	if len(r.runs.Clones.removed) != 1 {
		t.Fatalf("the clone was not removed: %v", r.runs.Clones.removed)
	}
}

func TestTheOutcomeIsNotedOnTheBoardTaskThePullRequestBelongsTo(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "board-1", Title: "Greeting", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Store.SetCodeContext(ctx, "board-1", &store.CodeContext{Repo: "acme/widget", Branch: "feature/greeting", RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}
	view := r.start(t, pr12)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Fine."})

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || outputs(t, view, "review")["linked_task"] != "board-1" {
		t.Fatalf("run %q, outputs %+v", view.Run.Status, outputs(t, view, "review"))
	}
	notes, err := r.worker.Store.ListTaskNotes(ctx, "board-1")
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0].Body, "approved after 1 pass") {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
}

func TestAReviewRunIsRefusedAtStartWhenItCannotReview(t *testing.T) {
	t.Run("review passes off", func(t *testing.T) {
		r := newReviewRun(t, ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
			o.Config = func() service.ReviewPassConfig { return service.ReviewPassConfig{} }
			return o
		}})
		refused(t, r.start(t, pr12), "review passes are off")
	})
	t.Run("the reviewer App cannot read the repository", func(t *testing.T) {
		r := newReviewRun(t, ReviewPassOptions{})
		r.worker.ReviewGitHub.NotInstalled("acme/widget")
		view := r.start(t, pr12)
		refused(t, view, "not installed on the account")
		refused(t, view, "not among the repositories the installation selects")
		refused(t, view, "no such repository")
		refused(t, view, "review-pr workflow")
	})
	t.Run("the pull request is closed", func(t *testing.T) {
		r := newReviewRun(t, ReviewPassOptions{})
		r.worker.ReviewGitHub.ClosePR(12)
		refused(t, r.start(t, pr12), "is closed: a review loop needs an open pull request")
	})
	t.Run("an agent author for a pull request no board task links", func(t *testing.T) {
		r := newReviewRun(t, ReviewPassOptions{})
		refused(t, r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true"}), "agent_author needs a board task")
	})
}

func refused(t *testing.T, view *store.WorkflowRunView, want string) {
	t.Helper()
	if view.Run.Status != "failed" || !strings.Contains(view.Steps[0].Error, want) {
		t.Fatalf("run %q, step error %q; want %q", view.Run.Status, view.Steps[0].Error, want)
	}
	if len(view.Attempts) != 1 {
		t.Fatalf("attempts = %+v", view.Attempts)
	}
}

func TestCancellingAReviewRunEndsItsLoopAndRemovesItsClone(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()

	view, err := r.runs.Workflows.Cancel(ctx, view.Run.ID, "not needed")
	if err != nil || view.Run.Status != "cancelled" {
		t.Fatalf("cancel = %+v, %v", view, err)
	}
	r.worker.Settle()
	if _, err := r.worker.Store.GetAgentSessionInSlot(ctx, task, "reviewer"); err == nil {
		t.Fatal("the reviewer still runs")
	}
	if loop, _ := r.worker.Store.GetHandoffReview(ctx, task); loop == nil || loop.Phase != service.ReviewPhaseEnded || loop.Reason != service.ReviewOutcomeCancelled {
		t.Fatalf("loop = %+v", loop)
	}
	if len(r.runs.Clones.removed) != 1 {
		t.Fatalf("removed = %v", r.runs.Clones.removed)
	}
	r.tick()
	if len(r.worker.ReviewGitHub.Posted()) != 0 {
		t.Fatal("a cancelled loop posted")
	}
}

func TestTheRunSaysWhereItsReviewLoopStands(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	details := func() service.ReviewRunDetails {
		t.Helper()
		got, ok := r.show(t, view.Run.ID).Details["review"].(service.ReviewRunDetails)
		if !ok {
			t.Fatalf("details = %+v", r.show(t, view.Run.ID).Details)
		}
		return got
	}
	if d := details(); d.Phase != service.ReviewPhaseReviewing || d.Summary != "Reviewing · pass 1" {
		t.Fatalf("reviewing: %+v", d)
	}
	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{
		{PR: 12, Severity: "blocking", Title: "Greets nobody", Body: "An empty name.", FailureScenario: "greet(\"\") prints a bare comma.", Place: &service.ReviewPlace{Path: "greet.go", Line: 4}},
	}})
	d := details()
	if d.Phase != service.ReviewPhaseWaitingForAuthor || d.Summary != "Waiting for author · 1 blocking" || len(d.Reviews) != 1 || d.Reviews[0].URL != "https://github.com/review/1" {
		t.Fatalf("waiting: %+v", d)
	}
}
