package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// restartFixture is an agent service with a Claude Code adapter over a mock
// tmux and a real store.
type restartFixture struct {
	svc   AgentService
	store *store.Store
	tmux  *mockTmux
}

func newRestartFixture(t *testing.T) restartFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := store.New(filepath.Join(t.TempDir(), "restart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter: hooks.NewClaudeCodeAdapter("legato"), DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond,
	})
	svc.(*agentService).nativeBindTimeout = time.Millisecond
	return restartFixture{svc: svc, store: s, tmux: mt}
}

// reportConversation records the conversation the running agent of taskID
// reported, as its bridge does.
func (f restartFixture) reportConversation(t *testing.T, taskID, cwd string) {
	t.Helper()
	ctx := context.Background()
	session, err := f.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RecordTaskNativeSession(ctx, taskID, session.ID, session.ExecutionID, "claude-code", "conversation-1", cwd, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func (f restartFixture) lastLaunch(t *testing.T, taskID string) string {
	t.Helper()
	lines := f.tmux.sentLinesFor("legato-" + taskID)
	if len(lines) == 0 {
		t.Fatal("nothing was launched")
	}
	return lines[len(lines)-1]
}

func TestRestartAgentStartsItAgainResumingItsConversation(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-r")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	before, _ := f.store.GetAgentSessionByTaskID(ctx, "task-r")
	f.reportConversation(t, "task-r", cwd)

	if err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-r", nil); err != nil {
		t.Fatal(err)
	}

	if launch := f.lastLaunch(t, "task-r"); !strings.HasPrefix(launch, "claude --resume conversation-1") {
		t.Fatalf("launch = %q, want claude --resume conversation-1", launch)
	}
	after, err := f.store.GetAgentSessionByTaskID(ctx, "task-r")
	if err != nil || after.ExecutionID == before.ExecutionID {
		t.Fatalf("session after restart = %#v, %v; want a new execution", after, err)
	}
	execution, err := f.store.GetExecution(ctx, after.ExecutionID)
	if err != nil || execution.WorkingDir != cwd {
		t.Fatalf("execution = %#v, %v; want working dir %s", execution, err, cwd)
	}
	old, err := f.store.GetExecution(ctx, before.ExecutionID)
	if err != nil || old.Status != store.ExecutionCancelled {
		t.Fatalf("old execution = %#v, %v; want it cancelled", old, err)
	}
}

func TestRestartAgentResumesAnEphemeralAgentsConversation(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	if err := f.svc.SpawnEphemeralAgent(ctx, "scratch", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	agents, err := f.svc.ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		t.Fatalf("agents = %v, %v", agents, err)
	}
	taskID := agents[0].TaskID
	f.reportConversation(t, taskID, cwd)

	if err := f.svc.(AgentRestarter).RestartAgent(ctx, taskID, nil); err != nil {
		t.Fatal(err)
	}

	if launch := f.lastLaunch(t, taskID); !strings.HasPrefix(launch, "claude --resume conversation-1") {
		t.Fatalf("launch = %q, want claude --resume conversation-1", launch)
	}
	if env := f.tmux.envVarsFor("legato-" + taskID); env["LEGATO_TASK_EPHEMERAL"] != "1" {
		t.Fatalf("restarted agent is no longer ephemeral: %v", env)
	}
}

func TestRestartAgentLeavesAnAgentItCannotRestartRunning(t *testing.T) {
	for _, why := range []string{"no conversation reported yet", "swarm worker"} {
		t.Run(why, func(t *testing.T) {
			f := newRestartFixture(t)
			ctx := context.Background()
			createTask(t, f.store, "task-r")
			opt := AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: t.TempDir()}
			if why == "swarm worker" {
				createTask(t, f.store, "parent")
				opt.ParentTaskID, opt.Role = "parent", "backend"
			}
			if err := f.svc.SpawnAgent(ctx, "task-r", 0, 0, opt); err != nil {
				t.Fatal(err)
			}
			before, _ := f.store.GetAgentSessionByTaskID(ctx, "task-r")

			err := f.svc.(AgentRestarter).RestartAgent(ctx, "task-r", nil)

			if !errors.Is(err, ErrAgentNotRestartable) {
				t.Fatalf("err = %v, want ErrAgentNotRestartable", err)
			}
			after, err := f.store.GetAgentSessionByTaskID(ctx, "task-r")
			if err != nil || after.ExecutionID != before.ExecutionID || !f.tmux.sessionAlive("legato-task-r") {
				t.Fatalf("agent after a refused restart = %#v, %v; want it still running", after, err)
			}
		})
	}
}

func TestRestartAgentResumesAChimeraAgentsOwnSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := store.New(filepath.Join(t.TempDir(), "restart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: hooks.NewChimeraAdapter("legato"), DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond})
	ctx := context.Background()
	if err := svc.SpawnEphemeralAgent(ctx, "scratch", 0, 0, AgentSpawnOptions{AgentKind: "chimera", WorkingDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	agents, _ := svc.ListAgents(ctx)
	taskID := agents[0].TaskID
	if err := s.UpdateChimeraDiscovery(ctx, taskID, "chimera-session-7", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}

	if err := svc.(AgentRestarter).RestartAgent(ctx, taskID, nil); err != nil {
		t.Fatal(err)
	}

	lines := mt.sentLinesFor("legato-" + taskID)
	if launch := lines[len(lines)-1]; !strings.HasPrefix(launch, "chimera --session 'chimera-session-7'") && !strings.HasPrefix(launch, "chimera --session chimera-session-7") {
		t.Fatalf("launch = %q, want chimera --session chimera-session-7", launch)
	}
}
