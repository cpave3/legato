package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/handofftest"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// A main's POST /api/tasks/{id}/handoff with a review override reaches a
// worker whose review loop is built as its runtime builds it, and the pass
// runs with the override's reviewer, model, skill and pass limit (LEG-166).
func TestAHandOffCallsReviewOverrideRunsOnAWorkerWiredAsTheRuntimeWiresIt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "code-review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "skills", "code-review", "SKILL.md"), []byte("# code-review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`db:
  path: `+filepath.Join(root, "legato.db")+`
adapters:
  codex:
    tiers:
      careful:
        launch_args: ["-m", "gpt-5.5"]
credentials:
  - name: reviewer-app
    secret: env://REVIEWER_KEY
    profile: github-reviewer
    settings: {client_id: Iv23liReViEwEr, repositories: ["acme/*"]}
review_passes:
  enabled: true
  reviewer: {agent: codex, tier: careful}
  github_credential: reviewer-app
  max_passes: 3
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	worker := handofftest.StartWorker(t, handofftest.WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"},
		ReviewPasses: &handofftest.ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
			return workerReviewPassOptions(cfg, o)
		}}})
	main := handofftest.NewMain(t, handofftest.MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := handofftest.Pair(t, main, worker)
	card, err := main.Board.CreateTask(ctx, "Fix the flaky login test", "", "Ready", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := server.New(main.Board, nil, nil, "")
	srv.SetAuthToken("main-token")
	srv.SetMainHandoffs(main.Handoffs)
	body := `{"worker": "` + paired.InstanceID + `", "purpose": "make-change", "kind": "new-branch", "repo": "acme/widget",
		"agent_kind": "claude-code", "review": {"enabled": true, "agent": "claude-code", "model": "opus-x", "skill": "code-review", "max_passes": 2}}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+card.ID+"/handoff", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer main-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("hand off: %d %s", rec.Code, rec.Body.String())
	}
	var answer struct {
		Away struct {
			WorkerTaskID string `json:"worker_task_id"`
		} `json:"away"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.Away.WorkerTaskID == "" {
		t.Fatalf("answer %s (%v)", rec.Body.String(), err)
	}
	cardOnWorker := answer.Away.WorkerTaskID

	// The author opens a pull request and finishes; the loop starts a pass.
	worker.Settle()
	branch := "legato/" + strings.ToLower(card.ID)
	worker.Checkout.Set(worker.Tmux.Spawned()[0].WorkDir, branch, "aaa111")
	worker.Checkout.File("login.go", 40)
	worker.ReviewGitHub.OpenPR(7, branch, "aaa111")
	if err := worker.Board.MoveCard(ctx, cardOnWorker, "Done"); err != nil {
		t.Fatal(err)
	}
	worker.Reviews.Tick(ctx)
	worker.Settle()

	reviewer, err := worker.Store.GetAgentSessionInSlot(ctx, cardOnWorker, "reviewer")
	if err != nil {
		t.Fatalf("no reviewer runs: %v", err)
	}
	if reviewer.AgentKind != "claude-code" {
		t.Fatalf("the reviewer runs %q, want the override's claude-code", reviewer.AgentKind)
	}
	if _, err := worker.Reviews.Submit(ctx, cardOnWorker, reviewer.ExecutionID, service.ReviewSubmission{Summary: "Solid fix."}); err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	posted := worker.ReviewGitHub.Posted()
	if len(posted) != 1 || !strings.Contains(posted[0].Footer, "claude-code (opus-x)") || !strings.Contains(posted[0].Footer, "code-review") {
		t.Fatalf("posted = %+v", posted)
	}
	loop, err := worker.Store.GetHandoffReview(ctx, cardOnWorker)
	if err != nil || loop == nil {
		t.Fatalf("loop %+v, %v", loop, err)
	}
	var settings service.ReviewPassSettings
	if err := json.Unmarshal([]byte(loop.Settings), &settings); err != nil || settings.MaxPasses != 2 {
		t.Fatalf("settings %s (%v), want max passes 2", loop.Settings, err)
	}
}
