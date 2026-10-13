package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/store"
)

func seedRunningClaudeAgent(t *testing.T, s *store.Store, taskID string) *store.AgentSession {
	t.Helper()
	seedTask(t, s, taskID, "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{
		TaskID: taskID, TmuxSession: "legato-" + taskID, Command: "shell", AgentKind: "claude-code", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func linkNativeTestExecution(t *testing.T, s *store.Store, session *store.AgentSession) {
	t.Helper()
	if err := s.CreateExecution(context.Background(), store.Execution{
		ID: "native-" + session.TaskID, AgentKind: session.AgentKind,
		Kind: store.ExecutionInteractive, Status: store.ExecutionRunning,
		TaskID: session.TaskID, ExecutionTaskID: session.TaskID,
		AgentSessionID: &session.ID, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
}

// runningBridge starts the bridge command and returns the endpoint it
// announced to the mod, with a stop function that waits for it to exit.
func runningBridge(t *testing.T, s *store.Store, opts cli.ClaudeBridgeOptions) (endpoint string, stop func() error) {
	t.Helper()
	reader, writer := io.Pipe()
	opts.Stdout = writer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := cli.RunClaudeBridge(ctx, s, opts)
		writer.CloseWithError(err)
		done <- err
	}()

	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
		_, _ = io.Copy(io.Discard, reader)
	}()
	stopped := false
	stop = func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("bridge did not stop")
			return nil
		}
	}
	t.Cleanup(func() { _ = stop() })

	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatalf("bridge exited before announcing itself: %v", <-done)
		}
		var ready struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		}
		if err := json.Unmarshal([]byte(line), &ready); err != nil || ready.Type != "ready" || ready.URL == "" {
			t.Fatalf("first command = %q, want the ready announcement", line)
		}
		return ready.URL, stop
	case <-time.After(5 * time.Second):
		t.Fatal("bridge never announced itself")
		return "", stop
	}
}

func TestClaudeBridgeBindsTheRunningAgentForAsLongAsItRuns(t *testing.T) {
	s := newTestStore(t)
	seedRunningClaudeAgent(t, s, "task1")
	runtimeDir := t.TempDir()

	endpoint, stop := runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: runtimeDir})

	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatalf("endpoint = %q, want a loopback URL", endpoint)
	}
	session, err := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	if session.ChimeraEndpoint == nil || *session.ChimeraEndpoint != endpoint || session.ChimeraSessionID == nil || *session.ChimeraSessionID != "task1" {
		t.Fatalf("binding = %v / %v, want the bridge endpoint under the task", session.ChimeraEndpoint, session.ChimeraSessionID)
	}

	// The endpoint serves the native protocol under the task's name, which
	// is what Legato's discovery verifies.
	resp, err := http.Get(endpoint + "/v1/sessions/task1")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if snapshot.Name != "task1" {
		t.Fatalf("snapshot name = %q", snapshot.Name)
	}

	lockfile, err := claudebridge.ReadLockfile(runtimeDir, "task1")
	if err != nil || lockfile.URL != endpoint || lockfile.SessionID != "task1" {
		t.Fatalf("lockfile = %+v, %v", lockfile, err)
	}

	if err := stop(); err != nil {
		t.Fatalf("bridge exit: %v", err)
	}
	session, err = s.GetAgentSessionByTaskID(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	if session.ChimeraEndpoint != nil || session.ChimeraSessionID != nil {
		t.Fatalf("binding survived the bridge: %v / %v", session.ChimeraEndpoint, session.ChimeraSessionID)
	}
	if _, err := claudebridge.ReadLockfile(runtimeDir, "task1"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lockfile survived the bridge: %v", err)
	}
}

func TestClaudeBridgeRefusesATaskWithNoRunningAgent(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "task1", "Doing")

	err := cli.RunClaudeBridge(context.Background(), s, cli.ClaudeBridgeOptions{TaskID: "task1", Stdout: io.Discard, RuntimeDir: t.TempDir()})

	if err == nil || !strings.Contains(err.Error(), "task1") {
		t.Fatalf("err = %v, want a refusal naming the task", err)
	}
}

func TestClaudeBridgeRefusesAnExecutionThatDoesNotOwnTheAgent(t *testing.T) {
	s := newTestStore(t)
	session := seedRunningClaudeAgent(t, s, "task1")
	ctx := context.Background()
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-live", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: "task1", ExecutionTaskID: "task1", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkExecutionAgentSession(ctx, "ex-live", session.ID); err != nil {
		t.Fatal(err)
	}

	err := cli.RunClaudeBridge(ctx, s, cli.ClaudeBridgeOptions{TaskID: "task1", ExecutionID: "ex-stale", Stdout: io.Discard, RuntimeDir: t.TempDir()})

	if err == nil || !strings.Contains(err.Error(), "ex-stale") {
		t.Fatalf("err = %v, want a refusal naming the stale execution", err)
	}
}

func TestClaudeBridgeDoesNotTakeOverALiveBinding(t *testing.T) {
	s := newTestStore(t)
	session := seedRunningClaudeAgent(t, s, "task1")
	// Another Claude process inside the same Legato session (a nested run)
	// must not steal the task from the bridge already serving it.
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pid":1}`))
	}))
	defer live.Close()
	if err := s.UpdateChimeraDiscoveryForSession(context.Background(), session.ID, "task1", live.URL); err != nil {
		t.Fatal(err)
	}

	err := cli.RunClaudeBridge(context.Background(), s, cli.ClaudeBridgeOptions{
		TaskID: "task1", Stdout: io.Discard, RuntimeDir: t.TempDir(), HandoverWait: 50 * time.Millisecond,
	})

	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("err = %v, want a refusal because the task is already bridged", err)
	}
	bound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if bound.ChimeraEndpoint == nil || *bound.ChimeraEndpoint != live.URL {
		t.Fatalf("live binding was replaced: %v", bound.ChimeraEndpoint)
	}
}

func TestClaudeBridgeTakesOverFromABridgeThatIsLeaving(t *testing.T) {
	s := newTestStore(t)
	session := seedRunningClaudeAgent(t, s, "task1")
	// `/background`: the conversation moves to another Claude process, which
	// starts its bridge a moment before the old session's bridge has gone.
	leaving := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pid":1}`))
	}))
	defer leaving.Close()
	if err := s.UpdateChimeraDiscoveryForSession(context.Background(), session.ID, "task1", leaving.URL); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(150*time.Millisecond, leaving.Close)

	endpoint, _ := runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir(), HandoverWait: 5 * time.Second})

	bound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if bound.ChimeraEndpoint == nil || *bound.ChimeraEndpoint != endpoint {
		t.Fatalf("binding = %v, want the new bridge %q", bound.ChimeraEndpoint, endpoint)
	}
}

func TestClaudeBridgeReplacesADeadBinding(t *testing.T) {
	s := newTestStore(t)
	session := seedRunningClaudeAgent(t, s, "task1")
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	if err := s.UpdateChimeraDiscoveryForSession(context.Background(), session.ID, "task1", deadURL); err != nil {
		t.Fatal(err)
	}

	endpoint, _ := runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir()})

	bound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if bound.ChimeraEndpoint == nil || *bound.ChimeraEndpoint != endpoint {
		t.Fatalf("binding = %v, want the new bridge %q", bound.ChimeraEndpoint, endpoint)
	}
}

func TestClaudeBridgeTraceRecordsBothDirectionsForDebugging(t *testing.T) {
	s := newTestStore(t)
	seedRunningClaudeAgent(t, s, "task1")
	trace := filepath.Join(t.TempDir(), "bridge.jsonl")

	endpoint, stop := runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir(), TracePath: trace})
	resp, err := http.Post(endpoint+"/mod/events", "application/json", strings.NewReader(`{"events":[{"type":"hello","cwd":"/work"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := stop(); err != nil {
		t.Fatal(err)
	}

	recorded, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	// What the mod sent and what the bridge told it are both there, in order.
	for _, want := range []string{`"direction":"command"`, `"type":"ready"`, `"direction":"mod"`, `"type":"hello"`} {
		if !strings.Contains(string(recorded), want) {
			t.Fatalf("trace is missing %s:\n%s", want, recorded)
		}
	}
}

func TestClaudeBridgeOffersTheConfiguredModels(t *testing.T) {
	s := newTestStore(t)
	seedRunningClaudeAgent(t, s, "task1")

	endpoint, _ := runningBridge(t, s, cli.ClaudeBridgeOptions{
		TaskID: "task1", RuntimeDir: t.TempDir(), Models: []string{"claude-sonnet-5-5", "claude-haiku-5-5"},
	})

	resp, err := http.Get(endpoint + "/v1/sessions/task1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var models []struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, model := range models {
		refs = append(refs, model.Ref)
	}
	if strings.Join(refs, ",") != "anthropic/claude-sonnet-5-5,anthropic/claude-haiku-5-5" {
		t.Fatalf("models = %v", refs)
	}
}

func postClaudeIdentity(t *testing.T, endpoint, events string) (int, string) {
	t.Helper()
	resp, err := http.Post(endpoint+"/mod/events", "application/json", strings.NewReader(`{"events":`+events+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func assertNativeIdentity(t *testing.T, s *store.Store, kind, id, cwd string) {
	t.Helper()
	record, err := s.GetTaskNativeSession(context.Background(), "task1", kind)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		if record != nil {
			t.Fatalf("unexpected saved identity: %+v", record)
		}
		return
	}
	if record == nil || record.ProviderSessionID != id || record.CWD != cwd || record.ExecutionID != "native-task1" {
		t.Fatalf("saved %s identity = %+v, want %s / %s", kind, record, id, cwd)
	}
}

func TestClaudeBridgeCapturesIdentityAndResumedHistoryWithoutChangingBinding(t *testing.T) {
	for _, ephemeral := range []bool{false, true} {
		t.Run(fmt.Sprintf("ephemeral=%v", ephemeral), func(t *testing.T) {
			value := ""
			if ephemeral {
				value = "1"
			}
			t.Setenv("LEGATO_TASK_EPHEMERAL", value)
			s := newTestStore(t)
			owner := seedRunningClaudeAgent(t, s, "task1")
			linkNativeTestExecution(t, s, owner)
			endpoint, stop := runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir()})
			post := func(events, id, cwd string) {
				t.Helper()
				if code, body := postClaudeIdentity(t, endpoint, events); code != http.StatusNoContent {
					t.Fatalf("identity event = %d %s", code, body)
				}
				if ephemeral {
					id = ""
				}
				assertNativeIdentity(t, s, "claude-code", id, cwd)
			}
			post(`[{"type":"hello","sessionId":"claude-first","cwd":"/repo"}]`, "claude-first", "/repo")
			post(`[{"type":"reset"}]`, "claude-first", "/repo")
			post(`[{"type":"identity","sessionId":"claude-clear","cwd":"/repo"}]`, "claude-clear", "/repo")
			post(`[{"type":"hello","sessionId":"claude-resume","cwd":"/other","messages":[{"role":"user","text":"old question"},{"role":"assistant","text":"old answer"}]}]`, "claude-resume", "/other")
			resp, err := http.Get(endpoint + "/v1/sessions/task1")
			if err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				ID       string `json:"id"`
				Messages []struct {
					Content any `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if snapshot.ID != "task1" || len(snapshot.Messages) != 3 || !strings.Contains(fmt.Sprint(snapshot.Messages[2].Content), "old answer") {
				t.Fatalf("resumed panel = %+v", snapshot)
			}
			bound, err := s.GetAgentSessionByTaskID(context.Background(), "task1")
			if err != nil || bound.ID != owner.ID || bound.ChimeraSessionID == nil || *bound.ChimeraSessionID != "task1" {
				t.Fatalf("binding after transitions = %+v, %v", bound, err)
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			id := "claude-resume"
			if ephemeral {
				id = ""
			}
			assertNativeIdentity(t, s, "claude-code", id, "/other")
		})
	}
}

func TestClaudeBridgeReportsRejectedOwnershipRatherThanReplacingSavedIdentity(t *testing.T) {
	t.Setenv("LEGATO_TASK_EPHEMERAL", "")
	s := newTestStore(t)
	owner := seedRunningClaudeAgent(t, s, "task1")
	linkNativeTestExecution(t, s, owner)
	endpoint, stop := runningBridge(t, s, cli.ClaudeBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir()})
	if code, body := postClaudeIdentity(t, endpoint, `[{"type":"hello","sessionId":"owned","cwd":"/repo"}]`); code != http.StatusNoContent {
		t.Fatalf("initial capture = %d %s", code, body)
	}
	if err := s.FinishAgentSession(context.Background(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "task1", AgentKind: "claude-code", TmuxSession: "replacement", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	code, body := postClaudeIdentity(t, endpoint, `[{"type":"hello","sessionId":"stale","cwd":"/repo"}]`)
	if code != http.StatusInternalServerError || !strings.Contains(body, "resume") {
		t.Fatalf("stale identity = %d %s", code, body)
	}
	if err := stop(); err == nil || !strings.Contains(err.Error(), "resume identity was not recorded") {
		t.Fatalf("bridge failure = %v", err)
	}
	assertNativeIdentity(t, s, "claude-code", "owned", "/repo")
}
