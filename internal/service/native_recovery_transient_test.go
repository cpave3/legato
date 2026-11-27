package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/codexbridge"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// A restart of Legato that finds a Chimera agent's endpoint alive but failing
// for the moment (busy, slow) keeps its binding: nothing proves it gone.
func TestRecoveryKeepsABindingWhoseEndpointFailsTransiently(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	for taskID, handler := range map[string]http.HandlerFunc{
		"BUSY": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "busy", http.StatusServiceUnavailable)
		},
		"SLOW": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(chimeraDiscoveryRequestTimeout + time.Second):
			}
		},
		"AUTH": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no", http.StatusUnauthorized)
		},
	} {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		runningAgent(t, s, taskID, "chimera")
		if err := s.UpdateChimeraDiscovery(ctx, taskID, "session-"+taskID, server.URL); err != nil {
			t.Fatal(err)
		}
	}

	if n := svc.(NativeEndpointRecoverer).RecoverNativeEndpoints(ctx); n != 0 {
		t.Fatalf("recovered=%d", n)
	}
	for _, taskID := range []string{"BUSY", "SLOW", "AUTH"} {
		if sessionID, endpoint := binding(t, s, taskID); sessionID != "session-"+taskID || endpoint == "" {
			t.Errorf("%s: binding = %q / %q, want it kept", taskID, sessionID, endpoint)
		}
	}
}

// The operator switches an agent to its terminal while a discovery that
// read the row earlier is still probing: the discovery must not bind it and
// undo the operator's choice.
func TestAutomaticChimeraDiscoveryNeverUndoesADowngradeMadeMeanwhile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	instances := filepath.Join(home, ".chimera", "instances")
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	runningAgent(t, s, "CHIMERA-1", "chimera")
	session, err := s.GetAgentSessionByTaskID(ctx, "CHIMERA-1")
	if err != nil {
		t.Fatal(err)
	}
	downgraded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !downgraded {
			// The mod bound the agent, and the operator switched it to its
			// terminal, while this probe was on its way.
			downgraded = true
			if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "mod-session", "http://127.0.0.1:9/mod"); err != nil {
				t.Error(err)
			}
			if ok, err := s.KeepAgentSessionOnTmux(ctx, session.ID, "mod-session", "http://127.0.0.1:9/mod"); err != nil || !ok {
				t.Errorf("downgrade = %v, %v", ok, err)
			}
		}
		fmt.Fprint(w, `{"id":"session-abc","name":"CHIMERA-1"}`)
	}))
	t.Cleanup(server.Close)
	writeChimeraLockfile(t, instances, "100.json", "session-abc", server.URL)

	_ = svc.DiscoverChimera(ctx, "CHIMERA-1")

	after, err := s.GetAgentSessionByTaskID(ctx, "CHIMERA-1")
	if err != nil {
		t.Fatal(err)
	}
	if !after.NativeOff || after.ChimeraEndpoint != nil {
		t.Fatalf("native_off=%v endpoint bound=%v: discovery undid the downgrade", after.NativeOff, after.ChimeraEndpoint != nil)
	}
}

// The same for a bridge: an automatic discovery that reaches a downgraded
// agent (its precheck raced the downgrade) leaves it in its terminal, and
// only the operator's upgrade binds it again.
func TestAutomaticBridgeDiscoveryNeverUndoesADowngrade(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	runningAgent(t, s, "CODEX-1", "codex")
	endpoint := nativeEndpoint(t, "CODEX-1", "CODEX-1")
	if err := codexbridge.WriteLockfile(ipc.SocketDir(), "CODEX-1", codexbridge.Lockfile{SessionID: "CODEX-1", URL: endpoint}); err != nil {
		t.Fatal(err)
	}
	session, _ := s.GetAgentSessionByTaskID(ctx, "CODEX-1")
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, "CODEX-1", endpoint); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.KeepAgentSessionOnTmux(ctx, session.ID, "CODEX-1", endpoint); err != nil || !ok {
		t.Fatalf("downgrade = %v, %v", ok, err)
	}

	_ = svc.DiscoverChimera(ctx, "CODEX-1")
	if after, _ := s.GetAgentSessionByTaskID(ctx, "CODEX-1"); !after.NativeOff || after.ChimeraEndpoint != nil {
		t.Fatal("an automatic discovery undid the downgrade")
	}

	if err := svc.DiscoverChimera(WithOperatorUpgrade(ctx), "CODEX-1"); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.GetAgentSessionByTaskID(ctx, "CODEX-1"); after.NativeOff || after.ChimeraEndpoint == nil {
		t.Fatal("the operator's upgrade did not bind it")
	}
}
