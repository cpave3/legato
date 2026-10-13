package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

func savedNativeConversation(t *testing.T, s *store.Store, taskID, kind, cwd, storageDir string) {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, AgentKind: kind, ExecutionID: "ex-old", TmuxSession: "old", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-old", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, AgentKind: kind, AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordTaskNativeSession(ctx, taskID, id, "ex-old", kind, "saved-conversation", cwd, storageDir); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentSessionStatus(ctx, taskID, "dead"); err != nil {
		t.Fatal(err)
	}
}

func TestSpawnAgentResumesNativeConversationWithoutFreshKickoff(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			s, err := store.New(filepath.Join(t.TempDir(), "resume.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			createTask(t, s, "task-resume")
			cwd, storageDir := t.TempDir(), t.TempDir()
			savedNativeConversation(t, s, "task-resume", kind, cwd, storageDir)
			mt := newMockTmux()
			var adapter AIToolAdapter = hooks.NewClaudeCodeAdapter("legato")
			want := "claude --resume saved-conversation"
			storageKey := "CLAUDE_CONFIG_DIR"
			if kind == "codex" {
				codex := hooks.NewCodexAdapter("legato")
				codex.SetNativeBridge(true)
				adapter, want, storageKey = codex, "legato agent codex -- resume saved-conversation", "CODEX_HOME"
			}
			svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{Adapter: adapter, DisablePeerAwareness: true, BriefKickoffDelay: time.Millisecond})
			svc.(*agentService).nativeBindTimeout = time.Millisecond
			if err := svc.SpawnAgent(context.Background(), "task-resume", 0, 0, AgentSpawnOptions{SessionPolicy: "resume", Brief: "fresh kickoff must not be sent"}); err != nil {
				t.Fatal(err)
			}
			lines := mt.sentLinesFor("legato-task-resume")
			if len(lines) != 1 || !strings.HasPrefix(lines[0], want) {
				t.Fatalf("launch lines = %q, want %s", lines, want)
			}
			if strings.Contains(lines[0], "developer_instructions") || strings.Contains(lines[0], "append-system-prompt") {
				t.Fatalf("fresh prompt was injected: %s", lines[0])
			}
			env := mt.envVarsFor("legato-task-resume")
			if env[storageKey] != storageDir || env["LEGATO_RESUME_SESSION_ID"] != "saved-conversation" {
				t.Fatalf("resume environment = %v", env)
			}
			execution, err := s.GetExecution(context.Background(), env["LEGATO_EXECUTION_ID"])
			if err != nil || execution.WorkingDir != cwd {
				t.Fatalf("execution = %#v, %v", execution, err)
			}
			time.Sleep(30 * time.Millisecond)
			if lines := mt.sentLinesFor("legato-task-resume"); len(lines) != 1 {
				t.Fatalf("resume replayed kickoff: %q", lines)
			}
		})
	}
}

func TestSpawnAgentRejectsIncompatibleNativeResumeBeforeLaunch(t *testing.T) {
	for _, failure := range []string{"missing-record", "wrong-id", "different-directory", "deleted-directory", "ephemeral", "wrong-adapter", "wrong-adapter-new"} {
		t.Run(failure, func(t *testing.T) {
			s, err := store.New(filepath.Join(t.TempDir(), "resume.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Close() })
			createTask(t, s, "task-resume")
			cwd := t.TempDir()
			if failure != "missing-record" {
				savedNativeConversation(t, s, "task-resume", "codex", cwd, "")
			}
			opt := AgentSpawnOptions{AgentKind: "codex", SessionPolicy: "resume"}
			adapter := AIToolAdapter(hooks.NewCodexAdapter("legato"))
			switch failure {
			case "wrong-id":
				opt.ResumeSessionID = "another-conversation"
			case "different-directory":
				opt.WorkingDir = t.TempDir()
			case "deleted-directory":
				if err := os.Remove(cwd); err != nil {
					t.Fatal(err)
				}
			case "ephemeral":
				opt.Ephemeral = true
			case "wrong-adapter":
				adapter = hooks.NewClaudeCodeAdapter("legato")
			case "wrong-adapter-new":
				adapter = hooks.NewClaudeCodeAdapter("legato")
				opt.SessionPolicy = "new"
			}
			mt := newMockTmux()
			svc := NewAgentService(s, mt, cwd, AgentServiceOptions{Adapter: adapter, DisablePeerAwareness: true})
			if err := svc.SpawnAgent(context.Background(), "task-resume", 0, 0, opt); !errors.Is(err, ErrTaskResumeConflict) {
				t.Fatalf("resume error = %v", err)
			}
			if mt.sessionAlive("legato-task-resume") {
				t.Fatal("invalid resume launched a terminal")
			}
		})
	}
}

func TestNativeHistoryDoesNotChangeWorkflowLaunchDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := store.New(filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTask(t, s, "task-resume")
	cwd := t.TempDir()
	savedNativeConversation(t, s, "task-resume", "codex", cwd, "")
	mt := newMockTmux()
	svc := NewAgentService(s, mt, cwd, AgentServiceOptions{Adapter: hooks.NewCodexAdapter("legato"), DisablePeerAwareness: true})
	if err := svc.SpawnAgent(context.Background(), "task-resume", 0, 0, AgentSpawnOptions{ExecutionKind: store.ExecutionWorkflowAgent}); err != nil {
		t.Fatal(err)
	}
	if cmd := mt.sentLinesFor("legato-task-resume"); len(cmd) != 1 || strings.Contains(cmd[0], " resume ") {
		t.Fatalf("workflow launch = %q", cmd)
	}
}

func TestSpawnAgentRejectsConfiguredNativeSessionOverride(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		for _, tier := range []string{"", "selected"} {
			t.Run(kind+"/"+tier, func(t *testing.T) {
				s, err := store.New(filepath.Join(t.TempDir(), "resume.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.Close() })
				createTask(t, s, "task-resume")
				cwd := t.TempDir()
				savedNativeConversation(t, s, "task-resume", kind, cwd, t.TempDir())
				var adapter AIToolAdapter
				flag := "--worktree"
				if kind == "claude-code" {
					a := hooks.NewClaudeCodeAdapter("legato")
					if tier == "" {
						a.SetLaunchArgs([]string{flag})
					} else {
						a.SetTiers(map[string][]string{tier: {flag}})
					}
					adapter = a
				} else {
					flag = "--cd=/different/workspace"
					a := hooks.NewCodexAdapter("legato")
					if tier == "" {
						a.SetLaunchArgs([]string{flag})
					} else {
						a.SetTiers(map[string][]string{tier: {flag}})
					}
					adapter = a
				}
				mt := newMockTmux()
				svc := NewAgentService(s, mt, cwd, AgentServiceOptions{Adapter: adapter, DisablePeerAwareness: true})
				err = svc.SpawnAgent(context.Background(), "task-resume", 0, 0, AgentSpawnOptions{SessionPolicy: "resume", Tier: tier})
				if !errors.Is(err, ErrTaskResumeConflict) || !strings.Contains(err.Error(), flag) {
					t.Fatalf("configured override error = %v", err)
				}
				if mt.sessionAlive("legato-task-resume") {
					t.Fatal("invalid resume launched a terminal")
				}
			})
		}
	}
}
