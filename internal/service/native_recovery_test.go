package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/codexbridge"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// nativeEndpoint stands in for a live native endpoint (a Chimera instance or
// a bridge) serving one session under a name.
func nativeEndpoint(t *testing.T, sessionID, name string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/instance", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"pid":123}`)
	})
	mux.HandleFunc("GET /v1/sessions/"+sessionID, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"id":%q,"name":%q}`, sessionID, name)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func runningAgent(t *testing.T, s *store.Store, taskID, kind string) {
	t.Helper()
	createTask(t, s, taskID)
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: taskID, TmuxSession: "legato-" + taskID, AgentKind: kind, Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
}

func binding(t *testing.T, s *store.Store, taskID string) (sessionID, endpoint string) {
	t.Helper()
	session, err := s.GetAgentSessionByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if session.ChimeraSessionID != nil {
		sessionID = *session.ChimeraSessionID
	}
	if session.ChimeraEndpoint != nil {
		endpoint = *session.ChimeraEndpoint
	}
	return sessionID, endpoint
}

func TestDiscoverRebindsACodexAgentToItsRunningBridge(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	svc, s, publisher := newDiscoveryTestService(t)
	runningAgent(t, s, "TASK-42", "codex")
	endpoint := nativeEndpoint(t, "TASK-42", "TASK-42")
	// The bridge keeps advertising itself after a downgrade to tmux.
	if err := codexbridge.WriteLockfile(ipc.SocketDir(), "TASK-42", codexbridge.Lockfile{SessionID: "TASK-42", URL: endpoint}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DiscoverChimera(context.Background(), "TASK-42"); err != nil {
		t.Fatal(err)
	}

	if sessionID, bound := binding(t, s, "TASK-42"); sessionID != "TASK-42" || bound != endpoint {
		t.Fatalf("binding = %q / %q", sessionID, bound)
	}
	if len(publisher.discoveries) != 1 || publisher.discoveries[0] != "TASK-42" {
		t.Fatalf("discoveries = %v", publisher.discoveries)
	}
}

func TestDiscoverLeavesACodexAgentAloneWithoutAUsableBridge(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	svc, s, _ := newDiscoveryTestService(t)
	runningAgent(t, s, "TASK-42", "codex")

	// No bridge advertises itself for the task.
	if err := svc.DiscoverChimera(context.Background(), "TASK-42"); err != ErrChimeraDiscoveryNotFound {
		t.Fatalf("error = %v, want not found", err)
	}
	// One does, but it serves another task's session.
	other := nativeEndpoint(t, "TASK-42", "TASK-7")
	if err := codexbridge.WriteLockfile(ipc.SocketDir(), "TASK-42", codexbridge.Lockfile{SessionID: "TASK-42", URL: other}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DiscoverChimera(context.Background(), "TASK-42"); err != ErrChimeraDiscoveryNotFound {
		t.Fatalf("error = %v, want not found", err)
	}
	if _, bound := binding(t, s, "TASK-42"); bound != "" {
		t.Fatalf("bound to %q", bound)
	}
}

func TestRestartingLegatoRecoversAgentsWhoseNativeEndpointIsStillRunning(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	instances := filepath.Join(home, ".chimera", "instances")
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()

	// Three agents were connected natively; their bindings did not survive
	// (a failed snapshot cleared one, a launch-time discovery never finished).
	runningAgent(t, s, "CODEX-1", "codex")
	codexEndpoint := nativeEndpoint(t, "CODEX-1", "CODEX-1")
	if err := codexbridge.WriteLockfile(ipc.SocketDir(), "CODEX-1", codexbridge.Lockfile{SessionID: "CODEX-1", URL: codexEndpoint}); err != nil {
		t.Fatal(err)
	}
	runningAgent(t, s, "CLAUDE-1", "claude-code")
	claudeEndpoint := nativeEndpoint(t, "CLAUDE-1", "CLAUDE-1")
	if err := claudebridge.WriteLockfile(ipc.SocketDir(), "CLAUDE-1", claudebridge.Lockfile{SessionID: "CLAUDE-1", URL: claudeEndpoint}); err != nil {
		t.Fatal(err)
	}
	runningAgent(t, s, "CHIMERA-1", "chimera")
	chimeraEndpoint := nativeEndpoint(t, "session-abc", "CHIMERA-1")
	writeChimeraLockfile(t, instances, "100.json", "session-abc", chimeraEndpoint)

	// One is still bound to an endpoint that is alive, under a session name
	// of its own; it is left exactly as it is.
	runningAgent(t, s, "CHIMERA-2", "chimera")
	keptEndpoint := nativeEndpoint(t, "session-kept", "Ephemeral session")
	if err := s.UpdateChimeraDiscovery(ctx, "CHIMERA-2", "session-kept", keptEndpoint); err != nil {
		t.Fatal(err)
	}
	// One is bound to an endpoint that has gone, with nothing to take its place.
	runningAgent(t, s, "CODEX-2", "codex")
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	if err := s.UpdateChimeraDiscovery(ctx, "CODEX-2", "CODEX-2", goneURL); err != nil {
		t.Fatal(err)
	}
	// And one runs in a terminal with no native endpoint at all.
	runningAgent(t, s, "SHELL-1", "codex")

	recoverer, ok := svc.(NativeEndpointRecoverer)
	if !ok {
		t.Fatal("the agent service cannot recover native endpoints")
	}
	if recovered := recoverer.RecoverNativeEndpoints(ctx); recovered != 3 {
		t.Fatalf("recovered %d agents, want 3", recovered)
	}

	for taskID, want := range map[string][2]string{
		"CODEX-1":   {"CODEX-1", codexEndpoint},
		"CLAUDE-1":  {"CLAUDE-1", claudeEndpoint},
		"CHIMERA-1": {"session-abc", chimeraEndpoint},
		"CHIMERA-2": {"session-kept", keptEndpoint},
		// A panel pointed at a dead endpoint shows nothing; the terminal does.
		"CODEX-2": {"", ""},
		"SHELL-1": {"", ""},
	} {
		if sessionID, endpoint := binding(t, s, taskID); sessionID != want[0] || endpoint != want[1] {
			t.Errorf("%s: binding = %q / %q, want %q / %q", taskID, sessionID, endpoint, want[0], want[1])
		}
	}
}

func TestRecoveryLeavesFinishedAgentsAlone(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	svc, s, _ := newDiscoveryTestService(t)
	createTask(t, s, "CODEX-1")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "CODEX-1", TmuxSession: "legato-CODEX-1", AgentKind: "codex", Status: "dead",
	}); err != nil {
		t.Fatal(err)
	}
	endpoint := nativeEndpoint(t, "CODEX-1", "CODEX-1")
	if err := codexbridge.WriteLockfile(ipc.SocketDir(), "CODEX-1", codexbridge.Lockfile{SessionID: "CODEX-1", URL: endpoint}); err != nil {
		t.Fatal(err)
	}

	if recovered := svc.(NativeEndpointRecoverer).RecoverNativeEndpoints(context.Background()); recovered != 0 {
		t.Fatalf("recovered %d agents, want none", recovered)
	}
}

// An agent switched to its terminal on purpose stays there across a
// restart, though its bridge still runs; upgrading it again binds it.
func TestADeliberateDowngradeToTmuxSurvivesARestart(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	runningAgent(t, s, "CODEX-1", "codex")
	endpoint := nativeEndpoint(t, "CODEX-1", "CODEX-1")
	if err := codexbridge.WriteLockfile(ipc.SocketDir(), "CODEX-1", codexbridge.Lockfile{SessionID: "CODEX-1", URL: endpoint}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DiscoverChimera(ctx, "CODEX-1"); err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(ctx, "CODEX-1")

	downgraded, err := svc.(NativeDowngrader).DowngradeToTmux(ctx, session.ID, "CODEX-1", endpoint)
	if err != nil || !downgraded {
		t.Fatalf("DowngradeToTmux = %v, %v", downgraded, err)
	}
	if recovered := svc.(NativeEndpointRecoverer).RecoverNativeEndpoints(ctx); recovered != 0 {
		t.Fatalf("recovered %d agents, want the downgraded one left alone", recovered)
	}
	if _, bound := binding(t, s, "CODEX-1"); bound != "" {
		t.Fatalf("bound again to %q after a restart", bound)
	}

	// The person upgrades it again: it is native, and stays so.
	if err := svc.DiscoverChimera(ctx, "CODEX-1"); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.GetAgentSessionByTaskID(ctx, "CODEX-1"); after.NativeOff {
		t.Fatal("still marked as kept on tmux after an upgrade")
	}
}

func TestRecoveryUsesStoredChimeraSecretForAReviewerSlot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(os.Getenv("HOME"), ".chimera", "instances")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/kept-secret/v1/instance":
			fmt.Fprint(w, `{"pid":123}`)
		case "/kept-secret/v1/sessions/reviewer-conversation":
			fmt.Fprint(w, `{"id":"reviewer-conversation","name":"task_reviewer"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	createTask(t, s, "task")
	id, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "task", Slot: SlotReviewer, AgentKind: "chimera", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetChimeraLaunchSecret(ctx, id, "kept-secret", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, id, "old", "http://127.0.0.1:1/kept-secret"); err != nil {
		t.Fatal(err)
	}
	writeChimeraLockfile(t, dir, "reviewer.json", "reviewer-conversation", server.URL)
	// A new service uses the persisted private record, not launch-time memory.
	restarted := NewAgentService(s, newMockTmux(), t.TempDir())
	if n := restarted.(NativeEndpointRecoverer).RecoverNativeEndpoints(ctx); n != 1 {
		t.Fatalf("recovered=%d", n)
	}
	got, err := s.GetAgentSessionInSlot(ctx, "task", SlotReviewer)
	if err != nil || got.ChimeraEndpoint == nil || *got.ChimeraEndpoint != server.URL+"/kept-secret" {
		t.Fatalf("binding=%+v err=%v", got, err)
	}
	if n := restarted.(NativeEndpointRecoverer).RecoverNativeEndpoints(ctx); n != 0 {
		t.Fatalf("live secret binding was rebound: %d", n)
	}
}
