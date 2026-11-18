package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var agentAuthored = map[string]string{"repo": "acme/widget", "pr_number": "12", "agent_author": "true"}

// withBoardAuthor gives the worker a board task linked to #12 whose agent
// runs in dir.
func withBoardAuthor(t *testing.T, r reviewRun) string {
	t.Helper()
	ctx := context.Background()
	if err := r.worker.Store.CreateTask(ctx, store.Task{ID: "board-1", Title: "Greeting", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := r.worker.Store.SetCodeContext(ctx, "board-1", &store.CodeContext{Repo: "acme/widget", Branch: "feature/greeting", WorkingDir: dir, RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}
	if err := r.worker.Agents.SpawnAgent(ctx, "board-1", 0, 0, service.AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: dir}); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	return dir
}

// agentPassOne runs pass 1 with one blocking finding and tells the author.
func agentPassOne(t *testing.T, r reviewRun) (*store.WorkflowRunView, string) {
	t.Helper()
	view := r.start(t, agentAuthored)
	if view.Run.Status != "running" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Steps[0].Error)
	}
	r.tick()
	result := r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	r.tick()
	return view, result.Findings[0]
}

func TestABoardTasksAgentAuthorsTheLoopAndItsAnswersAreRuledOn(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	view, finding := agentPassOne(t, r)

	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato review pass] Pass 1 has 1 blocking finding. Run legato review-pass feedback --json to read them.") {
		t.Fatalf("typed to the author: %q", typed)
	}
	feedback, err := service.ReviewFeedback(ctx, r.worker.Store, "board-1")
	if err != nil || len(feedback.Items) != 1 || feedback.Items[0].ID != finding {
		t.Fatalf("feedback = %+v, %v", feedback, err)
	}
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", finding, service.AnswerReplied, "An empty name never reaches greet.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if replies := r.worker.ReviewGitHub.Replies(); len(replies) != 1 || !strings.HasPrefix(replies[0], "author ") {
		t.Fatalf("replies = %v", replies)
	}
	r.tick()
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Pass != 2 || !r.reviewer(t, view.Run.TaskID) {
		t.Fatalf("no pass 2 on the dispute: %+v", loop)
	}
	spawns := r.worker.Tmux.Spawned()
	if brief := briefOf(t, spawns[len(spawns)-1]); !strings.Contains(brief, "<author-answer>replied: An empty name never reaches greet.</author-answer>") {
		t.Fatalf("the brief does not carry the answer:\n%s", brief)
	}
}

func TestAnExitedAgentAuthorIsResumedFromItsConversation(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	dir := withBoardAuthor(t, r)
	ctx := context.Background()
	first, err := r.worker.Store.GetAgentSessionByTaskID(ctx, "board-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.worker.Store.RecordTaskNativeSession(ctx, "board-1", first.ID, first.ExecutionID, "claude-code", "author-conversation", dir, ""); err != nil {
		t.Fatal(err)
	}
	_ = r.worker.Tmux.Kill("legato-board-1")
	agentPassOne(t, r)

	spawns := r.worker.Tmux.Spawned()
	var resumed *Spawn
	for i := range spawns {
		if spawns[i].Name == "legato-board-1" && strings.Contains(strings.Join(spawns[i].Env, "\n"), "LEGATO_RESUME_SESSION_ID=author-conversation") {
			resumed = &spawns[i]
		}
	}
	if resumed == nil {
		t.Fatalf("the author was not resumed: %+v", spawns)
	}
}

func TestAnAgentAuthorThatCannotBeResumedMakesTheLoopStuck(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	_ = r.worker.Tmux.Kill("legato-board-1")
	spawned := len(r.worker.Tmux.Spawned())
	view, _ := agentPassOne(t, r)
	loop := r.loopPhase(t, view.Run.TaskID)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "no saved conversation") {
		t.Fatalf("loop = %+v", loop)
	}
	for _, s := range r.worker.Tmux.Spawned()[spawned:] {
		if s.Name == "legato-board-1" {
			t.Fatal("a fresh author was started")
		}
	}
}

func TestMessageTheAuthorReachesTheAgentOfAStuckLoop(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{MaxPasses: 1})
	withBoardAuthor(t, r)
	ctx := context.Background()
	view, finding := agentPassOne(t, r)
	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", finding, service.AnswerReplied, "Intended.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.tick()
	if loop := r.loopPhase(t, view.Run.TaskID); loop.Phase != service.ReviewPhaseStuck {
		t.Fatalf("loop = %+v", loop)
	}
	items := r.attention(t)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	var ids []string
	for _, a := range items[0].Actions {
		ids = append(ids, a.ID)
	}
	if strings.Join(ids, ",") != "open,another_pass,end,message" {
		t.Fatalf("actions = %v", ids)
	}
	if err := service.NewWorkflowAttentionAdapter(r.runs.Workflows).Execute(ctx, service.AttentionActionRequest{SourceKey: items[0].SourceKey, ActionID: "message", Text: "The reviewer is right: guard the empty name."}); err != nil {
		t.Fatal(err)
	}
	r.worker.Settle()
	if typed := r.worker.typedTo("legato-board-1"); !strings.Contains(typed, "[legato review pass] From the operator: The reviewer is right: guard the empty name.") {
		t.Fatalf("typed = %q", typed)
	}
}
