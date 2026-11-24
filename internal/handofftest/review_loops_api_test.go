package handofftest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// GET /api/review-loops (spec LEG-559 § API contract, LEG-584): the loop
// that holds a pull request, whichever caller opened it, read from its
// claim, so a launcher opens that loop instead of starting another.

type foundLoop struct {
	LoopID   string `json:"loop_id"`
	Kind     string `json:"kind"`
	RunID    string `json:"run_id"`
	StepPath string `json:"step_path"`
	Phase    string `json:"phase"`
	State    string `json:"state"`
	Held     bool   `json:"held"`
	Outcome  string `json:"outcome"`
	Link     string `json:"link"`
}

// reviewLoopOn asks the worker's web server which loop holds the pull
// request: nil for {loop: null}.
func reviewLoopOn(t *testing.T, w *Worker, query string) *foundLoop {
	t.Helper()
	resp := w.Do(http.MethodGet, "/api/review-loops?"+query, w.WebToken(), "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/review-loops?%s: %d %s", query, resp.StatusCode, body)
	}
	var got struct {
		Loop *foundLoop `json:"loop"`
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["loop"]; !ok {
		t.Fatalf("no loop key in %s", body)
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	return got.Loop
}

// An inline-step run's loop: found by its pull request, the repository's
// case ignored, pointing at the run's page.
func TestThePullRequestOfARunsLoopFindsThatLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "12"})
	r.tick()
	held, err := r.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", 12)
	if err != nil || held == nil {
		t.Fatalf("no loop holds #12: %v", err)
	}

	got := reviewLoopOn(t, r.worker, "repo=ACME/Widget&pr=12")
	if got == nil {
		t.Fatal("no loop found")
	}
	want := foundLoop{LoopID: held.ID, Kind: "run", RunID: view.Run.ID, StepPath: held.StepPath, Phase: held.Phase,
		State: "open", Outcome: held.Outcome, Link: "/workflows/runs/" + view.Run.ID}
	if *got != want || got.StepPath == "" || got.Phase == "" {
		t.Fatalf("loop = %+v, want %+v", *got, want)
	}
}

// A handoff's loop: found by a pull request its pass pinned, pointing at
// the handed-off card.
func TestThePullRequestOfAHandoffsLoopFindsThatLoop(t *testing.T) {
	h := newReviewed(t, ReviewPassOptions{})
	h.finish(t) // pass 1 of the handoff's loop pins #7
	held, err := h.worker.Store.ActiveReviewLoop(context.Background(), "acme/widget", 7)
	if err != nil || held == nil {
		t.Fatalf("no loop holds #7: %v", err)
	}

	got := reviewLoopOn(t, h.worker, "repo=acme/widget&pr=7")
	if got == nil {
		t.Fatal("no loop found")
	}
	want := foundLoop{LoopID: held.ID, Kind: "handoff", Phase: held.Phase, State: "open", Outcome: held.Outcome,
		Link: "/board?highlight_task=" + h.card}
	if *got != want {
		t.Fatalf("loop = %+v, want %+v", *got, want)
	}
}

// A loop opened by a child run (a workflow.run step's, LEG-489) is a
// child_run, pointing at the child's page.
func TestThePullRequestOfAChildRunsLoopFindsThatLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	hosts(t, r.worker, "parent-task", "child-task")
	for _, run := range []store.WorkflowRun{
		{ID: "wr-parent", WorkflowName: "ship", Status: "running", TaskID: "parent-task", DefinitionJSON: "{}", InputsJSON: "{}"},
		{ID: "wr-child", WorkflowName: "review-loop", Status: "running", TaskID: "child-task", DefinitionJSON: "{}", InputsJSON: "{}",
			ParentRunID: "wr-parent", ParentStepPath: "review"},
	} {
		if err := r.worker.Store.CreateWorkflowRun(ctx, run, nil); err != nil {
			t.Fatal(err)
		}
	}
	loop, _, err := r.worker.Reviews.Open(ctx, store.ReviewLoopOpen{
		Loop: store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-child", StepPath: "loop", HostTask: "child-task", Repo: "acme/widget"},
		PRs:  []store.ReviewLoopPR{{Number: 12}}})
	if err != nil {
		t.Fatal(err)
	}

	got := reviewLoopOn(t, r.worker, "repo=acme/widget&pr=12")
	if got == nil || got.LoopID != loop.ID || got.Kind != "child_run" || got.RunID != "wr-child" || got.StepPath != "loop" ||
		got.Link != "/workflows/runs/wr-child" {
		t.Fatalf("loop = %+v", got)
	}
}

// A pull request no loop holds gives {loop: null}; so does one whose loop
// closed, which let its claim go.
func TestAPullRequestNoLoopHoldsGivesNoLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	if got := reviewLoopOn(t, r.worker, "repo=acme/widget&pr=12"); got != nil {
		t.Fatalf("loop = %+v", got)
	}
	ctx := context.Background()
	hosts(t, r.worker, "run-a")
	loop, _, err := r.worker.Reviews.Open(ctx, store.ReviewLoopOpen{
		Loop: store.ReviewLoop{OwnerKind: store.ReviewLoopOwnerRun, RunID: "wr-a", StepPath: "review", HostTask: "run-a", Repo: "acme/widget"},
		PRs:  []store.ReviewLoopPR{{Number: 12}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := reviewLoopOn(t, r.worker, "repo=acme/widget&pr=13"); got != nil {
		t.Fatalf("#13: loop = %+v", got)
	}
	if got := reviewLoopOn(t, r.worker, "repo=acme/other&pr=12"); got != nil {
		t.Fatalf("acme/other#12: loop = %+v", got)
	}
	if _, err := r.worker.Reviews.Close(ctx, loop.ID, "ended"); err != nil {
		t.Fatal(err)
	}
	if got := reviewLoopOn(t, r.worker, "repo=acme/widget&pr=12"); got != nil {
		t.Fatalf("after close: loop = %+v", got)
	}
}

// A missing or invalid parameter is a 400; the route needs the web token.
func TestABadReviewLoopQueryIsRefused(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	for _, query := range []string{"", "repo=acme/widget", "pr=12", "repo=acme/widget&pr=x", "repo=acme/widget&pr=0",
		"repo=acme/widget&pr=-3", "repo=widget&pr=12", "repo=acme/widget/extra&pr=12", "repo=../widget&pr=12"} {
		resp := r.worker.Do(http.MethodGet, "/api/review-loops?"+query, r.worker.WebToken(), "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("?%s: %d, want 400", query, resp.StatusCode)
		}
	}
	if resp := r.worker.Do(http.MethodGet, "/api/review-loops?repo=acme/widget&pr=12", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", resp.StatusCode)
	}
}
