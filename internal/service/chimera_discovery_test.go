package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestDiscoverChimeraVerifiesAndPersistsExactTaskSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	instancesDir := filepath.Join(home, ".chimera", "instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instance":
			fmt.Fprint(w, `{"pid":123,"cwd":"/repo","version":"test"}`)
		case "/v1/sessions/TASK-42":
			fmt.Fprint(w, `{"id":"TASK-42","name":"TASK-42"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chimeraServer.Close)

	writeChimeraLockfile(t, instancesDir, "wrong.json", "OTHER", chimeraServer.URL)
	writeChimeraLockfile(t, instancesDir, "match.json", "TASK-42", chimeraServer.URL)

	svc, s, publisher := newDiscoveryTestService(t)
	createTask(t, s, "TASK-42")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "chimera", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DiscoverChimera(context.Background(), "TASK-42"); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(context.Background(), "TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	if session.ChimeraSessionID == nil || *session.ChimeraSessionID != "TASK-42" {
		t.Fatalf("session ID = %v", session.ChimeraSessionID)
	}
	if session.ChimeraEndpoint == nil || *session.ChimeraEndpoint != chimeraServer.URL {
		t.Fatalf("endpoint = %v", session.ChimeraEndpoint)
	}
	if len(publisher.discoveries) != 1 || publisher.discoveries[0] != "TASK-42" {
		t.Fatalf("discovery events = %v", publisher.discoveries)
	}
}

func TestDiscoverChimeraMatchesByNameWithRandomSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	instancesDir := filepath.Join(home, ".chimera", "instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate the collision case: Chimera generated a random session ID
	// but preserved the session name (task ID). Discovery should match by
	// name, not by exact session ID.
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instance":
			fmt.Fprint(w, `{"pid":123,"cwd":"/repo","version":"test"}`)
		case "/v1/sessions/01KZTTNR9GKBTSF9BSRXP4Y1MT":
			fmt.Fprint(w, `{"id":"01KZTTNR9GKBTSF9BSRXP4Y1MT","name":"TASK-42"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chimeraServer.Close)

	writeChimeraLockfile(t, instancesDir, "match.json", "01KZTTNR9GKBTSF9BSRXP4Y1MT", chimeraServer.URL)

	svc, s, publisher := newDiscoveryTestService(t)
	createTask(t, s, "TASK-42")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "chimera", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DiscoverChimera(context.Background(), "TASK-42"); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(context.Background(), "TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	// The lockfile's actual session ID (random) is persisted, not the task ID.
	if session.ChimeraSessionID == nil || *session.ChimeraSessionID != "01KZTTNR9GKBTSF9BSRXP4Y1MT" {
		t.Fatalf("session ID = %v", session.ChimeraSessionID)
	}
	if session.ChimeraEndpoint == nil || *session.ChimeraEndpoint != chimeraServer.URL {
		t.Fatalf("endpoint = %v", session.ChimeraEndpoint)
	}
	if len(publisher.discoveries) != 1 || publisher.discoveries[0] != "TASK-42" {
		t.Fatalf("discovery events = %v", publisher.discoveries)
	}
}

func TestInvalidateChimeraBindingRoutesSubsequentMessagesThroughTmux(t *testing.T) {
	svc, s, _ := newDiscoveryTestService(t)
	ctx := context.Background()
	createTask(t, s, "TASK-42")
	sessionID, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{
		TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "chimera", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, sessionID, "chimera-old", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}

	invalidator := svc.(ChimeraBindingInvalidator)
	cleared, err := invalidator.InvalidateChimeraBinding(ctx, sessionID, "chimera-old", "http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("binding was not invalidated")
	}
	mt := svc.(*agentService).tmux.(*mockTmux)
	mt.mu.Lock()
	mt.sessions["legato-TASK-42"] = true
	mt.mu.Unlock()
	if err := svc.SendMessage(ctx, "TASK-42", "continue in tmux"); err != nil {
		t.Fatal(err)
	}

	if got := mt.sentMultilineFor("legato-TASK-42"); len(got) != 1 || got[0] != "continue in tmux" {
		t.Fatalf("tmux messages = %v", got)
	}
}

func TestDiscoverChimeraDoesNotAttachToReplacementAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	instancesDir := filepath.Join(home, ".chimera", "instances")
	if err := os.MkdirAll(instancesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	verificationStarted := make(chan struct{})
	continueVerification := make(chan struct{})
	chimeraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instance":
			close(verificationStarted)
			<-continueVerification
			fmt.Fprint(w, `{"pid":123}`)
		case "/v1/sessions/TASK-42":
			fmt.Fprint(w, `{"id":"TASK-42","name":"TASK-42"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(chimeraServer.Close)
	writeChimeraLockfile(t, instancesDir, "match.json", "TASK-42", chimeraServer.URL)

	svc, s, publisher := newDiscoveryTestService(t)
	createTask(t, s, "TASK-42")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "TASK-42", TmuxSession: "old", AgentKind: "chimera", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	discoveryDone := make(chan error, 1)
	go func() { discoveryDone <- svc.DiscoverChimera(context.Background(), "TASK-42") }()
	<-verificationStarted
	if err := s.UpdateAgentSessionStatus(context.Background(), "TASK-42", "dead"); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{
		TaskID: "TASK-42", TmuxSession: "replacement", AgentKind: "chimera", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	close(continueVerification)

	if err := <-discoveryDone; !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DiscoverChimera() error = %v, want stale-session failure", err)
	}
	session, err := s.GetAgentSessionByTaskID(context.Background(), "TASK-42")
	if err != nil {
		t.Fatal(err)
	}
	if session.TmuxSession != "replacement" || session.ChimeraEndpoint != nil {
		t.Fatalf("replacement session was mutated: %#v", session)
	}
	if len(publisher.discoveries) != 0 {
		t.Fatalf("stale discovery published events: %v", publisher.discoveries)
	}
}

func TestDiscoverChimeraFailsClosedWithoutUniqueVerifiedCandidate(t *testing.T) {
	tests := []struct {
		name      string
		lockfiles func(t *testing.T, dir string)
		wantErr   error
	}{
		{
			name: "no matching session",
			lockfiles: func(t *testing.T, dir string) {
				writeChimeraLockfile(t, dir, "other.json", "OTHER", "http://127.0.0.1:1")
			},
			wantErr: ErrChimeraDiscoveryNotFound,
		},
		{
			name: "non-loopback endpoint",
			lockfiles: func(t *testing.T, dir string) {
				writeChimeraLockfile(t, dir, "remote.json", "TASK-42", "http://192.0.2.1:8080")
			},
			wantErr: ErrChimeraDiscoveryNotFound,
		},
		{
			name: "dead endpoint",
			lockfiles: func(t *testing.T, dir string) {
				writeChimeraLockfile(t, dir, "dead.json", "TASK-42", "http://127.0.0.1:1")
			},
			wantErr: ErrChimeraDiscoveryNotFound,
		},
		{
			name: "redirected endpoint",
			lockfiles: func(t *testing.T, dir string) {
				target := newChimeraDiscoveryServer(t, "TASK-42", "TASK-42")
				redirect := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusTemporaryRedirect))
				t.Cleanup(redirect.Close)
				writeChimeraLockfile(t, dir, "redirect.json", "TASK-42", redirect.URL)
			},
			wantErr: ErrChimeraDiscoveryNotFound,
		},
		{
			name: "endpoint serves another session",
			lockfiles: func(t *testing.T, dir string) {
				server := newChimeraDiscoveryServer(t, "TASK-42", "OTHER")
				writeChimeraLockfile(t, dir, "wrong.json", "TASK-42", server.URL)
			},
			wantErr: ErrChimeraDiscoveryNotFound,
		},
		{
			name: "multiple verified endpoints",
			lockfiles: func(t *testing.T, dir string) {
				first := newChimeraDiscoveryServer(t, "TASK-42", "TASK-42")
				second := newChimeraDiscoveryServer(t, "TASK-42", "TASK-42")
				writeChimeraLockfile(t, dir, "first.json", "TASK-42", first.URL)
				writeChimeraLockfile(t, dir, "second.json", "TASK-42", second.URL)
			},
			wantErr: ErrChimeraDiscoveryAmbiguous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			instancesDir := filepath.Join(home, ".chimera", "instances")
			if err := os.MkdirAll(instancesDir, 0o755); err != nil {
				t.Fatal(err)
			}
			tt.lockfiles(t, instancesDir)

			svc, s, publisher := newDiscoveryTestService(t)
			createTask(t, s, "TASK-42")
			if err := s.InsertAgentSession(context.Background(), store.AgentSession{
				TaskID: "TASK-42", TmuxSession: "legato-TASK-42", AgentKind: "chimera", Status: "running",
			}); err != nil {
				t.Fatal(err)
			}

			err := svc.DiscoverChimera(context.Background(), "TASK-42")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DiscoverChimera() error = %v, want %v", err, tt.wantErr)
			}
			session, err := s.GetAgentSessionByTaskID(context.Background(), "TASK-42")
			if err != nil {
				t.Fatal(err)
			}
			if session.ChimeraEndpoint != nil || session.ChimeraSessionID != nil {
				t.Fatalf("failed discovery mutated session: %#v", session)
			}
			if len(publisher.discoveries) != 0 {
				t.Fatalf("failed discovery published events: %v", publisher.discoveries)
			}
		})
	}
}

func newChimeraDiscoveryServer(t *testing.T, sessionID, sessionName string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instance":
			fmt.Fprint(w, `{"pid":123,"cwd":"/repo","version":"test"}`)
		case "/v1/sessions/" + sessionID:
			fmt.Fprintf(w, `{"id":%q,"name":%q}`, sessionID, sessionName)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newDiscoveryTestService(t *testing.T) (AgentService, *store.Store, *recordingPublisher) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	publisher := &recordingPublisher{}
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{EventBus: publisher})
	return svc, s, publisher
}

func writeChimeraLockfile(t *testing.T, dir, name, sessionID, endpoint string) {
	t.Helper()
	body := fmt.Sprintf(`{"pid":123,"sessionId":%q,"url":%q}`, sessionID, endpoint)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
