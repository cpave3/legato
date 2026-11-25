package handofftest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// prompted is a worker with review passes and review runs, a local card with
// two open pull requests (a stack, #31 then #32), and the review prompt.
type prompted struct {
	r       reviewRun
	adapter *service.ReviewPromptAttentionAdapter
	card    string
}

func newPrompted(t *testing.T, reviewsOn bool) prompted {
	t.Helper()
	return newPromptedWith(t, reviewsOn, nil)
}

// beforeStacks is the review-pr-passes workflow as it was added before
// stacks: no scope input.
func beforeStacks(t *testing.T) []byte {
	t.Helper()
	example, _ := workflowengine.ExampleNamed("review-pr-passes")
	var kept []string
	lines := strings.Split(string(example.YAML), "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "scope:" {
			i += 3 // its type, default and description
			continue
		}
		if strings.Contains(lines[i], "scope: ${{") {
			continue
		}
		kept = append(kept, lines[i])
	}
	out := strings.Join(kept, "\n")
	if strings.Contains(out, "scope") && strings.Contains(out, "inputs.scope") {
		t.Fatal("the scope input is still there")
	}
	return []byte(out)
}

// newPromptedWith is newPrompted with a review-pr-passes file beside the
// built-ins when workflowYAML is given: an operator's old copy.
func newPromptedWith(t *testing.T, reviewsOn bool, workflowYAML []byte) prompted {
	t.Helper()
	ctx := context.Background()
	r := newReviewRun(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.OpenStackedPR(31, "local/one", "main", "sha31")
	r.worker.ReviewGitHub.OpenStackedPR(32, "local/two", "local/one", "sha32")
	db := r.worker.Store
	if err := db.CreateTask(ctx, store.Task{ID: "local-1", Title: "Local work", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TrackTaskBranches(ctx, "local-1", []store.BranchRef{{Repo: "acme/widget", Branch: "local/one"}, {Repo: "acme/widget", Branch: "local/two"}}, service.BranchSourceStack); err != nil {
		t.Fatal(err)
	}
	setHead(t, db, "local/one", 31, "sha31")
	setHead(t, db, "local/two", 32, "sha32")
	// The agent that did the work: the author.
	if err := db.SetCodeContext(ctx, "local-1", &store.CodeContext{Repo: "acme/widget", Branch: "local/two", WorkingDir: t.TempDir(), RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if workflowYAML != nil {
		if err := os.WriteFile(filepath.Join(dir, "review-pr-passes.workflow.yaml"), workflowYAML, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Legato's discovery: the files, then the built-ins, review-loop
	// among them.
	launcher := service.NewTaskWorkflowLauncher(db, r.runs.Workflows, func() workflowengine.DiscoveryResult {
		return workflowengine.WithBuiltIns(workflowengine.DiscoverPaths(dir), service.BuiltInWorkflows()...)
	})
	adapter := service.NewReviewPromptAttentionAdapter(db, launcher)
	if reviewsOn {
		adapter.SetReviews(r.worker.Reviews)
	}
	return prompted{r: r, adapter: adapter, card: "local-1"}
}

func setHead(t *testing.T, db *store.Store, branch string, number int, head string) {
	t.Helper()
	if err := db.SetTaskBranchPR(context.Background(), "local-1", "acme/widget", branch, &store.PRMeta{Repo: "acme/widget", Branch: branch, PRNumber: number, State: "OPEN", Head: head}); err != nil {
		t.Fatal(err)
	}
}

func (p prompted) items(t *testing.T) []service.AttentionInput {
	t.Helper()
	items, err := p.adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func (p prompted) move(t *testing.T, column string) {
	t.Helper()
	if err := p.r.worker.Board.MoveCard(context.Background(), p.card, column); err != nil {
		t.Fatal(err)
	}
}

func TestALocalCardInDoneWithOpenPRsPromptsOneStackReview(t *testing.T) {
	p := newPrompted(t, true)
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("a card in Doing raised %+v", items)
	}

	p.move(t, "Done")
	items := p.items(t)
	if len(items) != 1 || items[0].TaskID != p.card || items[0].Title != "local-1 has 2 open PRs not reviewed" || items[0].Actions[0].ID != "start_review" {
		t.Fatalf("items %+v", items)
	}

	if err := p.adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: p.card, ActionID: "start_review"}); err != nil {
		t.Fatal(err)
	}
	// The loop it started clears the item at once, before it opens.
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("after starting: %+v", items)
	}
	p.r.runs.Open(context.Background())
	runs, err := p.r.worker.Store.ListWorkflowRuns(context.Background(), store.WorkflowRunFilter{WorkflowName: "review-loop"})
	if err != nil || len(runs) != 1 || runs[0].Status != "running" {
		t.Fatalf("runs %+v, %v", runs, err)
	}
	// One loop record holds the whole stack, the card's agent its author.
	loop := p.loopOn(t, 31)
	if !strings.HasPrefix(loop.ID, "rl-") || loop.RunID != runs[0].ID || loop.Scope != service.ReviewScopeStack || !loop.AgentAuthor || loop.AuthorTask != p.card {
		t.Fatalf("loop %+v", loop)
	}
	if other := p.loopOn(t, 32); other.ID != loop.ID {
		t.Fatalf("#32 is held by %s, not %s", other.ID, loop.ID)
	}
	// The loop it started clears the item.
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("after starting: %+v", items)
	}
}

// loopOn is the loop that holds a pull request of the card's repository.
func (p prompted) loopOn(t *testing.T, number int) *store.ReviewLoop {
	t.Helper()
	loop, err := p.r.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", number)
	if err != nil || loop == nil {
		t.Fatalf("no loop holds #%d: %v", number, err)
	}
	return loop
}

func TestAnActiveLoopOrAHandoffCardRaisesNothing(t *testing.T) {
	p := newPrompted(t, true)
	p.move(t, "Review")
	p.r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "32", "scope": "pr"})
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("with a loop on #32: %+v", items)
	}
}

func TestWithReviewPassesOffTheItemSaysSoAndOffersNothing(t *testing.T) {
	p := newPrompted(t, false)
	p.move(t, "Done")
	items := p.items(t)
	if len(items) != 1 || !strings.Contains(items[0].Detail, "Review passes are off") {
		t.Fatalf("items %+v", items)
	}
	for _, a := range items[0].Actions {
		if a.ID == "start_review" {
			t.Fatal("it offers to start review passes")
		}
	}
}

func TestAPushChangesTheItemsRevision(t *testing.T) {
	p := newPrompted(t, true)
	p.move(t, "Done")
	before := p.items(t)[0].SourceRevision

	setHead(t, p.r.worker.Store, "local/two", 32, "sha32-b")

	if after := p.items(t)[0].SourceRevision; after == before {
		t.Fatalf("revision %q after a push", after)
	}
}

func TestAnAwayCardRaisesNothing(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "Away work")
	db := main.Store
	if _, err := db.TrackTaskBranches(ctx, taskID, []store.BranchRef{{Repo: "acme/widget", Branch: "away/one"}}, service.BranchSourceAPI); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskBranchPR(ctx, taskID, "acme/widget", "away/one", &store.PRMeta{Repo: "acme/widget", PRNumber: 41, State: "OPEN", Head: "sha41"}); err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.Status = "Done"
	if err := db.UpdateTask(ctx, *task); err != nil {
		t.Fatal(err)
	}

	items, err := service.NewReviewPromptAttentionAdapter(db, nil).Snapshot(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("an away card raised %+v, %v", items, err)
	}
}

// review-loop is built in, so the item never asks for a workflow to be
// added, and an old review-pr-passes copy is not what it starts (LEG-597).
func TestTheItemStartsTheBuiltInReviewLoopNeverAnAddedCopy(t *testing.T) {
	example, _ := workflowengine.ExampleNamed("review-pr-passes")
	for name, yaml := range map[string][]byte{"no copy": nil, "an old copy": example.YAML, "a copy from before stacks": beforeStacks(t)} {
		t.Run(name, func(t *testing.T) {
			p := newPromptedWith(t, true, yaml)
			p.move(t, "Done")
			items := p.items(t)
			if len(items) != 1 || items[0].Actions[0].ID != "start_review" || strings.Contains(items[0].Detail, "workflow add") {
				t.Fatalf("items %+v", items)
			}
			if err := p.adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: p.card, ActionID: "start_review"}); err != nil {
				t.Fatal(err)
			}
			runs, err := p.r.worker.Store.ListWorkflowRuns(context.Background(), store.WorkflowRunFilter{})
			if err != nil || len(runs) != 1 || runs[0].WorkflowName != "review-loop" {
				t.Fatalf("runs %+v, %v", runs, err)
			}
		})
	}
}

// A loop that took one of the card's pull requests after the item was raised
// is opened instead of a second (AC 2): the action refuses, naming where the
// loop is, and starts nothing.
func TestOnAPRALoopHoldsTheActionOpensTheHolder(t *testing.T) {
	p := newPrompted(t, true)
	p.move(t, "Done")
	if items := p.items(t); len(items) != 1 {
		t.Fatalf("items %+v", items)
	}
	holder := p.r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "32", "scope": "pr"})

	err := p.adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: p.card, ActionID: "start_review"})
	var elsewhere *service.AttentionElsewhereError
	if !errors.As(err, &elsewhere) || elsewhere.Href != "/workflows/runs/"+holder.Run.ID || !strings.Contains(err.Error(), "review_loop_active") {
		t.Fatalf("err = %v", err)
	}
	runs, _ := p.r.worker.Store.ListWorkflowRuns(context.Background(), store.WorkflowRunFilter{WorkflowName: "review-loop"})
	if len(runs) != 0 {
		t.Fatalf("it started %+v", runs)
	}
}

// The card's agent is the author only of pull requests it opened through
// Legato, as Legato's author App (D8): a branch the operator linked by hand,
// or a pull request a person opened, is reviewed as a person's.
func TestTheAuthorIsTheCardsAgentOnlyForPRsLegatosAuthorAppOpened(t *testing.T) {
	ctx := context.Background()
	t.Run("linked by hand", func(t *testing.T) {
		p := newPrompted(t, true)
		db := p.r.worker.Store
		if err := db.CreateTask(ctx, store.Task{ID: "local-2", Title: "Linked", Status: "Done"}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.TrackTaskBranches(ctx, "local-2", []store.BranchRef{{Repo: "acme/widget", Branch: "local/one"}}, service.BranchSourceManual); err != nil {
			t.Fatal(err)
		}
		if err := db.SetTaskBranchPR(ctx, "local-2", "acme/widget", "local/one", &store.PRMeta{Repo: "acme/widget", Branch: "local/one", PRNumber: 31, State: "OPEN", Head: "sha31"}); err != nil {
			t.Fatal(err)
		}
		if err := p.adapter.Execute(ctx, service.AttentionActionRequest{SourceKey: "local-2", ActionID: "start_review"}); err != nil {
			t.Fatal(err)
		}
		p.r.runs.Open(ctx)
		if loop := p.loopOn(t, 31); loop.AgentAuthor || loop.AuthorTask != "" {
			t.Fatalf("loop %+v", loop)
		}
	})
	t.Run("opened by a person", func(t *testing.T) {
		p := newPrompted(t, true)
		db := p.r.worker.Store
		for _, pr := range []struct {
			branch string
			number int
		}{{"local/one", 31}, {"local/two", 32}} {
			if err := db.SetTaskBranchPR(ctx, p.card, "acme/widget", pr.branch, &store.PRMeta{Repo: "acme/widget", Branch: pr.branch, PRNumber: pr.number, State: "OPEN", Head: fmt.Sprintf("sha%d", pr.number), Author: "alice"}); err != nil {
				t.Fatal(err)
			}
		}
		p.move(t, "Done")
		if err := p.adapter.Execute(ctx, service.AttentionActionRequest{SourceKey: p.card, ActionID: "start_review"}); err != nil {
			t.Fatal(err)
		}
		p.r.runs.Open(ctx)
		if loop := p.loopOn(t, 31); loop.AgentAuthor {
			t.Fatalf("loop %+v", loop)
		}
	})
	// openedBy starts a review of the card's two pull requests, which
	// author opened; Legato knows its author App's login (it learns it
	// when the App posts).
	openedBy := func(t *testing.T, author string) *store.ReviewLoop {
		t.Helper()
		p := newPrompted(t, true)
		db := p.r.worker.Store
		if err := db.RecordGitHubAppLogin(ctx, "legato-author[bot]"); err != nil {
			t.Fatal(err)
		}
		for _, pr := range []struct {
			branch string
			number int
		}{{"local/one", 31}, {"local/two", 32}} {
			if err := db.SetTaskBranchPR(ctx, p.card, "acme/widget", pr.branch, &store.PRMeta{Repo: "acme/widget", Branch: pr.branch, PRNumber: pr.number, State: "OPEN", Head: fmt.Sprintf("sha%d", pr.number), Author: author}); err != nil {
				t.Fatal(err)
			}
		}
		p.move(t, "Done")
		if err := p.adapter.Execute(ctx, service.AttentionActionRequest{SourceKey: p.card, ActionID: "start_review"}); err != nil {
			t.Fatal(err)
		}
		p.r.runs.Open(ctx)
		loop := p.loopOn(t, 31)
		if loop.AgentAuthor && loop.AuthorTask != p.card {
			t.Fatalf("loop %+v", loop)
		}
		return loop
	}
	t.Run("opened by the author App", func(t *testing.T) {
		if loop := openedBy(t, "legato-author[bot]"); !loop.AgentAuthor {
			t.Fatalf("loop %+v", loop)
		}
	})
	t.Run("opened by another App", func(t *testing.T) {
		if loop := openedBy(t, "dependabot[bot]"); loop.AgentAuthor {
			t.Fatalf("loop %+v", loop)
		}
	})
}

// A workflow run's loop that approved the card's pull requests counts as
// their review, though it is kept on the run's own task (LEG-613); a push
// after the approval asks again.
func TestARunLoopThatApprovedTheCardsPRsRaisesNothing(t *testing.T) {
	p := newPrompted(t, true)
	p.move(t, "Done")
	view := p.r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "31"})
	p.r.tick()
	p.r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "All good."})
	if shown := p.r.show(t, view.Run.ID); shown.Run.Status != "succeeded" {
		t.Fatalf("run %q", shown.Run.Status)
	}
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("after an approving run: %+v", items)
	}

	setHead(t, p.r.worker.Store, "local/two", 32, "sha32-b")
	items := p.items(t)
	if len(items) != 1 || items[0].Title != "local-1 has 1 open PR not reviewed" {
		t.Fatalf("after a push past the approval: %+v", items)
	}
}

// approve runs a loop on one pull request of the card's stack alone and
// has its one pass approve it.
func (p prompted) approve(t *testing.T, number int) {
	t.Helper()
	view := p.r.start(t, map[string]string{"repo": "acme/widget", "pr_number": fmt.Sprint(number), "scope": "pr"})
	p.r.tick()
	p.r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "All good."})
	if shown := p.r.show(t, view.Run.ID); shown.Run.Status != "succeeded" {
		t.Fatalf("run %q", shown.Run.Status)
	}
}

// What counts is Legato's latest review at the head: a later pass at the
// same head that found a problem undoes the approval before it (LEG-573).
func TestALaterCommentAtTheApprovedHeadAsksAgain(t *testing.T) {
	p := newPrompted(t, true)
	p.move(t, "Done")
	p.approve(t, 31)

	view := p.r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "31", "scope": "pr"})
	p.r.tick()
	p.r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{
		{PR: 31, Severity: "blocking", Title: "Breaks", Body: "It breaks.", FailureScenario: "Run it and it breaks."},
	}})
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("while a loop holds #31: %+v", items)
	}
	if _, err := p.r.runs.Workflows.Cancel(context.Background(), view.Run.ID, "done"); err != nil {
		t.Fatal(err)
	}

	items := p.items(t)
	if len(items) != 1 || items[0].Title != "local-1 has 2 open PRs not reviewed" {
		t.Fatalf("after a comment at the approved head: %+v", items)
	}
}

// An approved pull request is never reviewed again by the prompt's loop:
// with the bottom of the stack approved, Start review passes reviews the
// rest without it (LEG-573).
func TestStartingAfterAnApprovalLeavesTheApprovedPROut(t *testing.T) {
	p := newPrompted(t, true)
	p.move(t, "Done")
	p.approve(t, 31)

	items := p.items(t)
	if len(items) != 1 || items[0].Title != "local-1 has 1 open PR not reviewed" || !strings.Contains(items[0].Detail, "#32") || strings.Contains(items[0].Detail, "#31") {
		t.Fatalf("after #31 approved: %+v", items)
	}
	if err := p.adapter.Execute(context.Background(), service.AttentionActionRequest{SourceKey: p.card, ActionID: "start_review"}); err != nil {
		t.Fatal(err)
	}
	p.r.runs.Open(context.Background())
	if loop := p.loopOn(t, 32); loop.Scope != service.ReviewScopePR || loop.SubjectPR != 32 {
		t.Fatalf("loop %+v", loop)
	}
	if held, _ := p.r.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", 31); held != nil {
		t.Fatalf("the approved #31 is reviewed again by %s", held.ID)
	}
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("after starting: %+v", items)
	}
}

// promptFor is the review prompt's items for one card.
func promptFor(t *testing.T, adapter *service.ReviewPromptAttentionAdapter, card string) []service.AttentionInput {
	t.Helper()
	all, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []service.AttentionInput
	for _, item := range all {
		if item.TaskID == card {
			out = append(out, item)
		}
	}
	return out
}

// A handoff's loop holds its pull request: a local card in Done that tracks
// the same pull request is not prompted while it does, nor once the loop
// approved it, until a push moves past the approval (LEG-573).
func TestAHandoffLoopHoldsItsPRFromThePrompt(t *testing.T) {
	ctx := context.Background()
	r := newReviewed(t, ReviewPassOptions{})
	db := r.worker.Store
	if err := db.CreateTask(ctx, store.Task{ID: "local-7", Title: "Same PR", Status: "Done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.TrackTaskBranches(ctx, "local-7", []store.BranchRef{{Repo: "acme/widget", Branch: r.branch}}, service.BranchSourceStack); err != nil {
		t.Fatal(err)
	}
	head := func(sha string) {
		t.Helper()
		if err := db.SetTaskBranchPR(ctx, "local-7", "acme/widget", r.branch, &store.PRMeta{Repo: "acme/widget", Branch: r.branch, PRNumber: 7, State: "OPEN", Head: sha}); err != nil {
			t.Fatal(err)
		}
	}
	head("aaa111")
	adapter := service.NewReviewPromptAttentionAdapter(db, nil)
	adapter.SetReviews(r.worker.Reviews)

	if items := promptFor(t, adapter, "local-7"); len(items) != 1 {
		t.Fatalf("before any loop: %+v", items)
	}
	r.finish(t)
	if items := promptFor(t, adapter, "local-7"); len(items) != 0 {
		t.Fatalf("while the handoff's loop holds #7: %+v", items)
	}

	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Looks right."}); err != nil {
		t.Fatal(err)
	}
	if phase := r.loop(t).Phase; phase != service.ReviewPhaseClean {
		t.Fatalf("loop %q", phase)
	}
	if items := promptFor(t, adapter, "local-7"); len(items) != 0 {
		t.Fatalf("after the handoff's loop approved #7: %+v", items)
	}

	head("ccc333")
	if items := promptFor(t, adapter, "local-7"); len(items) != 1 {
		t.Fatalf("after a push past the approval: %+v", items)
	}
}
