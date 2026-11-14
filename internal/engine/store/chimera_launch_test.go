package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFallbackBindingDoesNotReplaceNativeBinding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "launch")
	id, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "launch", Status: "running", TmuxSession: "legato-launch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscoveryForSession(ctx, id, "mod", "http://127.0.0.1:1/private"); err != nil {
		t.Fatal(err)
	}
	bound, err := s.BindChimeraDiscoveryIfUnbound(ctx, id, "legacy", "http://127.0.0.1:2")
	if err != nil || bound {
		t.Fatalf("fallback bound=%v err=%v", bound, err)
	}
	got, err := s.GetAgentSessionByTaskID(ctx, "launch")
	if err != nil || *got.ChimeraSessionID != "mod" {
		t.Fatalf("binding=%+v err=%v", got, err)
	}
}

func TestChimeraLaunchSecretStaysOutOfSessionListings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "launch")
	id, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "launch", Status: "running", TmuxSession: "legato-launch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetChimeraLaunchSecret(ctx, id, "private-value", "/private/path"); err != nil {
		t.Fatal(err)
	}
	secret, path, err := s.GetChimeraLaunchSecret(ctx, id)
	if err != nil || secret != "private-value" || path != "/private/path" {
		t.Fatalf("secret=%q path=%q err=%v", secret, path, err)
	}
	sessions, err := s.ListAgentSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") {
		t.Fatalf("secret leaked: %s", data)
	}
}

func TestChimeraBootstrapCleanupRetainsPrivateRecoverySecret(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "launch")
	id, err := s.InsertAgentSessionReturningID(ctx, AgentSession{TaskID: "launch", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("recovery-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChimeraLaunchSecret(ctx, id, "recovery-secret", path); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.RemoveChimeraLaunchSecretFile(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("secret file remains: %v", err)
	}
	secret, _, err := s.GetChimeraLaunchSecret(ctx, id)
	if err != nil || secret != "recovery-secret" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
}
