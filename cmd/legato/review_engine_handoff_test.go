package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/handofftest"
	"github.com/cpave3/legato/internal/service"
)

// The review engine a TUI builds without its web server (another process
// holds the web port) still knows the worker's handed-off reviews: its
// handoff caller is registered with the engine, not with the web routes, so
// when that TUI later holds the broker it moves them on, and its one loop
// per pull request check sees the handoff's PR.
func TestTheReviewEngineBuiltWithoutAWebServerKnowsHandedOffReviews(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`db:
  path: `+filepath.Join(root, "legato.db")+`
credentials:
  - name: reviewer-app
    secret: env://REVIEWER_KEY
    profile: github-reviewer
    settings: {client_id: Iv23liReViEwEr, repositories: ["acme/*"]}
review_passes:
  enabled: true
  reviewer: {agent: claude-code}
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

	defaultsDB, err := store.New(filepath.Join(root, "defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer defaultsDB.Close()

	// serve: the worker that took the handoff and runs its first pass.
	worker := handofftest.StartWorker(t, handofftest.WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"},
		ReviewPasses: &handofftest.ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
			return workerReviewPassOptions(cfg, defaultsDB, o)
		}}})
	main := handofftest.NewMain(t, handofftest.MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := handofftest.Pair(t, main, worker)
	card, err := main.Board.CreateTask(ctx, "Fix the flaky login test", "", "Ready", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: card.ID, Worker: paired.InstanceID,
		Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch, Repo: "acme/widget", AgentKind: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	branch := "legato/" + strings.ToLower(card.ID)
	worker.Checkout.Set(worker.Tmux.Spawned()[0].WorkDir, branch, "aaa111")
	worker.Checkout.File("login.go", 40)
	worker.ReviewGitHub.OpenPR(7, branch, "aaa111")
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	worker.Reviews.Tick(ctx)
	worker.Settle()
	if worker.Reviews.ActiveLoopOn(ctx, "acme/widget", 7) == nil {
		t.Fatal("the worker's own engine has no loop on #7")
	}

	// The TUI on the same data folder, which could not take the web port:
	// its engine, built as the runtime builds it, without wireIntegrations.
	tui := startReviewEngine(integrationDeps{cfg: cfg, db: worker.Store, bus: worker.Bus, agents: worker.Agents,
		board: worker.Board, approvals: &approvalRuntime{cfg: cfg, channels: &service.CredentialChannels{},
			brokerOwner: func() bool { return false }}})
	defer tui.stop()
	if tui.reviews == nil {
		t.Fatal("no engine")
	}
	if active := tui.reviews.ActiveLoopOn(ctx, "acme/widget", 7); active == nil {
		t.Fatal("the TUI's engine sees no active loop on the handed-off task's #7")
	}
}
