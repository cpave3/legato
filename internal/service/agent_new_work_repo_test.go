package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// newWorkRepoFixture records the repository each spawn asked defaults for,
// and answers Work for autopilot-dev/autopilot and Default otherwise.
type newWorkRepoFixture struct {
	db        *store.Store
	mt        *mockTmux
	svc       AgentService
	mu        sync.Mutex
	asked     []string
	serviceWD string
}

func newNewWorkRepoFixture(t *testing.T) *newWorkRepoFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &newWorkRepoFixture{db: db, mt: newMockTmux(), serviceWD: t.TempDir()}
	codex := hooks.NewCodexAdapter("legato")
	claude := hooks.NewClaudeCodeAdapter("legato")
	f.svc = NewAgentService(db, f.mt, f.serviceWD, AgentServiceOptions{Adapter: claude, Adapters: map[string]AIToolAdapter{"codex": codex, "claude-code": claude}, DisablePeerAwareness: true,
		CheckoutInspector: CheckoutInspectFunc(func(string) (*store.CodeContext, error) {
			return &store.CodeContext{Repo: "git@github.com:autopilot-dev/autopilot.git"}, nil
		}),
		NewWorkDefaults: func(_ context.Context, repo string) (NewWorkDefaults, error) {
			f.mu.Lock()
			f.asked = append(f.asked, repo)
			f.mu.Unlock()
			if repo == "github.com/autopilot-dev/autopilot" {
				return NewWorkDefaults{Agent: "codex"}, nil
			}
			return NewWorkDefaults{Agent: "claude-code"}, nil
		}})
	return f
}

func (f *newWorkRepoFixture) kind(t *testing.T, taskID string) string {
	t.Helper()
	session, err := f.db.GetAgentSessionByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return session.AgentKind
}

func TestNewWorkDefaultsFollowTheTasksRecordedRepository(t *testing.T) {
	f := newNewWorkRepoFixture(t)
	ctx := context.Background()
	createTask(t, f.db, "work")
	if err := f.db.SetCodeContext(ctx, "work", &store.CodeContext{Repo: "Autopilot-Dev/Autopilot"}); err != nil {
		t.Fatal(err)
	}
	createTask(t, f.db, "plain")
	if err := f.svc.SpawnAgent(ctx, "work", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "plain", 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := f.kind(t, "work"); got != "codex" {
		t.Fatalf("work task's agent = %s", got)
	}
	if got := f.kind(t, "plain"); got != "claude-code" {
		t.Fatalf("plain task's agent = %s", got)
	}
	// The plain task names no repository. The agent service's own folder
	// (whose origin the inspector would call autopilot) is a guess, and
	// usage never reads it: the global policy applies.
	if strings.Join(f.asked, ",") != "github.com/autopilot-dev/autopilot," {
		t.Fatalf("asked = %q", f.asked)
	}
}

func TestNewWorkDefaultsInspectAnExplicitWorkingDirectory(t *testing.T) {
	f := newNewWorkRepoFixture(t)
	ctx := context.Background()
	createTask(t, f.db, "dir")
	dir := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "dir", 0, 0, AgentSpawnOptions{WorkingDir: dir}); err != nil {
		t.Fatal(err)
	}
	if got := f.kind(t, "dir"); got != "codex" {
		t.Fatalf("agent = %s", got)
	}
	if len(f.asked) != 1 || f.asked[0] != "github.com/autopilot-dev/autopilot" {
		t.Fatalf("asked = %q", f.asked)
	}
}

func TestNewWorkDefaultsNeverReadACallersDefaultedFolder(t *testing.T) {
	f := newNewWorkRepoFixture(t)
	createTask(t, f.db, "defaulted")
	if err := f.svc.SpawnAgent(context.Background(), "defaulted", 0, 0, AgentSpawnOptions{WorkingDir: t.TempDir(), WorkingDirDefaulted: true}); err != nil {
		t.Fatal(err)
	}
	if got := f.kind(t, "defaulted"); got != "claude-code" || len(f.asked) != 1 || f.asked[0] != "" {
		t.Fatalf("agent = %s asked = %q", got, f.asked)
	}
}

func TestNewWorkDefaultsNameTheRepositoryAJobCloneIsAskedFor(t *testing.T) {
	f := newNewWorkRepoFixture(t)
	createTask(t, f.db, "cloned")
	// No repository service: the clone is refused, after the defaults were
	// read for the repository the spawn names.
	_ = f.svc.SpawnAgent(context.Background(), "cloned", 0, 0, AgentSpawnOptions{Repo: "autopilot-dev/autopilot"})
	if len(f.asked) != 1 || f.asked[0] != "github.com/autopilot-dev/autopilot" {
		t.Fatalf("asked = %q", f.asked)
	}
}

func TestExplicitLaunchChoicesAndRolesSkipRepositoryDefaults(t *testing.T) {
	f := newNewWorkRepoFixture(t)
	ctx := context.Background()
	createTask(t, f.db, "explicit")
	if err := f.db.SetCodeContext(ctx, "explicit", &store.CodeContext{Repo: "autopilot-dev/autopilot"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SpawnAgent(ctx, "explicit", 0, 0, AgentSpawnOptions{AgentKind: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if got := f.kind(t, "explicit"); got != "claude-code" || len(f.asked) != 0 {
		t.Fatalf("agent = %s asked = %q", got, f.asked)
	}
}
