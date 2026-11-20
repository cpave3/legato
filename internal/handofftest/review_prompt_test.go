package handofftest

import (
	"context"
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
	example, _ := workflowengine.ExampleNamed("review-pr-passes")
	return newPromptedWith(t, reviewsOn, example.YAML)
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
	if err := os.WriteFile(filepath.Join(dir, "review-pr-passes.workflow.yaml"), workflowYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := service.NewTaskWorkflowLauncher(db, r.runs.Workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) })
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
	runs, err := p.r.worker.Store.ListWorkflowRuns(context.Background(), store.WorkflowRunFilter{WorkflowName: "review-pr-passes"})
	if err != nil || len(runs) != 1 || !strings.Contains(runs[0].WorkflowTitle, "#31–#32") {
		t.Fatalf("runs %+v, %v", runs, err)
	}
	// The loop it started clears the item.
	if items := p.items(t); len(items) != 0 {
		t.Fatalf("after starting: %+v", items)
	}
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

func TestAWorkflowAddedBeforeStacksIsNotOfferedButNamed(t *testing.T) {
	p := newPromptedWith(t, true, beforeStacks(t))
	p.move(t, "Done")

	items := p.items(t)
	if len(items) != 1 || !strings.Contains(items[0].Detail, "legato workflow add review-pr-passes --force") {
		t.Fatalf("items %+v", items)
	}
	for _, a := range items[0].Actions {
		if a.ID == "start_review" {
			t.Fatal("it offers a start that cannot work")
		}
	}
}
