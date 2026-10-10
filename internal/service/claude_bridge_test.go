package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// bridgedClaudeAgent is a running Claude Code agent whose bridge is bound to
// its agent session, as `legato agent bridge` leaves it.
type bridgedClaudeAgent struct {
	t        *testing.T
	svc      AgentService
	store    *store.Store
	endpoint string
	commands chan map[string]any
}

type lineWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	lines chan map[string]any
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadBytes('\n')
		if err != nil {
			w.buf.Write(line)
			break
		}
		var command map[string]any
		if json.Unmarshal(line, &command) == nil {
			w.lines <- command
		}
	}
	return len(p), nil
}

func newBridgedClaudeAgent(t *testing.T, taskID string) *bridgedClaudeAgent {
	t.Helper()
	svc, s, _ := newDiscoveryTestService(t)
	createTask(t, s, taskID)
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{
		TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: "claude-code", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	commands := &lineWriter{lines: make(chan map[string]any, 32)}
	bridge := claudebridge.New(claudebridge.Options{TaskID: taskID, Token: "secret", Commands: commands})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	endpoint := server.URL + "/secret"
	if err := s.UpdateChimeraDiscovery(ctx, taskID, taskID, endpoint); err != nil {
		t.Fatal(err)
	}
	agent := &bridgedClaudeAgent{t: t, svc: svc, store: s, endpoint: endpoint, commands: commands.lines}
	agent.mod(`{"type":"hello","sessionId":"claude-1","cwd":` + jsonString(t.TempDir()) + `,"model":"claude-opus-5-5"}`)
	return agent
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// mod posts engine events the way the Claude Code mod does.
func (a *bridgedClaudeAgent) mod(events ...string) {
	a.t.Helper()
	resp, err := http.Post(a.endpoint+"/mod/events", "application/json", strings.NewReader(`{"events":[`+strings.Join(events, ",")+`]}`))
	if err != nil {
		a.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		a.t.Fatalf("POST /mod/events = %d", resp.StatusCode)
	}
}

func (a *bridgedClaudeAgent) nextCommand() map[string]any {
	a.t.Helper()
	select {
	case command := <-a.commands:
		return command
	case <-time.After(2 * time.Second):
		a.t.Fatal("timed out waiting for a command to the mod")
		return nil
	}
}

func TestMessageToABridgedClaudeAgentIsSubmittedDirectly(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")

	if err := agent.svc.SendMessage(context.Background(), "TASK-42", "run the linter"); err != nil {
		t.Fatal(err)
	}

	if command := agent.nextCommand(); command["type"] != "submit" || command["text"] != "run the linter" {
		t.Fatalf("command = %v, want the message submitted as a prompt rather than typed into tmux", command)
	}
}

func TestMessageToABusyBridgedClaudeAgentJoinsItsTurn(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")
	agent.mod(`{"type":"turn_start","turnId":"turn-1"}`)
	queuer, ok := agent.svc.(AgentMessageQueuer)
	if !ok {
		t.Fatal("agent service cannot queue messages")
	}

	// Both delivery paths reach a busy session: Claude Code folds the
	// message into the running turn instead of refusing it.
	if err := queuer.QueueMessage(context.Background(), "TASK-42", "also update the docs"); err != nil {
		t.Fatal(err)
	}
	if command := agent.nextCommand(); command["type"] != "queue" || command["text"] != "also update the docs" {
		t.Fatalf("queued command = %v", command)
	}
	if err := agent.svc.SendMessage(context.Background(), "TASK-42", "[swarm event] worker built"); err != nil {
		t.Fatalf("SendMessage to a busy session: %v", err)
	}
	if command := agent.nextCommand(); command["type"] != "queue" || command["text"] != "[swarm event] worker built" {
		t.Fatalf("sent command = %v", command)
	}
}

func TestAttachmentsReachABridgedClaudeAgentAheadOfTheMessage(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")
	// The session's working directory comes from the bridge snapshot.
	var snapshot struct {
		CWD string `json:"cwd"`
	}
	resp, err := http.Get(agent.endpoint + "/v1/sessions/TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := os.WriteFile(filepath.Join(snapshot.CWD, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := agent.svc.SendMessage(context.Background(), "TASK-42", "explain @main.go"); err != nil {
		t.Fatal(err)
	}

	attached, sent := agent.nextCommand(), agent.nextCommand()
	text, _ := attached["text"].(string)
	if attached["type"] != "append" || !strings.Contains(text, "package main") {
		t.Fatalf("attachment command = %v", attached)
	}
	if sent["type"] != "submit" || sent["text"] != "explain @main.go" {
		t.Fatalf("message command = %v", sent)
	}
}

func TestInterruptReachesABridgedClaudeAgent(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")
	agent.mod(`{"type":"turn_start","turnId":"turn-1"}`)

	if err := agent.svc.InterruptAgent(context.Background(), "TASK-42"); err != nil {
		t.Fatal(err)
	}

	if command := agent.nextCommand(); command["type"] != "interrupt" {
		t.Fatalf("command = %v", command)
	}
}

func TestPermissionDecisionReachesABridgedClaudeAgent(t *testing.T) {
	agent := newBridgedClaudeAgent(t, "TASK-42")
	// A native viewer is watching, so the bridge holds the mod's request.
	ctx, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, agent.endpoint+"/v1/sessions/TASK-42/events", nil)
	watching, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer watching.Body.Close()
	events := bufio.NewScanner(watching.Body)

	answer := make(chan string, 1)
	go func() {
		resp, err := http.Post(agent.endpoint+"/mod/permissions", "application/json",
			strings.NewReader(`{"requestId":"permission-1","tool":"Bash","input":{"command":"git push"}}`))
		if err != nil {
			answer <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		var decided struct {
			Decision string `json:"decision"`
			Feedback string `json:"feedback"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&decided)
		answer <- decided.Decision + ":" + decided.Feedback
	}()
	// Wait until the request is open before deciding it.
	for events.Scan() {
		if strings.Contains(events.Text(), `"permission_request"`) {
			break
		}
	}

	err = agent.svc.ResolvePermission(context.Background(), "TASK-42", "permission-1", PermissionAction{Decision: "deny", Feedback: "not on a Friday"})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-answer:
		if got != "deny:not on a Friday" {
			t.Fatalf("answer to the mod = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the mod's request was never answered")
	}
}

func TestDiscoverRebindsAClaudeCodeAgentToItsRunningBridge(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	svc, s, publisher := newDiscoveryTestService(t)
	createTask(t, s, "TASK-42")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "claude-code", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	bridge := claudebridge.New(claudebridge.Options{TaskID: "TASK-42", Token: "secret"})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	endpoint := server.URL + "/secret"
	// The bridge keeps advertising itself after a downgrade to tmux.
	if err := claudebridge.WriteLockfile(ipc.SocketDir(), "TASK-42", claudebridge.Lockfile{SessionID: "TASK-42", URL: endpoint}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DiscoverChimera(context.Background(), "TASK-42"); err != nil {
		t.Fatal(err)
	}

	session, err := s.GetAgentSessionByTaskID(context.Background(), "TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	if session.ChimeraEndpoint == nil || *session.ChimeraEndpoint != endpoint || session.ChimeraSessionID == nil || *session.ChimeraSessionID != "TASK-42" {
		t.Fatalf("binding = %v / %v", session.ChimeraEndpoint, session.ChimeraSessionID)
	}
	if len(publisher.discoveries) != 1 || publisher.discoveries[0] != "TASK-42" {
		t.Fatalf("discoveries = %v", publisher.discoveries)
	}
}

func TestDiscoverLeavesAClaudeCodeAgentAloneWithoutAUsableBridge(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	otherTask := claudebridge.New(claudebridge.Options{TaskID: "OTHER-1"})
	otherServer := httptest.NewServer(otherTask.Handler())
	t.Cleanup(func() {
		otherTask.Close()
		otherServer.Close()
	})

	tests := []struct {
		name     string
		lockfile *claudebridge.Lockfile
	}{
		{name: "no bridge was ever started"},
		{name: "the bridge has exited", lockfile: &claudebridge.Lockfile{SessionID: "TASK-42", URL: goneURL}},
		{name: "the lockfile points off this machine", lockfile: &claudebridge.Lockfile{SessionID: "TASK-42", URL: "http://example.com/x"}},
		{name: "the endpoint serves another task", lockfile: &claudebridge.Lockfile{SessionID: "TASK-42", URL: otherServer.URL}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			svc, s, publisher := newDiscoveryTestService(t)
			createTask(t, s, "TASK-42")
			if err := s.InsertAgentSession(context.Background(), store.AgentSession{
				TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "claude-code", Status: "running",
			}); err != nil {
				t.Fatal(err)
			}
			if tt.lockfile != nil {
				if err := claudebridge.WriteLockfile(ipc.SocketDir(), "TASK-42", *tt.lockfile); err != nil {
					t.Fatal(err)
				}
			}

			err := svc.DiscoverChimera(context.Background(), "TASK-42")

			if !errors.Is(err, ErrChimeraDiscoveryNotFound) {
				t.Fatalf("err = %v, want not found", err)
			}
			session, _ := s.GetAgentSessionByTaskID(context.Background(), "TASK-42")
			if session.ChimeraEndpoint != nil || len(publisher.discoveries) != 0 {
				t.Fatalf("failed discovery bound the agent: %v", session.ChimeraEndpoint)
			}
		})
	}
}
