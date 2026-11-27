package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestSpawnAgentNewWorkDefaults(t *testing.T) {
	for _, tc := range []struct{ name, agent, tier, policy, wantKind, wantModel string }{
		{"profile", "", "", "", "codex", "profile-model"},
		{"fresh", "", "", "new", "codex", "profile-model"},
		{"explicit agent", "claude-code", "", "", "claude-code", ""},
		{"explicit tier", "", "chosen", "", "codex", "chosen-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			createTask(t, db, "task-profile")
			codex := hooks.NewCodexAdapter("legato")
			codex.SetTiers(map[string][]string{"profile": {"--model", "profile-model"}, "chosen": {"--model", "chosen-model"}})
			claude := hooks.NewClaudeCodeAdapter("legato")
			mt := newMockTmux()
			calls := 0
			svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: claude, Adapters: map[string]AIToolAdapter{"codex": codex, "claude-code": claude}, DisablePeerAwareness: true, NewWorkDefaults: func(context.Context, string) (NewWorkDefaults, error) {
				calls++
				return NewWorkDefaults{Agent: "codex", Tier: "profile"}, nil
			}})
			if got := svc.DefaultAdapter(); got != "codex" {
				t.Fatalf("default = %s", got)
			}
			calls = 0
			if err := svc.SpawnAgent(context.Background(), "task-profile", 0, 0, AgentSpawnOptions{AgentKind: tc.agent, Tier: tc.tier, SessionPolicy: tc.policy}); err != nil {
				t.Fatal(err)
			}
			lines := mt.sentLinesFor("legato-task-profile")
			if len(lines) != 1 {
				t.Fatalf("launch = %q", lines)
			}
			command := "codex"
			if tc.wantKind == "claude-code" {
				command = "env -u CLAUDE_CONFIG_DIR claude"
			}
			if !strings.HasPrefix(lines[0], command+" ") || (tc.wantModel != "" && !strings.Contains(lines[0], tc.wantModel)) {
				t.Fatalf("launch = %s", lines[0])
			}
			session, err := db.GetAgentSessionByTaskID(context.Background(), "task-profile")
			if err != nil || session.AgentKind != tc.wantKind {
				t.Fatalf("session = %+v, %v", session, err)
			}
			ex, err := db.GetExecution(context.Background(), session.ExecutionID)
			if err != nil || ex.AgentKind != tc.wantKind {
				t.Fatalf("execution = %+v, %v", ex, err)
			}
			wantCalls := 1
			if tc.agent != "" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("profile reads = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestSpawnAgentSurfacesNewWorkDefaultsError(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTask(t, db, "task-profile")
	want := errors.New("profile unavailable")
	mt := newMockTmux()
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewCodexAdapter("legato"), NewWorkDefaults: func(context.Context, string) (NewWorkDefaults, error) { return NewWorkDefaults{}, want }})
	if err := svc.SpawnAgent(context.Background(), "task-profile", 0, 0); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if mt.sessionAlive("legato-task-profile") {
		t.Fatal("launched despite defaults failure")
	}
}

func TestSpawnAgentNewWorkDefaultsExcludeSwarmAndReeve(t *testing.T) {
	for _, reeve := range []bool{false, true} {
		t.Run(fmt.Sprintf("reeve=%v", reeve), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			ctx := context.Background()
			db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			taskID := "conductor"
			opt := AgentSpawnOptions{Role: "conductor"}
			if reeve {
				taskID, err = db.CreateReeveTask(ctx, "Reeve")
				if err != nil {
					t.Fatal(err)
				}
				opt = AgentSpawnOptions{}
			} else {
				createTask(t, db, taskID)
			}
			mt := newMockTmux()
			svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), DisablePeerAwareness: true, NewWorkDefaults: func(context.Context, string) (NewWorkDefaults, error) {
				t.Fatal("excluded role read new-work defaults")
				return NewWorkDefaults{}, nil
			}})
			err = svc.SpawnAgent(ctx, taskID, 0, 0, opt)
			if reeve {
				if !errors.Is(err, ErrReeveSandboxed) {
					t.Fatalf("reeve error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			lines := mt.sentLinesFor("legato-" + taskID)
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "env -u CLAUDE_CONFIG_DIR claude ") {
				t.Fatalf("swarm launch = %q", lines)
			}
		})
	}
}

func TestSpawnAgentValidatesProfileTierBeforeLaunch(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createTask(t, db, "task-profile")
	codex := hooks.NewCodexAdapter("legato")
	codex.SetTiers(map[string][]string{"profile": {"--last"}})
	mt := newMockTmux()
	svc := NewAgentService(db, mt, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewClaudeCodeAdapter("legato"), Adapters: map[string]AIToolAdapter{"codex": codex}, NewWorkDefaults: func(context.Context, string) (NewWorkDefaults, error) {
		return NewWorkDefaults{Agent: "codex", Tier: "profile"}, nil
	}})
	if err := svc.SpawnAgent(ctx, "task-profile", 0, 0, AgentSpawnOptions{SessionPolicy: "new"}); !errors.Is(err, ErrTaskResumeConflict) || !strings.Contains(err.Error(), "--last") {
		t.Fatalf("validation = %v", err)
	}
	if mt.sessionAlive("legato-task-profile") {
		t.Fatal("invalid profile tier launched")
	}
}
