package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// A make-a-change handoff's brief says the repository's pull request mode
// on this worker; with none, it says drafts, as before.
func TestAHandoffsBriefSaysTheRepositorysPullRequestMode(t *testing.T) {
	for mode, want := range map[string]string{
		config.PullRequestsReady: "This repository opens PRs ready for review",
		config.PullRequestsDraft: "This repository opens PRs as drafts, and Legato refuses any other",
		"":                       "Open PRs as drafts; never merge.",
	} {
		t.Run("mode "+mode, func(t *testing.T) {
			ctx := context.Background()
			db, err := store.New(filepath.Join(t.TempDir(), "w.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			if err := db.CreateHandoffCard(ctx, store.Task{ID: "card-1", Title: "x", Status: "Doing"}, nil, store.Handoff{
				MainInstanceID: "srv-main", HandoffID: "ho-1", MainName: "workstation", MainTaskID: "T1", TaskID: "card-1",
				Purpose: HandoffMakeChange, Kind: HandoffNewBranch, Repo: "acme/widget", Branch: "legato/t1", AgentKind: "chimera", State: store.HandoffPending,
			}); err != nil {
				t.Fatal(err)
			}
			agents := &resumeAgents{}
			var rules []config.PullRequestRule
			if mode != "" {
				rules = append(rules, config.PullRequestRule{Match: "Acme/*", Mode: mode})
			}
			svc := NewHandoffWorkerService(db, HandoffWorkerOptions{Agents: agents, PullRequests: pullRequestRules(rules...)})
			svc.start(ctx, "srv-main", "ho-1")
			if len(agents.spawned) != 1 || !strings.Contains(agents.spawned[0].Assignment, want) {
				t.Fatalf("spawned %+v; want the brief to say %q", agents.spawned, want)
			}
		})
	}
}
