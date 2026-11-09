package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func spawnedLaunchPrompt(t *testing.T, mt *mockTmux, taskID string) string {
	t.Helper()
	env := mt.envVarsFor("legato-" + taskID)
	path := env["LEGATO_ROLE_PROMPT_FILE"]
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSpawnAgent_RepositoryGuidanceIsInTheLaunchPrompt(t *testing.T) {
	guidance := []config.AgentGuidance{
		{Repo: "github.com/acme/web-app", Text: "Run the prove-it skill when the work is done."},
		{Repo: "github.com/acme/other", Text: "OTHER REPOSITORY GUIDANCE"},
	}
	for _, name := range []string{"claude-code", "codex", "chimera"} {
		t.Run(name, func(t *testing.T) {
			db := newServiceTestStore(t)
			ctx := context.Background()
			createTask(t, db, "task-1")
			if err := db.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: "git@github.com:Acme/Web-App.git"}); err != nil {
				t.Fatal(err)
			}
			mt := newMockTmux()
			svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
				Adapter:            &fakeAdapter{name: name},
				RepositoryGuidance: NewRepositoryGuidanceService(nil, guidance),
			})
			if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
				t.Fatal(err)
			}
			prompt := spawnedLaunchPrompt(t, mt, "task-1")
			if !strings.Contains(prompt, "Run the prove-it skill when the work is done.") {
				t.Fatalf("launch prompt has no guidance for the repository: %q", prompt)
			}
			if strings.Contains(prompt, "OTHER REPOSITORY GUIDANCE") {
				t.Fatalf("launch prompt has another repository's guidance: %q", prompt)
			}
		})
	}
}

func TestSpawnAgent_RepositoryGuidanceFindsTheRepositoryOfAnyCheckout(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	createTask(t, db, "task-1")
	worktree := t.TempDir()
	inspector := CheckoutInspectFunc(func(dir string) (*store.CodeContext, error) {
		if dir != worktree {
			return nil, nil
		}
		return &store.CodeContext{Repo: "https://github.com/acme/web-app.git"}, nil
	})
	mt := newMockTmux()
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
		Adapter:           &fakeAdapter{name: "claude-code"},
		CheckoutInspector: inspector,
		RepositoryGuidance: NewRepositoryGuidanceService(nil, []config.AgentGuidance{
			{Repo: "github.com/acme/web-app", Text: "Run the prove-it skill when the work is done."},
		}),
	})
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{WorkingDir: worktree}); err != nil {
		t.Fatal(err)
	}
	if prompt := spawnedLaunchPrompt(t, mt, "task-1"); !strings.Contains(prompt, "Run the prove-it skill") {
		t.Fatalf("launch prompt has no guidance for the worktree's repository: %q", prompt)
	}
}

func TestSpawnAgent_ReviewsOffTellsTheAgentToSkipThem(t *testing.T) {
	for _, reviewsOff := range []bool{false, true} {
		db := newServiceTestStore(t)
		ctx := context.Background()
		createTask(t, db, "task-1")
		mt := newMockTmux()
		svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
			Adapter:    &fakeAdapter{name: "claude-code"},
			ReviewsOff: reviewsOff,
		})
		if err := svc.SpawnAgent(ctx, "task-1", 0, 0); err != nil {
			t.Fatal(err)
		}
		prompt := spawnedLaunchPrompt(t, mt, "task-1")
		says := strings.Contains(prompt, "Legato reviews are off") && strings.Contains(prompt, "legato-review")
		if says != reviewsOff {
			t.Fatalf("reviews off=%v, launch prompt=%q", reviewsOff, prompt)
		}
	}
}

// Claude Code and Codex learn of reviews only from the launch prompt, so with
// reviews on it asks them for one. An adapter that does not say so (Chimera,
// whose own prompt teaches reviews) and a swarm worker, which never reviews,
// are not asked.
func TestSpawnAgent_ReviewsOnAsksForATourWhereNothingElseDoes(t *testing.T) {
	spawn := func(t *testing.T, adapter AIToolAdapter, opt AgentSpawnOptions) string {
		t.Helper()
		db := newServiceTestStore(t)
		createTask(t, db, "task-1")
		mt := newMockTmux()
		svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter})
		if err := svc.SpawnAgent(context.Background(), "task-1", 0, 0, opt); err != nil {
			t.Fatal(err)
		}
		return spawnedLaunchPrompt(t, mt, "task-1")
	}
	asks := func(prompt string) bool {
		return strings.Contains(prompt, "use the legato-review skill") && !strings.Contains(prompt, "reviews are off")
	}

	learnsFromLaunch := reviewsPromptAdapter{fakeAdapter: &fakeAdapter{name: "claude-code", rolePrompts: map[string]string{"worker": "STANDING ROLE"}}}
	if prompt := spawn(t, learnsFromLaunch, AgentSpawnOptions{}); !asks(prompt) {
		t.Fatalf("a plain agent should be asked for a review tour: %q", prompt)
	}
	if prompt := spawn(t, learnsFromLaunch, AgentSpawnOptions{Role: "worker"}); asks(prompt) {
		t.Fatalf("a swarm worker should not be asked for a review tour: %q", prompt)
	}
	if prompt := spawn(t, &fakeAdapter{name: "chimera"}, AgentSpawnOptions{}); asks(prompt) {
		t.Fatalf("an adapter whose prompts teach reviews should not be asked again: %q", prompt)
	}
}

type reviewsPromptAdapter struct{ *fakeAdapter }

func (reviewsPromptAdapter) NeedsReviewsPrompt() bool { return true }

// The Reeve's own launch prompt gets no repository guidance and no review
// prompt.
func TestSpawnAgent_AnOwnLaunchPromptGetsNoGuidanceAndNoReviewPrompt(t *testing.T) {
	db := newServiceTestStore(t)
	ctx := context.Background()
	createTask(t, db, "task-1")
	db.SetCodeContext(ctx, "task-1", &store.CodeContext{Repo: "git@github.com:acme/web-app.git"})
	mt := newMockTmux()
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{
		Adapter:            reviewsPromptAdapter{fakeAdapter: &fakeAdapter{name: "claude-code"}},
		RepositoryGuidance: NewRepositoryGuidanceService(nil, []config.AgentGuidance{{Repo: "github.com/acme/web-app", Text: "REPOSITORY GUIDANCE"}}),
	})
	if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{LaunchPrompt: "YOU ARE THE REEVE"}); err != nil {
		t.Fatal(err)
	}
	prompt := spawnedLaunchPrompt(t, mt, "task-1")
	if !strings.HasPrefix(prompt, "YOU ARE THE REEVE") || strings.Contains(prompt, "REPOSITORY GUIDANCE") || strings.Contains(prompt, "legato-review") {
		t.Fatalf("launch prompt = %q", prompt)
	}
}
