package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// Only terminal/process boundaries are replaced. Store, hosts, adapters,
// task-agent handler, and panel snapshot proxy are the production code.
type resumeFlowTerminal struct {
	service.TmuxManager
	command string
	cwd     string
	env     []string
}

func (m *resumeFlowTerminal) Spawn(_ string, cwd string, _, _ int, env ...string) error {
	m.cwd, m.env = cwd, env
	return nil
}
func (m *resumeFlowTerminal) Kill(string) error            { return nil }
func (m *resumeFlowTerminal) IsAlive(string) (bool, error) { return false, nil }
func (m *resumeFlowTerminal) SendKeysShellCommand(_, command string) error {
	m.command = command
	return nil
}
func (m *resumeFlowTerminal) SetOption(string, string, string) error { return nil }

func TestNativeCaptureToTaskResumeToPanelHistory(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("LEGATO_TASK_EPHEMERAL", "")
			cwd, originalStorage := t.TempDir(), t.TempDir()
			storageKey := "CLAUDE_CONFIG_DIR"
			if kind == "codex" {
				storageKey = "CODEX_HOME"
			}
			t.Setenv(storageKey, originalStorage)
			db := newTestStore(t)
			if kind == "codex" {
				seedRunningCodexAgent(t, db, "task1")
			} else {
				owner := seedRunningClaudeAgent(t, db, "task1")
				linkNativeTestExecution(t, db, owner)
			}
			providerID := "recorded-" + kind
			startProvider := func(selectedID string) func() error {
				t.Helper()
				if kind == "claude-code" {
					endpoint, stop := runningBridge(t, db, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir()})
					messages := []map[string]string{}
					if selectedID == providerID {
						messages = []map[string]string{{"role": "user", "text": "previous question"}, {"role": "assistant", "text": "previous answer"}}
					}
					events, err := json.Marshal([]any{map[string]any{"type": "hello", "sessionId": selectedID, "cwd": cwd, "messages": messages}})
					if err != nil {
						t.Fatal(err)
					}
					if code, body := postClaudeIdentity(t, endpoint, string(events)); code != http.StatusNoContent {
						t.Fatalf("provider hello = %d %s", code, body)
					}
					return stop
				}
				codex := newCodexAppServer(t)
				codex.history = map[string]any{providerID: map[string]any{"id": providerID, "cwd": cwd, "turns": []any{map[string]any{"id": "previous-turn", "status": "completed", "items": []any{map[string]any{"id": "a1", "type": "agentMessage", "text": "previous answer"}}}}}}
				_, _, stop := runningCodexBridge(t, db, cli.CodexBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir(), StartServer: codex.start})
				codex.terminalOpensThread(map[string]any{"id": selectedID, "cwd": cwd})
				waitFor(t, "durable provider identity", func() bool {
					saved, err := db.GetTaskNativeSession(context.Background(), "task1", kind)
					return err == nil && saved != nil && saved.ProviderSessionID == selectedID
				})
				return stop
			}
			stop := startProvider(providerID)
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			saved, err := db.GetTaskNativeSession(context.Background(), "task1", kind)
			if err != nil || saved == nil || saved.StorageDir != originalStorage {
				t.Fatalf("captured provider storage = %#v, %v", saved, err)
			}
			owner, err := db.GetAgentSessionByTaskID(context.Background(), "task1")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.FinishAgentSession(context.Background(), owner.ID); err != nil {
				t.Fatal(err)
			}
			t.Setenv(storageKey, t.TempDir())
			terminal := &resumeFlowTerminal{}
			var adapter service.AIToolAdapter = hooks.NewClaudeCodeAdapter("legato")
			if kind == "codex" {
				codex := hooks.NewCodexAdapter("legato")
				codex.SetNativeBridge(true)
				adapter = codex
			}
			agents := service.NewAgentService(db, terminal, cwd, service.AgentServiceOptions{Adapter: adapter, DisablePeerAwareness: true})
			api := server.New(service.NewBoardService(db, nil), agents, nil, ":0").Handler()
			response := httptest.NewRecorder()
			api.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tasks/task1/agent", strings.NewReader(`{"session_policy":"resume"}`)))
			if response.Code != http.StatusCreated {
				t.Fatalf("resume request = %d %s", response.Code, response.Body.String())
			}
			selector := "--resume"
			if kind == "codex" {
				selector = "resume"
			}
			args := strings.Fields(terminal.command)
			selected := ""
			for i, arg := range args {
				if arg == selector && i+1 < len(args) {
					selected = args[i+1]
					break
				}
			}
			if selected != providerID || terminal.cwd != cwd || !strings.Contains(strings.Join(terminal.env, "\n"), storageKey+"="+originalStorage) {
				t.Fatalf("resume command/context = %s, %s, %v", terminal.command, terminal.cwd, terminal.env)
			}
			// The simulated provider consumes the selector emitted by the real
			// launch path, rather than a separate hardcoded resume fixture.
			for _, variable := range terminal.env {
				key, value, _ := strings.Cut(variable, "=")
				if key == storageKey {
					t.Setenv(key, value)
				}
			}
			stop = startProvider(selected)
			defer func() {
				if err := stop(); err != nil {
					t.Error(err)
				}
			}()
			waitFor(t, "resumed history through panel proxy", func() bool {
				panel := httptest.NewRecorder()
				api.ServeHTTP(panel, httptest.NewRequest(http.MethodGet, "/api/agents/task1/chimera/snapshot", nil))
				return panel.Code == http.StatusOK && strings.Contains(panel.Body.String(), "previous answer")
			})
			bound, err := db.GetAgentSessionByTaskID(context.Background(), "task1")
			if err != nil || bound.ChimeraSessionID == nil || *bound.ChimeraSessionID != "task1" {
				t.Fatalf("task binding = %+v, %v", bound, err)
			}
			if got := fmt.Sprint(bound.AgentKind); got != kind {
				t.Fatalf("bound provider = %s", got)
			}
		})
	}
}
