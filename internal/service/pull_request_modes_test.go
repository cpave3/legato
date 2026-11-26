package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func pullRequestRules(rules ...config.PullRequestRule) PullRequestModes {
	return func(context.Context) (config.PullRequestsConfig, error) {
		return config.PullRequestsConfig{Rules: rules}, nil
	}
}

// The launch prompt says how the repository opens pull requests: drafts,
// or ready for review. agent, and no rule, say nothing: the agent chooses.
func TestSpawnAgent_TheLaunchPromptSaysTheRepositorysPullRequestMode(t *testing.T) {
	modes := pullRequestRules(
		config.PullRequestRule{Match: "acme/*", Mode: config.PullRequestsDraft},
		config.PullRequestRule{Match: "acme/web-app", Mode: config.PullRequestsReady},
		config.PullRequestRule{Match: "acme/tools", Mode: config.PullRequestsAgent},
	)
	for repo, want := range map[string]string{
		"git@github.com:Acme/Web-App.git": "Open pull requests in acme/web-app ready for review, not as drafts.",
		"git@github.com:acme/api.git":     "Open pull requests in acme/api as drafts (`gh pr create --draft`): Legato refuses any other there.",
		"git@github.com:acme/tools.git":   "",
		"git@github.com:other/thing.git":  "",
	} {
		t.Run(repo, func(t *testing.T) {
			db := newServiceTestStore(t)
			ctx := context.Background()
			createTask(t, db, "task-1")
			if err := db.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: repo}); err != nil {
				t.Fatal(err)
			}
			mt := newMockTmux()
			svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
				Adapter:            &fakeAdapter{name: "claude-code"},
				RepositoryGuidance: NewRepositoryGuidanceService(nil, nil),
				PullRequestModes:   modes,
			})
			if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
				t.Fatal(err)
			}
			prompt := spawnedLaunchPrompt(t, mt, "task-1")
			if want == "" {
				if strings.Contains(prompt, "## Pull requests") {
					t.Fatalf("launch prompt names a pull request mode: %q", prompt)
				}
				return
			}
			if !strings.Contains(prompt, "## Pull requests\n\n"+want) {
				t.Fatalf("launch prompt does not say %q: %q", want, prompt)
			}
		})
	}
}

func TestThePullRequestModeReadsTheRulesEachTime(t *testing.T) {
	rules := []config.PullRequestRule{}
	modes := PullRequestModes(func(context.Context) (config.PullRequestsConfig, error) {
		return config.PullRequestsConfig{Rules: rules}, nil
	})
	if got := modes.PullRequestMode(context.Background(), "cpave3/legato"); got != "" {
		t.Fatalf("mode = %q", got)
	}
	rules = append(rules, config.PullRequestRule{Match: "CPAVE3/*", Mode: config.PullRequestsReady})
	if got := modes.PullRequestMode(context.Background(), "github.com/cpave3/legato"); got != config.PullRequestsReady {
		t.Fatalf("mode = %q", got)
	}
	var none PullRequestModes
	if got := none.PullRequestMode(context.Background(), "cpave3/legato"); got != "" {
		t.Fatalf("mode = %q", got)
	}
}

func TestReadyAfterReviewReadsTheRulesEachTime(t *testing.T) {
	ctx := context.Background()
	rules := []config.PullRequestRule{{Match: "acme/*", Mode: config.PullRequestsDraft}}
	modes := PullRequestModes(func(context.Context) (config.PullRequestsConfig, error) {
		return config.PullRequestsConfig{Rules: rules}, nil
	})
	if modes.ReadyAfterReview(ctx, "acme/widget") {
		t.Fatal("a draft rule without ready_after_review marks stacks ready")
	}
	rules[0].ReadyAfterReview = true
	if !modes.ReadyAfterReview(ctx, "acme/widget") {
		t.Fatal("the saved ready_after_review is not read")
	}
	failing := PullRequestModes(func(context.Context) (config.PullRequestsConfig, error) {
		return config.PullRequestsConfig{}, errors.New("unreadable")
	})
	var none PullRequestModes
	if failing.ReadyAfterReview(ctx, "acme/widget") || none.ReadyAfterReview(ctx, "acme/widget") {
		t.Fatal("rules that cannot be read mark stacks ready")
	}
}
