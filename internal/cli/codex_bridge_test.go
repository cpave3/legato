package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/codexbridge"
	"github.com/cpave3/legato/internal/engine/store"
)

// codexAppServer stands in for `codex app-server`: it answers every call with
// an empty result and lets a test announce what the terminal UI does.
type codexAppServer struct {
	t       *testing.T
	server  *httptest.Server
	mu      sync.Mutex
	conn    *websocket.Conn
	joined  chan struct{}
	stops   int
	history map[string]any
}

const codexToken = "codex-token"

func newCodexAppServer(t *testing.T) *codexAppServer {
	t.Helper()
	s := &codexAppServer{t: t, joined: make(chan struct{})}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+codexToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()
		close(s.joined)
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var call struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					ThreadID string `json:"threadId"`
				} `json:"params"`
			}
			if json.Unmarshal(data, &call) != nil || len(call.ID) == 0 {
				continue
			}
			reply := map[string]any{"id": call.ID, "result": map[string]any{}}
			if call.Method == "thread/resume" {
				reply = map[string]any{"id": call.ID, "error": map[string]any{"code": -32600, "message": "no rollout found"}}
				if thread := s.history[call.Params.ThreadID]; thread != nil {
					reply = map[string]any{"id": call.ID, "result": map[string]any{"thread": thread}}
				}
			}
			encoded, _ := json.Marshal(reply)
			_ = conn.Write(context.Background(), websocket.MessageText, encoded)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *codexAppServer) start(context.Context) (cli.CodexAppServer, error) {
	return cli.CodexAppServer{
		URL:   "ws" + strings.TrimPrefix(s.server.URL, "http"),
		Token: codexToken,
		Stop: func() {
			s.mu.Lock()
			s.stops++
			s.mu.Unlock()
		},
	}, nil
}

// terminalOpensThread is the Codex terminal UI attaching and opening its thread.
func (s *codexAppServer) terminalOpensThread(extra ...map[string]any) {
	s.t.Helper()
	select {
	case <-s.joined:
	case <-time.After(5 * time.Second):
		s.t.Fatal("the bridge never connected to the app server")
	}
	thread := map[string]any{"id": "thr-1", "ephemeral": false, "threadSource": "user", "cwd": "/work/repo", "model": "gpt-6-astra"}
	if len(extra) > 0 {
		for key, value := range extra[0] {
			thread[key] = value
		}
	}
	encoded, _ := json.Marshal(map[string]any{"method": "thread/started", "params": map[string]any{"thread": thread}})
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if err := conn.Write(context.Background(), websocket.MessageText, encoded); err != nil {
		s.t.Fatal(err)
	}
}

func (s *codexAppServer) hangUp() {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	_ = conn.Close(websocket.StatusGoingAway, "")
}

func seedRunningCodexAgent(t *testing.T, s *store.Store, taskID string) {
	t.Helper()
	seedTask(t, s, taskID, "Doing")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: taskID, TmuxSession: "legato-" + taskID, Command: "shell", AgentKind: "codex", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	linkNativeTestExecution(t, s, session)
}

// runningCodexBridge starts the bridge and returns what it told the launcher
// about where the terminal UI should connect.
func runningCodexBridge(t *testing.T, s *store.Store, opts cli.CodexBridgeOptions) (ready cli.CodexAppServer, done <-chan struct{}, stop func() error) {
	t.Helper()
	announced := make(chan cli.CodexAppServer, 1)
	opts.Ready = func(server cli.CodexAppServer) { announced <- server }
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	var runErr error
	go func() {
		runErr = cli.RunCodexBridge(ctx, s, opts)
		close(finished)
	}()
	stop = func() error {
		cancel()
		select {
		case <-finished:
			return runErr
		case <-time.After(5 * time.Second):
			t.Fatal("bridge did not stop")
			return nil
		}
	}
	t.Cleanup(func() { _ = stop() })
	select {
	case ready = <-announced:
		return ready, finished, stop
	case <-finished:
		t.Fatalf("bridge exited before it was ready: %v", runErr)
	case <-time.After(5 * time.Second):
		t.Fatal("bridge never said where the terminal should connect")
	}
	return ready, finished, stop
}

func boundEndpoint(t *testing.T, s *store.Store, taskID string) string {
	t.Helper()
	session, err := s.GetAgentSessionByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if session.ChimeraEndpoint == nil {
		return ""
	}
	if session.ChimeraSessionID == nil || *session.ChimeraSessionID != taskID {
		t.Fatalf("bound under session %v, want the task ID", session.ChimeraSessionID)
	}
	return *session.ChimeraEndpoint
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !done() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestCodexBridgeBindsTheRunningAgentForAsLongAsItRuns(t *testing.T) {
	s := newTestStore(t)
	seedRunningCodexAgent(t, s, "task1")
	runtimeDir := t.TempDir()
	codex := newCodexAppServer(t)

	ready, _, stop := runningCodexBridge(t, s, cli.CodexBridgeOptions{TaskID: "task1", RuntimeDir: runtimeDir, StartServer: codex.start})

	// The launcher points the terminal UI at the same app server.
	if !strings.HasPrefix(ready.URL, "ws://127.0.0.1:") || ready.Token != codexToken {
		t.Fatalf("ready = %+v", ready)
	}
	// The agent is bound before the terminal is even started: the panel can
	// show whatever the terminal asks on its way up, and a message sent now
	// is held until there is a thread to put it on.
	endpoint := boundEndpoint(t, s, "task1")
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatalf("endpoint = %q, want a loopback URL", endpoint)
	}
	codex.terminalOpensThread()
	waitFor(t, "the terminal's thread to be followed", func() bool {
		resp, err := http.Get(endpoint + "/v1/sessions/task1")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var snapshot struct {
			CWD string `json:"cwd"`
		}
		return json.NewDecoder(resp.Body).Decode(&snapshot) == nil && snapshot.CWD == "/work/repo"
	})

	// The endpoint serves the native protocol under the task's name, which
	// is what Legato's discovery verifies.
	resp, err := http.Get(endpoint + "/v1/sessions/task1")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Name string `json:"name"`
		CWD  string `json:"cwd"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if snapshot.Name != "task1" || snapshot.CWD != "/work/repo" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	lockfile, err := codexbridge.ReadLockfile(runtimeDir, "task1")
	if err != nil || lockfile.URL != endpoint || lockfile.SessionID != "task1" {
		t.Fatalf("lockfile = %+v, %v", lockfile, err)
	}

	if err := stop(); err != nil {
		t.Fatalf("bridge exit: %v", err)
	}
	if endpoint := boundEndpoint(t, s, "task1"); endpoint != "" {
		t.Fatalf("binding survived the bridge: %q", endpoint)
	}
	if _, err := codexbridge.ReadLockfile(runtimeDir, "task1"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lockfile survived the bridge: %v", err)
	}
	if codex.stops != 1 {
		t.Fatalf("the app server was stopped %d times, want once", codex.stops)
	}
}

func TestCodexBridgeEndsWithItsAppServer(t *testing.T) {
	s := newTestStore(t)
	seedRunningCodexAgent(t, s, "task1")
	codex := newCodexAppServer(t)
	_, done, _ := runningCodexBridge(t, s, cli.CodexBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir(), StartServer: codex.start})
	codex.terminalOpensThread()
	if boundEndpoint(t, s, "task1") == "" {
		t.Fatal("the agent was not bound")
	}

	codex.hangUp()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge outlived its app server")
	}
	if endpoint := boundEndpoint(t, s, "task1"); endpoint != "" {
		t.Fatalf("binding survived the app server: %q", endpoint)
	}
}

func TestCodexBridgeIsRefusedWhereItDoesNotBelong(t *testing.T) {
	s := newTestStore(t)
	codex := newCodexAppServer(t)
	started := 0
	start := func(ctx context.Context) (cli.CodexAppServer, error) {
		started++
		return codex.start(ctx)
	}

	if err := cli.RunCodexBridge(context.Background(), s, cli.CodexBridgeOptions{TaskID: "nobody", RuntimeDir: t.TempDir(), StartServer: start}); err == nil {
		t.Fatal("bridged a task with no running agent")
	}
	seedRunningCodexAgent(t, s, "task1")
	if err := cli.RunCodexBridge(context.Background(), s, cli.CodexBridgeOptions{TaskID: "task1", ExecutionID: "exec-earlier", RuntimeDir: t.TempDir(), StartServer: start}); err == nil {
		t.Fatal("bridged an agent for an execution that does not own it")
	}
	// Neither got as far as starting a Codex app server.
	if started != 0 {
		t.Fatalf("started %d app servers for bridges that were refused", started)
	}
}

func TestTheAppServersAddressIsReadFromWhatItPrints(t *testing.T) {
	banner := "codex app-server (WebSockets)\n  listening on: ws://127.0.0.1:41875\n  readyz: http://127.0.0.1:41875/readyz\n  note: binds localhost only\n"

	address, err := cli.CodexAppServerAddress(strings.NewReader(banner))

	if err != nil || address != "ws://127.0.0.1:41875" {
		t.Fatalf("address = %q, %v", address, err)
	}
	if _, err := cli.CodexAppServerAddress(strings.NewReader("error: unrecognized subcommand 'app-server'\n")); err == nil {
		t.Fatal("found an address in output that names none")
	}
}

func TestCodexBridgeCapturesTopLevelIdentityAndHistoryWithoutChangingBinding(t *testing.T) {
	for _, ephemeral := range []bool{false, true} {
		t.Run(fmt.Sprintf("ephemeral=%v", ephemeral), func(t *testing.T) {
			value := ""
			if ephemeral {
				value = "1"
			}
			t.Setenv("LEGATO_TASK_EPHEMERAL", value)
			s := newTestStore(t)
			seedRunningCodexAgent(t, s, "task1")
			codex := newCodexAppServer(t)
			codex.history = map[string]any{"thr-1": map[string]any{
				"id": "thr-1", "cwd": "/work/repo", "turns": []any{
					map[string]any{"id": "prior-turn", "status": "completed", "items": []any{
						map[string]any{"id": "u0", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "old question"}}},
						map[string]any{"id": "a0", "type": "agentMessage", "text": "old answer"},
					}},
				},
			}}
			_, _, stop := runningCodexBridge(t, s, cli.CodexBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir(), StartServer: codex.start})
			endpoint := boundEndpoint(t, s, "task1")
			codex.terminalOpensThread()
			waitFor(t, "resumed native panel history", func() bool {
				resp, err := http.Get(endpoint + "/v1/sessions/task1")
				if err != nil {
					return false
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				return strings.Contains(string(body), "old answer")
			})
			id := "thr-1"
			assertNativeIdentity(t, s, "codex", id, "/work/repo")
			codex.terminalOpensThread(map[string]any{"id": "child", "parentThreadId": "thr-1"})
			codex.terminalOpensThread(map[string]any{"id": "ephemeral-thread", "ephemeral": true})
			codex.terminalOpensThread(map[string]any{"id": "helper", "threadSource": "thread_title"})
			codex.terminalOpensThread(map[string]any{"id": "replacement", "cwd": "/other"})
			waitFor(t, "replacement thread", func() bool {
				resp, err := http.Get(endpoint + "/v1/sessions/task1")
				if err != nil {
					return false
				}
				defer resp.Body.Close()
				var snap struct {
					CWD string `json:"cwd"`
				}
				if json.NewDecoder(resp.Body).Decode(&snap) != nil || snap.CWD != "/other" {
					return false
				}
				record, err := s.GetTaskNativeSession(context.Background(), "task1", "codex")
				return err == nil && record != nil && record.ProviderSessionID == "replacement"
			})
			id = "replacement"
			assertNativeIdentity(t, s, "codex", id, "/other")
			bound, err := s.GetAgentSessionByTaskID(context.Background(), "task1")
			if err != nil || bound.ChimeraSessionID == nil || *bound.ChimeraSessionID != "task1" {
				t.Fatalf("binding after switch = %+v, %v", bound, err)
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			assertNativeIdentity(t, s, "codex", id, "/other")
		})
	}
}

func TestCodexBridgeReportsRejectedOwnershipRatherThanReplacingSavedIdentity(t *testing.T) {
	t.Setenv("LEGATO_TASK_EPHEMERAL", "")
	s := newTestStore(t)
	seedRunningCodexAgent(t, s, "task1")
	owner, err := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	codex := newCodexAppServer(t)
	_, done, stop := runningCodexBridge(t, s, cli.CodexBridgeOptions{TaskID: "task1", RuntimeDir: t.TempDir(), StartServer: codex.start})
	codex.terminalOpensThread()
	waitFor(t, "first capture", func() bool {
		record, err := s.GetTaskNativeSession(context.Background(), "task1", "codex")
		return err == nil && record != nil
	})
	if err := s.FinishAgentSession(context.Background(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "task1", AgentKind: "codex", TmuxSession: "replacement", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	codex.terminalOpensThread(map[string]any{"id": "stale"})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("capture failure was discarded")
	}
	if err := stop(); err == nil || !strings.Contains(err.Error(), "resume identity was not recorded") {
		t.Fatalf("bridge failure = %v", err)
	}
	assertNativeIdentity(t, s, "codex", "thr-1", "/work/repo")
}
