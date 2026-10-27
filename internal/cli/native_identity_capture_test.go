package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

// captureTestBridge sends native identities through the same bridge endpoints as
// the provider mocks, and waits for the panel to observe each transition.
func captureTestBridge(t *testing.T, s *store.Store, taskID, kind string) (func(string, string), func() error) {
	t.Helper()
	var endpoint string
	var stop func() error
	var send func(string, string)
	if kind == "claude-code" {
		endpoint, stop = runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: taskID, RuntimeDir: t.TempDir()})
		send = func(id, cwd string) {
			t.Helper()
			events, err := json.Marshal([]map[string]any{{"type": "hello", "sessionId": id, "cwd": cwd}})
			if err != nil {
				t.Fatal(err)
			}
			if code, body := postClaudeIdentity(t, endpoint, string(events)); code != http.StatusNoContent {
				t.Fatalf("identity callback = %d %s", code, body)
			}
		}
	} else {
		app := newCodexAppServer(t)
		_, _, stop = runningCodexBridge(t, s, cli.CodexBridgeOptions{TaskID: taskID, RuntimeDir: t.TempDir(), StartServer: app.start})
		endpoint = boundEndpoint(t, s, taskID)
		send = func(id, cwd string) {
			t.Helper()
			app.terminalOpensThread(map[string]any{"id": id, "cwd": cwd})
		}
	}
	return func(id, cwd string) {
		t.Helper()
		send(id, cwd)
		waitFor(t, "native panel transition", func() bool {
			resp, err := http.Get(endpoint + "/v1/sessions/" + taskID)
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			var snapshot struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			}
			return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&snapshot) == nil && snapshot.ID == taskID && snapshot.CWD == cwd
		})
	}, stop
}

func TestNativeIdentityCaptureStorageRootIsFrozen(t *testing.T) {
	for _, provider := range []struct{ kind, env, dir string }{
		{"claude-code", "CLAUDE_CONFIG_DIR", ".claude"},
		{"codex", "CODEX_HOME", ".codex"},
	} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%v", provider.kind, explicit), func(t *testing.T) {
				t.Setenv("LEGATO_TASK_EPHEMERAL", "")
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv(provider.env, "")
				wantRoot := filepath.Join(home, provider.dir)
				if explicit {
					wantRoot = t.TempDir()
					t.Setenv(provider.env, wantRoot)
				}
				s := newTestStore(t)
				var owner *store.AgentSession
				if provider.kind == "claude-code" {
					owner = seedRunningClaudeAgent(t, s, "task1")
					linkNativeTestExecution(t, s, owner)
				} else {
					seedRunningCodexAgent(t, s, "task1")
					var err error
					owner, err = s.GetAgentSessionByTaskID(context.Background(), "task1")
					if err != nil {
						t.Fatal(err)
					}
				}
				send, stop := captureTestBridge(t, s, "task1", provider.kind)
				// Change both inputs before the first callback: capture must use
				// the startup root, not the environment at first identity discovery.
				t.Setenv("HOME", t.TempDir())
				t.Setenv(provider.env, t.TempDir())
				for i := 1; i <= 2; i++ {
					id, cwd := fmt.Sprintf("provider-%d", i), fmt.Sprintf("/repo/%d", i)
					send(id, cwd)
					waitFor(t, "saved native identity", func() bool {
						record, err := s.GetTaskNativeSession(context.Background(), "task1", provider.kind)
						return err == nil && record != nil && record.ProviderSessionID == id
					})
					record, err := s.GetTaskNativeSession(context.Background(), "task1", provider.kind)
					if err != nil || record == nil || record.StorageDir != wantRoot || record.CWD != cwd || record.AgentSessionID != owner.ID || record.ExecutionID != "native-task1" {
						t.Fatalf("saved identity = %+v, %v; want startup root %q and original owner", record, err, wantRoot)
					}
					t.Setenv("HOME", t.TempDir())
					t.Setenv(provider.env, t.TempDir())
				}
				if err := stop(); err != nil {
					t.Fatal(err)
				}
				assertNativeIdentity(t, s, provider.kind, "provider-2", "/repo/2")
			})
		}
	}
}

func TestNativeIdentityCaptureExcludedOwnersKeepPanelRunning(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		for _, shape := range []string{"child", "subtask", "worker-role", "worker-execution", "other-attribution", "ephemeral-env", "ephemeral-task", "conductor"} {
			t.Run(kind+"/"+shape, func(t *testing.T) {
				t.Setenv("LEGATO_TASK_EPHEMERAL", "")
				t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
				t.Setenv("CODEX_HOME", t.TempDir())
				s := newTestStore(t)
				ctx := context.Background()
				taskID := "task1"
				if shape == "ephemeral-task" {
					var err error
					taskID, err = s.CreateEphemeralTask(ctx, "Temporary native task")
					if err != nil {
						t.Fatal(err)
					}
				} else {
					seedTask(t, s, taskID, "Doing")
				}
				seedTask(t, s, "parent", "Doing")
				a := store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, Command: "shell", AgentKind: kind, Status: "running"}
				e := store.Execution{ID: "native-" + taskID, TaskID: taskID, ExecutionTaskID: taskID, AgentKind: kind, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, StartedAt: time.Now().UTC()}
				parent, subtask := "parent", "subtask"
				switch shape {
				case "child":
					a.ParentTaskID = &parent
				case "subtask":
					a.SubtaskID = &subtask
				case "worker-role":
					a.Role = "worker"
				case "worker-execution":
					e.Kind = store.ExecutionSwarmWorker
				case "other-attribution":
					e.TaskID = parent
				case "ephemeral-env":
					t.Setenv("LEGATO_TASK_EPHEMERAL", "1")
				case "conductor":
					a.Role, a.ParentTaskID = "conductor", &taskID
				}
				ownerID, err := s.InsertAgentSessionReturningID(ctx, a)
				if err != nil {
					t.Fatal(err)
				}
				e.AgentSessionID = &ownerID
				if err := s.CreateExecution(ctx, e); err != nil {
					t.Fatal(err)
				}
				send, stop := captureTestBridge(t, s, taskID, kind)
				for i := 1; i <= 2; i++ {
					id, cwd := fmt.Sprintf("provider-%d", i), fmt.Sprintf("/repo/%d", i)
					send(id, cwd)
					saved := shape == "conductor" || strings.HasPrefix(shape, "ephemeral")
					if saved {
						waitFor(t, shape+" identity", func() bool {
							record, err := s.GetTaskNativeSession(ctx, taskID, kind)
							return err == nil && record != nil && record.ProviderSessionID == id
						})
					}
					record, err := s.GetTaskNativeSession(ctx, taskID, kind)
					if err != nil {
						t.Fatal(err)
					}
					if !saved && record != nil {
						t.Fatalf("excluded %s owner persisted identity: %+v", shape, record)
					}
					bound, err := s.GetAgentSessionByTaskID(ctx, taskID)
					if err != nil || bound == nil || bound.ID != ownerID || bound.ChimeraSessionID == nil || *bound.ChimeraSessionID != taskID || bound.ChimeraEndpoint == nil || *bound.ChimeraEndpoint == "" || bound.Status != "running" {
						t.Fatalf("panel binding after callback = %+v, %v", bound, err)
					}
				}
				if err := stop(); err != nil {
					t.Fatalf("excluded owner stopped bridge with error: %v", err)
				}
				if shape != "conductor" && !strings.HasPrefix(shape, "ephemeral") {
					if record, err := s.GetTaskNativeSession(ctx, taskID, kind); err != nil || record != nil {
						t.Fatalf("excluded identity after bridge shutdown = %+v, %v", record, err)
					}
				}
			})
		}
	}
}
