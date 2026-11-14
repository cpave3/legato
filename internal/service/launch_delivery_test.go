package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
)

// nativeLaunchAdapter is an adapter whose sessions bind a native endpoint on
// their own shortly after launch, as Claude Code sessions do once the Legato
// mod has started their bridge.
type nativeLaunchAdapter struct {
	fakeAdapter
}

func (a *nativeLaunchAdapter) BindsNativeEndpoint() bool { return true }

func newNativeLaunchService(t *testing.T, workDir string) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mt := newMockTmux()
	adapter := &nativeLaunchAdapter{fakeAdapter{
		name:        "claude-code",
		rolePrompts: map[string]string{"conductor": "you are the conductor"},
		launchFn:    func(map[string]string, string, string, string) string { return "claude" },
	}}
	svc := NewAgentService(s, mt, workDir, AgentServiceOptions{Adapter: adapter, BriefKickoffDelay: time.Millisecond})
	return svc, s, mt
}

// bindBridge stands in for the session's bridge coming up: the moment the
// agent's native endpoint is bound.
func bindBridge(t *testing.T, s *store.Store, taskID string) <-chan map[string]any {
	t.Helper()
	commands := &lineWriter{lines: make(chan map[string]any, 32)}
	bridge := claudebridge.New(claudebridge.Options{TaskID: taskID, Commands: commands})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	if err := s.UpdateChimeraDiscovery(context.Background(), taskID, taskID, server.URL); err != nil {
		t.Fatal(err)
	}
	return commands.lines
}

func awaitBridgeCommand(t *testing.T, commands <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case command := <-commands:
		return command
	case <-time.After(3 * time.Second):
		t.Fatal("no command reached the bridge")
		return nil
	}
}

func TestLaunchKickoffWaitsForTheSessionsBridgeAndIsSubmittedDirectly(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	createTask(t, s, "parent-1")

	if err := svc.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	// Claude Code is still starting. Typing the kickoff now is how it used to
	// be lost: the text lands in the prompt box and the Enter goes nowhere.
	time.Sleep(60 * time.Millisecond)
	if lines := mt.sentLinesFor("legato-parent-1"); len(lines) != 1 || lines[0] != "claude" {
		t.Fatalf("lines typed while the session was starting = %v, want only the launch command", lines)
	}

	commands := bindBridge(t, s, "parent-1")

	if command := awaitBridgeCommand(t, commands); command["type"] != "submit" || command["text"] != briefKickoffMessage {
		t.Fatalf("command = %v, want the kickoff submitted as a prompt", command)
	}
	if lines := mt.sentLinesFor("legato-parent-1"); len(lines) != 1 {
		t.Fatalf("the kickoff was also typed into the terminal: %v", lines)
	}
}

func TestLaunchKickoffIsTypedWhenNoBridgeAppears(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	// A session that never loads the mod (an older Claude Code, a failed
	// load) still gets its kickoff the way it always did.
	svc.(*agentService).nativeBindTimeout = 40 * time.Millisecond
	createTask(t, s, "parent-1")

	if err := svc.SpawnAgent(context.Background(), "parent-1", 0, 0, AgentSpawnOptions{
		Role: "conductor", ParentTaskID: "parent-1", Brief: "kick off the swarm",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for len(mt.sentLinesFor("legato-parent-1")) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("kickoff never typed; lines = %v", mt.sentLinesFor("legato-parent-1"))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := mt.sentLinesFor("legato-parent-1")[1]; got != briefKickoffMessage {
		t.Fatalf("typed line = %q, want the kickoff", got)
	}
}

func TestPeerNoticeToANewClaudeSessionWaitsForItsBridge(t *testing.T) {
	repo := initTestRepo(t)
	svc, s, mt := newNativeLaunchService(t, repo)
	createTask(t, s, "first")
	createTask(t, s, "second")
	ctx := context.Background()
	if err := svc.SpawnAgent(ctx, "first", 80, 24); err != nil {
		t.Fatal(err)
	}

	if err := svc.SpawnAgent(ctx, "second", 80, 24); err != nil {
		t.Fatal(err)
	}

	time.Sleep(60 * time.Millisecond)
	mt.mu.Lock()
	typed := append([]string(nil), mt.sentMultiline["legato-second"]...)
	mt.mu.Unlock()
	if len(typed) != 0 {
		t.Fatalf("the notice was typed into a session still starting: %v", typed)
	}

	commands := bindBridge(t, s, "second")

	command := awaitBridgeCommand(t, commands)
	text, _ := command["text"].(string)
	if command["type"] != "submit" || !strings.Contains(text, "[legato coordination]") || !strings.Contains(text, "first") {
		t.Fatalf("command = %v, want the coordination notice submitted as a prompt", command)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if typed := mt.sentMultiline["legato-second"]; len(typed) != 0 {
		t.Fatalf("the notice was also typed into the terminal: %v", typed)
	}
}

func TestResumedChimeraReviewerBriefWaitsForBindingAndUsesSecretHTTP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LEGATO_HOME", t.TempDir())
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	adapter := hooks.NewChimeraAdapter("legato")
	adapter.SetModDir(t.TempDir())
	svc.(*agentService).adapter = adapter
	ctx := context.Background()
	createTask(t, s, "review")
	brief := "Review pass two.\nCheck the revised diff."
	if err := svc.SpawnAgent(ctx, "review", 0, 0, AgentSpawnOptions{Slot: SlotReviewer, SessionPolicy: "resume", ResumeSessionID: "reviewer-id", Prompt: brief}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	lines := mt.sentLinesFor("legato-review_reviewer")
	if len(lines) != 1 || !strings.Contains(lines[0], "--session reviewer-id") || strings.Contains(lines[0], "--prompt") {
		t.Fatalf("launch lines=%v", lines)
	}
	session, err := s.GetAgentSessionInSlot(ctx, "review", SlotReviewer)
	if err != nil {
		t.Fatal(err)
	}
	secret, path, err := s.GetChimeraLaunchSecret(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines[0], path) || strings.Contains(lines[0], secret) {
		t.Fatal("launch must name file, never its secret")
	}
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/"+secret+"/v1/sessions/reviewer-id/messages" {
			var body struct {
				Text string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			received <- body.Text
			fmt.Fprint(w, `{}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "reviewer-id", server.URL+"/"+secret); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got != brief {
			t.Fatalf("brief=%q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resumed brief did not reach HTTP")
	}
	if lines := mt.sentLinesFor("legato-review_reviewer"); len(lines) != 1 {
		t.Fatalf("brief was typed: %v", lines)
	}
	// The mod link's binding handler calls this cleanup after persisting its URL.
	if err := s.RemoveChimeraLaunchSecretFile(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("secret file remains: %v", err)
	}
}

func TestFreshChimeraLaunchIsKeptUntilTheBoundSessionAcceptsIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LEGATO_HOME", t.TempDir())
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	adapter := hooks.NewChimeraAdapter("legato")
	adapter.SetModDir(t.TempDir())
	svc.(*agentService).adapter = adapter
	ctx := context.Background()
	createTask(t, s, "fresh")
	message := "The operator's task.\nKeep both lines."
	if err := svc.SpawnAgent(ctx, "fresh", 0, 0, AgentSpawnOptions{Prompt: message}); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 1)
	release := make(chan struct{})
	releaseRequest := sync.OnceFunc(func() { close(release) })
	defer releaseRequest()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/secret/v1/sessions/actual-conversation/messages" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		started <- body.Content
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	// Release the held request before closing the HTTP server on failure.
	t.Cleanup(server.Close)
	session, err := s.GetAgentSessionByTaskID(ctx, "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "actual-conversation", server.URL+"/secret"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-started:
		if got != message {
			t.Fatalf("HTTP content = %q, want %q", got, message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("launch message never reached the bound conversation")
	}
	kept, err := s.ListLaunchMessages(ctx)
	if err != nil || len(kept) != 1 || kept[0].Message != message {
		t.Fatalf("pending while HTTP has not accepted = %+v, %v; want the intact launch message", kept, err)
	}
	// Startup recovery can overlap a launch in this same service. It must
	// not POST a second turn while the first request is still unacknowledged.
	svc.(LaunchMessageResumer).ResumeLaunchMessages(ctx)
	select {
	case duplicate := <-started:
		t.Fatalf("launch delivered twice during recovery: %q", duplicate)
	case <-time.After(100 * time.Millisecond):
	}

	releaseRequest()
	awaitLaunchMessagesDelivered(t, s)
	if lines := mt.sentLinesFor("legato-fresh"); len(lines) != 1 {
		t.Fatalf("launch message was typed: %v", lines)
	}
}

func TestFreshChimeraLaunchWaitsForRoleTurnInsteadOfAppending(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LEGATO_HOME", t.TempDir())
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	adapter := hooks.NewChimeraAdapter("legato")
	adapter.SetModDir(t.TempDir())
	svc.(*agentService).adapter = adapter
	svc.(*agentService).nativeBindTimeout = time.Second
	ctx := context.Background()
	createTask(t, s, "busy-launch")
	message := "This is a delivery test from the Reeve, CHIM-89.\nReply with exactly LAUNCH PROMPT RECEIVED, change no files, and then stop."
	if err := svc.SpawnAgent(ctx, "busy-launch", 0, 0, AgentSpawnOptions{Prompt: message}); err != nil {
		t.Fatal(err)
	}
	var requests []string
	var contents []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/secret/v1/sessions/actual-id/messages" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.URL.RequestURI())
		contents = append(contents, body.Content)
		if len(requests) == 1 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		// An append is acknowledged while the role turn is starting, but is
		// not a new user turn. Only a later ordinary POST starts the task.
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	session, err := s.GetAgentSessionByTaskID(ctx, "busy-launch")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "actual-id", server.URL+"/secret"); err != nil {
		t.Fatal(err)
	}
	awaitLaunchMessagesDelivered(t, s)
	mu.Lock()
	defer mu.Unlock()
	want := "/secret/v1/sessions/actual-id/messages"
	if len(requests) != 2 || requests[0] != want || requests[1] != want {
		t.Fatalf("requests = %v, want two ordinary POSTs, never append", requests)
	}
	for _, content := range contents {
		if content != message {
			t.Fatalf("native launch sent %q instead of original multiline prompt", content)
		}
	}
	if lines := mt.sentLinesFor("legato-busy-launch"); len(lines) != 1 {
		t.Fatalf("launch was typed: %v", lines)
	}
}

type launchLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *launchLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.Write(p)
}
func (l *launchLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}
func waitLaunchLog(t *testing.T, output *launchLog, text string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(output.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("missing log %q in %s", text, output.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChimeraLaunchFailureIsRetainedReportedAndNeverTyped(t *testing.T) {
	for _, mode := range []string{"unbound", "busy", "http-error", "transport-error"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("LEGATO_HOME", t.TempDir())
			output := &launchLog{}
			previous := log.Writer()
			log.SetOutput(output)
			t.Cleanup(func() { log.SetOutput(previous) })
			svc, s, mt := newNativeLaunchService(t, t.TempDir())
			adapter := hooks.NewChimeraAdapter("legato")
			adapter.SetModDir(t.TempDir())
			svc.(*agentService).adapter = adapter
			svc.(*agentService).nativeBindTimeout = 80 * time.Millisecond
			ctx := context.Background()
			taskID := "failure-" + mode
			createTask(t, s, taskID)
			message := "The task\nwith a second line"
			if err := svc.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{Prompt: message}); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var paths []string
			accept := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				paths = append(paths, r.URL.RequestURI())
				if accept {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				if mode == "transport-error" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				if mode == "busy" {
					w.WriteHeader(http.StatusConflict)
				} else {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			session, err := s.GetAgentSessionByTaskID(ctx, taskID)
			if err != nil {
				t.Fatal(err)
			}
			if mode != "unbound" {
				if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "actual-session", server.URL+"/private-secret"); err != nil {
					t.Fatal(err)
				}
			}
			waitLaunchLog(t, output, "for "+taskID+" retained:")
			kept, err := s.ListLaunchMessages(ctx)
			if err != nil || len(kept) != 1 || kept[0].Message != message {
				t.Fatalf("retained messages=%+v, %v", kept, err)
			}
			if lines := mt.sentLinesFor("legato-" + taskID); len(lines) != 1 {
				t.Fatalf("launch task was typed: %v", lines)
			}
			mu.Lock()
			for _, path := range paths {
				if strings.Contains(path, "append=") {
					t.Errorf("launch task appended: %s", path)
				}
			}
			accept = true
			mu.Unlock()
			if mode == "unbound" {
				if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "actual-session", server.URL+"/private-secret"); err != nil {
					t.Fatal(err)
				}
			}
			// The same retained message can be delivered after native recovery.
			if n := svc.(LaunchMessageResumer).ResumeLaunchMessages(ctx); n != 1 {
				t.Fatalf("resumed=%d", n)
			}
			awaitLaunchMessagesDelivered(t, s)
			waitLaunchLog(t, output, "for "+taskID+" delivered")
			if strings.Contains(output.String(), "private-secret") {
				t.Fatal("secret endpoint leaked into log")
			}
		})
	}
}
