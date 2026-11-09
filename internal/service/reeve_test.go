package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeReeveAgents struct {
	spawned []AgentSpawnOptions
	tasks   []string
	killed  []string
}

func (f *fakeReeveAgents) SpawnAgent(_ context.Context, taskID string, _, _ int, opts ...AgentSpawnOptions) error {
	f.tasks = append(f.tasks, taskID)
	f.spawned = append(f.spawned, opts[0])
	return nil
}

func (f *fakeReeveAgents) KillAgent(_ context.Context, taskID string) error {
	f.killed = append(f.killed, taskID)
	return nil
}

func newReeveFixture(t *testing.T) (*ReeveService, *store.Store, *fakeReeveAgents, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	agents := &fakeReeveAgents{}
	folder := filepath.Join(dir, "state", "legato", "reeve")
	svc := NewReeveService(ReeveServiceOptions{Store: db, Agents: agents, Folder: folder,
		Defaults: func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Model: "opus", LaunchArgs: []string{"--verbose"}} }})
	return svc, db, agents, folder
}

func TestTheReeveFolderIsInTheStateFolder(t *testing.T) {
	env := map[string]string{"XDG_STATE_HOME": "/s", "HOME": "/h"}
	if got := ReeveFolder(func(k string) string { return env[k] }); got != "/s/legato/reeve" {
		t.Fatalf("with XDG_STATE_HOME: %s", got)
	}
	delete(env, "XDG_STATE_HOME")
	if got := ReeveFolder(func(k string) string { return env[k] }); got != "/h/.local/state/legato/reeve" {
		t.Fatalf("without: %s", got)
	}
}

func TestAReeveSessionIsAHiddenTaskInTheReevesFolder(t *testing.T) {
	svc, db, agents, folder := newReeveFixture(t)
	ctx := context.Background()
	session, err := svc.Start(ctx, ReeveStart{Model: "sonnet", Brief: "why did the sync fail?"})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := db.GetTask(ctx, session.TaskID)
	if !task.Ephemeral || task.Kind != store.TaskKindReeve {
		t.Fatalf("task = %+v", task)
	}
	opt := agents.spawned[0]
	if opt.WorkingDir != filepath.Join(folder, "work") || opt.AgentKind != "claude-code" || opt.Model != "sonnet" || opt.Unsandboxed || len(opt.LaunchArgs) != 1 {
		t.Fatalf("spawn = %+v", opt)
	}
	if len(opt.Resources) != 1 || opt.Resources[0].Paths[0].Path != folder || opt.Resources[0].Paths[0].Access != "write" {
		t.Fatalf("the folder is not given read-write: %+v", opt.Resources)
	}
	if listed, _ := db.ListTasksByStatus(ctx, task.Status); len(listed) != 0 {
		t.Fatalf("on the board: %+v", listed)
	}
}

func TestOneReeveSessionRunsAtATime(t *testing.T) {
	svc, _, _, _ := newReeveFixture(t)
	ctx := context.Background()
	if _, err := svc.Start(ctx, ReeveStart{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(ctx, ReeveStart{}); !errors.Is(err, ErrReeveRunning) {
		t.Fatalf("a second session: %v", err)
	}
}

func TestEndingRevokesTheTokenAndResumingMakesAnother(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	session, _ := svc.Start(ctx, ReeveStart{})
	first, _ := db.GetReeveSession(ctx, session.TaskID)
	if _, err := svc.Authenticate(ctx, first.APIToken); err != nil {
		t.Fatalf("a running session's token: %v", err)
	}
	if err := svc.End(ctx, session.TaskID); err != nil {
		t.Fatal(err)
	}
	if len(agents.killed) != 1 {
		t.Fatal("the agent was not stopped")
	}
	if _, err := svc.Authenticate(ctx, first.APIToken); err == nil {
		t.Fatal("an ended session's token still works")
	}
	if err := svc.Delete(ctx, "nope"); err == nil {
		t.Fatal("deleted a session that does not exist")
	}

	resumed, err := svc.Resume(ctx, session.TaskID, ReeveStart{})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := db.GetReeveSession(ctx, resumed.TaskID)
	if again.APIToken == "" || again.APIToken == first.APIToken {
		t.Fatal("no new token on resume")
	}
	if agents.spawned[1].SessionPolicy != "resume" {
		t.Fatalf("resume spawn = %+v", agents.spawned[1])
	}
	if err := svc.Delete(ctx, session.TaskID); !errors.Is(err, ErrReeveNotEnded) {
		t.Fatalf("deleting a running session: %v", err)
	}
	svc.End(ctx, session.TaskID)
	if err := svc.Delete(ctx, session.TaskID); err != nil {
		t.Fatal(err)
	}
	if overview, _ := svc.Overview(ctx); len(overview.History) != 0 {
		t.Fatalf("history after delete = %+v", overview.History)
	}
}

func TestResumingWhileAnotherRunsIsRefused(t *testing.T) {
	svc, _, _, _ := newReeveFixture(t)
	ctx := context.Background()
	old, _ := svc.Start(ctx, ReeveStart{})
	svc.End(ctx, old.TaskID)
	svc.Start(ctx, ReeveStart{})
	if _, err := svc.Resume(ctx, old.TaskID, ReeveStart{}); !errors.Is(err, ErrReeveRunning) {
		t.Fatalf("resume while another runs: %v", err)
	}
}

// A reeve task never runs outside the sandbox.
func TestAReeveTaskIsRefusedOutsideTheSandbox(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	taskID, _ := db.CreateReeveTask(ctx, "The Reeve")
	svc := NewAgentService(db, newMockTmux(), t.TempDir())
	if err := svc.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{Unsandboxed: true}); !errors.Is(err, ErrReeveSandboxed) {
		t.Fatalf("run without sandbox: %v", err)
	}
	if err := svc.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{}); !errors.Is(err, ErrReeveSandboxed) {
		t.Fatalf("with the sandbox off: %v", err)
	}
}
