package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type resumeLaunchTmux struct {
	mockTmuxManager
	command string
	cwd     string
	env     []string
}

func (m *resumeLaunchTmux) Spawn(_ string, cwd string, _, _ int, env ...string) error {
	m.cwd, m.env = cwd, env
	return nil
}
func (m *resumeLaunchTmux) SendKeysShellCommand(_, command string) error {
	m.command = command
	return nil
}

func TestTaskAgentEndpointLaunchesSavedNativeConversation(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		for _, policy := range []string{"resume", "new", ""} {
			t.Run(kind+"/"+policy, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				ctx := context.Background()
				db, err := store.New(filepath.Join(t.TempDir(), "resume.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				if err := db.UpsertTask(ctx, store.Task{ID: "task-resume", Title: "Resume", Status: "Doing", Priority: "medium"}); err != nil {
					t.Fatal(err)
				}
				cwd, providerHome := t.TempDir(), t.TempDir()
				id, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "task-resume", AgentKind: kind, TmuxSession: "old", Status: "running"})
				if err != nil {
					t.Fatal(err)
				}
				if err := db.CreateExecution(ctx, store.Execution{ID: "ex-old", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-resume", ExecutionTaskID: "task-resume", AgentKind: kind, AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.RecordTaskNativeSession(ctx, "task-resume", id, "ex-old", kind, "actual-provider-id", cwd, providerHome); err != nil {
					t.Fatal(err)
				}
				if err := db.FinishAgentSession(ctx, id); err != nil {
					t.Fatal(err)
				}
				mt := &resumeLaunchTmux{}
				var adapter service.AIToolAdapter = hooks.NewClaudeCodeAdapter("legato")
				if kind == "codex" {
					codex := hooks.NewCodexAdapter("legato")
					codex.SetNativeBridge(true)
					adapter = codex
				}
				agents := service.NewAgentService(db, mt, cwd, service.AgentServiceOptions{Adapter: adapter, DisablePeerAwareness: true})
				board := &mockBoardService{cardDetail: &service.CardDetail{ID: "task-resume"}}
				server := New(board, agents, nil, ":0")
				body, err := json.Marshal(map[string]string{"agent_kind": kind, "session_policy": policy})
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/task-resume/agent", strings.NewReader(string(body))))
				if response.Code != http.StatusCreated {
					t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
				}
				if policy != "new" {
					want := "claude --resume actual-provider-id"
					if kind == "codex" {
						want = "legato agent codex -- resume actual-provider-id"
					}
					if !strings.HasPrefix(mt.command, want) || mt.cwd != cwd {
						t.Fatalf("launch = %q in %q, want %q in %q", mt.command, mt.cwd, want, cwd)
					}
					key := "CLAUDE_CONFIG_DIR"
					if kind == "codex" {
						key = "CODEX_HOME"
					}
					if !strings.Contains(strings.Join(mt.env, "\n"), fmt.Sprintf("%s=%s", key, providerHome)) {
						t.Fatalf("provider storage environment = %q", mt.env)
					}
				} else if strings.Contains(mt.command, "actual-provider-id") {
					t.Fatalf("new launch reused provider ID: %s", mt.command)
				}
				saved, err := db.GetTaskNativeSession(ctx, "task-resume", kind)
				if err != nil || saved == nil || saved.ProviderSessionID != "actual-provider-id" {
					t.Fatalf("record lost before provider capture: %#v, %v", saved, err)
				}
			})
		}
	}
}

func TestTaskAgentEndpointReturnsNativeResumeConflict(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		agents := &taskAgentFake{defaultAdapter: kind}
		server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "task-1"}}, agents, nil, ":0")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/agent", strings.NewReader(`{"session_policy":"resume"}`)))
		if response.Code != http.StatusConflict || agents.spawned != "" {
			t.Fatalf("%s: status %d, spawned %s", kind, response.Code, agents.spawned)
		}
	}
	agents := &taskAgentFake{defaultAdapter: "codex", nativeSessions: map[string]*service.TaskResumeSession{"codex": {ID: "saved"}}, spawnErr: fmt.Errorf("%w: wrong worktree", service.ErrTaskResumeConflict)}
	server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "task-1"}}, agents, nil, ":0")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/agent", strings.NewReader(`{"session_policy":"resume"}`)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "wrong worktree") {
		t.Fatalf("status %d, body %s", response.Code, response.Body.String())
	}
}

func nativeResumeStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.UpsertTask(context.Background(), store.Task{ID: "task-resume", Title: "Resume", Status: "Doing", Priority: "medium"}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedNativeResumeRecord(t *testing.T, db *store.Store, kind string) *store.TaskNativeSession {
	t.Helper()
	ctx := context.Background()
	if err := db.DeleteDeadAgentSessions(ctx, "task-resume"); err != nil {
		t.Fatal(err)
	}
	executionID := "ex-saved-" + kind
	id, err := db.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "task-resume", AgentKind: kind, ExecutionID: executionID, TmuxSession: "old", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateExecution(ctx, store.Execution{ID: executionID, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task-resume", ExecutionTaskID: "task-resume", AgentKind: kind, AgentSessionID: &id, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordTaskNativeSession(ctx, "task-resume", id, executionID, kind, "saved-"+kind, t.TempDir(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	finishNativeResumeAgent(t, db)
	return readNativeResumeRecord(t, db, kind)
}

func readNativeResumeRecord(t *testing.T, db *store.Store, kind string) *store.TaskNativeSession {
	t.Helper()
	record, err := db.GetTaskNativeSession(context.Background(), "task-resume", kind)
	if err != nil || record == nil {
		t.Fatalf("saved %s record = %#v, %v", kind, record, err)
	}
	return record
}

func finishNativeResumeAgent(t *testing.T, db *store.Store) {
	t.Helper()
	ctx := context.Background()
	session, err := db.GetAgentSessionByTaskID(ctx, "task-resume")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.TerminalizeExecution(ctx, session.ExecutionID, store.ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishAgentSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
}

func postNativeResumeAgent(t *testing.T, server *Server, body string) {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/task-resume/agent", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func nativeResumeEnv(env []string) map[string]string {
	values := make(map[string]string)
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	return values
}

func TestTaskAgentNativeResumeSwitchesConfiguredProvidersWithoutOverwrite(t *testing.T) {
	for _, defaultKind := range []string{"claude-code", "codex"} {
		t.Run(defaultKind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			db := nativeResumeStore(t)
			saved := map[string]*store.TaskNativeSession{}
			for _, kind := range []string{"claude-code", "codex"} {
				saved[kind] = seedNativeResumeRecord(t, db, kind)
			}
			claude := hooks.NewClaudeCodeAdapter("legato")
			codex := hooks.NewCodexAdapter("legato")
			codex.SetNativeBridge(true)
			adapters := map[string]service.AIToolAdapter{"claude-code": claude, "codex": codex}
			mt := &resumeLaunchTmux{}
			agents := service.NewAgentService(db, mt, t.TempDir(), service.AgentServiceOptions{Adapter: adapters[defaultKind], Adapters: adapters, DisablePeerAwareness: true})
			server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "task-resume"}}, agents, nil, ":0")
			otherKind := "codex"
			if defaultKind == otherKind {
				otherKind = "claude-code"
			}
			for step, request := range []struct{ body, kind string }{
				{`{}`, defaultKind},
				{fmt.Sprintf(`{"agent_kind":%q}`, otherKind), otherKind},
				{`{"session_policy":"resume"}`, defaultKind},
				{fmt.Sprintf(`{"agent_kind":%q,"session_policy":"resume"}`, otherKind), otherKind},
			} {
				postNativeResumeAgent(t, server, request.body)
				selected := saved[request.kind]
				prefix, storageKey, otherKey := "claude --resume ", "CLAUDE_CONFIG_DIR", "CODEX_HOME"
				if request.kind == "codex" {
					prefix, storageKey, otherKey = "legato agent codex -- resume ", "CODEX_HOME", "CLAUDE_CONFIG_DIR"
				}
				if mt.command != prefix+selected.ProviderSessionID || mt.cwd != selected.CWD {
					t.Fatalf("step %d: launch = %q in %q, saved = %#v", step, mt.command, mt.cwd, selected)
				}
				env := nativeResumeEnv(mt.env)
				if env[storageKey] != selected.StorageDir || env[otherKey] != "" || env["LEGATO_RESUME_SESSION_ID"] != selected.ProviderSessionID || env["LEGATO_SESSION_POLICY"] != "resume" {
					t.Fatalf("step %d: wrong provider environment: %v", step, env)
				}
				for kind, record := range saved {
					if actual := readNativeResumeRecord(t, db, kind); !reflect.DeepEqual(actual, record) {
						t.Fatalf("launch overwrote %s record: %#v, want %#v", kind, actual, record)
					}
				}
				// Simulate capture by the newly launched owner, then switch providers.
				session, err := db.GetAgentSessionByTaskID(context.Background(), "task-resume")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.RecordTaskNativeSession(context.Background(), "task-resume", session.ID, session.ExecutionID, request.kind, fmt.Sprintf("captured-%s-%d", request.kind, step), selected.CWD, selected.StorageDir); err != nil {
					t.Fatal(err)
				}
				saved[request.kind] = readNativeResumeRecord(t, db, request.kind)
				for kind, record := range saved {
					if actual := readNativeResumeRecord(t, db, kind); !reflect.DeepEqual(actual, record) {
						t.Fatalf("provider capture overwrote %s record: %#v, want %#v", kind, actual, record)
					}
				}
				finishNativeResumeAgent(t, db)
			}
		})
	}
}

// The shell survives a provider exit, as a tmux pane would. Only the temporary
// provider runs: no tmux, CLI bridge, or live provider is involved here.
type nativeResumeProcessTmux struct {
	resumeLaunchTmux
	pane, stderr bytes.Buffer
	commands     []string
	exitErr      error
}

func (m *nativeResumeProcessTmux) SendKeysShellCommand(_ string, command string) error {
	m.command = command
	m.commands = append(m.commands, command)
	m.pane.Reset()
	m.stderr.Reset()
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = m.cwd
	cmd.Env = append(os.Environ(), m.env...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &m.stderr
	m.exitErr = cmd.Run()
	m.pane.Write(stdout.Bytes())
	m.pane.Write(m.stderr.Bytes())
	return nil
}

func (m *nativeResumeProcessTmux) CaptureWithEscapes(_ string) (string, error) {
	return m.pane.String(), nil
}

func TestTaskAgentNativeResumeMissingTranscriptRequiresExplicitNew(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			db := nativeResumeStore(t)
			saved := seedNativeResumeRecord(t, db, kind)
			binDir := t.TempDir()
			tool := "claude"
			var adapter service.AIToolAdapter = hooks.NewClaudeCodeAdapter("legato")
			if kind == "codex" {
				tool, adapter = "codex", hooks.NewCodexAdapter("legato")
			}
			// Direct-provider Codex mode deliberately excludes the CLI bridge;
			// this test exercises the API/store/adapter command and pane boundary.
			script := `#!/bin/sh
case "$1" in
  --resume|resume)
    printf 'missing transcript: %s\n' "$2" >&2
    exit 42
    ;;
  *) printf 'fresh conversation launched\n' ;;
esac
`
			if err := os.WriteFile(filepath.Join(binDir, tool), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			freshCWD := t.TempDir()
			mt := &nativeResumeProcessTmux{}
			agents := service.NewAgentService(db, mt, freshCWD, service.AgentServiceOptions{Adapter: adapter, DisablePeerAwareness: true})
			server := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "task-resume"}}, agents, nil, ":0")
			for _, body := range []string{`{}`, `{"session_policy":"resume"}`, `{}`} {
				postNativeResumeAgent(t, server, body)
				want := "missing transcript: " + saved.ProviderSessionID + "\n"
				if exit, ok := mt.exitErr.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
					t.Fatalf("provider exit = %v, want 42", mt.exitErr)
				}
				pane, err := agents.CaptureOutput(context.Background(), "task-resume")
				if err != nil || pane != want || mt.stderr.String() != want {
					t.Fatalf("pane = %q, stderr = %q, error = %v", pane, mt.stderr.String(), err)
				}
				// An existing failed-provider pane stays visible rather than
				// silently launching a fresh conversation on another API request.
				response := httptest.NewRecorder()
				count := len(mt.commands)
				server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/task-resume/agent", strings.NewReader(`{}`)))
				if response.Code != http.StatusOK || len(mt.commands) != count {
					t.Fatalf("existing pane: status %d, commands %q", response.Code, mt.commands)
				}
				if actual := readNativeResumeRecord(t, db, kind); !reflect.DeepEqual(actual, saved) {
					t.Fatalf("failed resume changed durable record: %#v, want %#v", actual, saved)
				}
				finishNativeResumeAgent(t, db)
			}
			prefix := tool + " --resume "
			if kind == "codex" {
				prefix = tool + " resume "
			}
			if len(mt.commands) != 3 {
				t.Fatalf("unexpected fallback commands: %q", mt.commands)
			}
			for _, command := range mt.commands {
				if command != prefix+saved.ProviderSessionID {
					t.Fatalf("fresh or wrong resume command without New: %q", command)
				}
			}
			postNativeResumeAgent(t, server, `{"session_policy":"new"}`)
			pane, err := agents.CaptureOutput(context.Background(), "task-resume")
			if err != nil || pane != "fresh conversation launched\n" || mt.exitErr != nil || mt.stderr.Len() != 0 || len(mt.commands) != 4 || mt.command != tool || mt.cwd != freshCWD {
				t.Fatalf("New: pane %q, error %v, exit %v, stderr %q, commands %q, cwd %q", pane, err, mt.exitErr, mt.stderr.String(), mt.commands, mt.cwd)
			}
			env := nativeResumeEnv(mt.env)
			if env["LEGATO_SESSION_POLICY"] != "new" || env["LEGATO_RESUME_SESSION_ID"] != "" || env["CLAUDE_CONFIG_DIR"] != "" || env["CODEX_HOME"] != "" {
				t.Fatalf("New retained resume environment: %v", env)
			}
			if actual := readNativeResumeRecord(t, db, kind); !reflect.DeepEqual(actual, saved) {
				t.Fatalf("New erased old record before capture: %#v, want %#v", actual, saved)
			}
		})
	}
}
