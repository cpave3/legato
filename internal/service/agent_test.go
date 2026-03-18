package service

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type mockTmux struct {
	sessions map[string]bool
	captures map[string]string
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions: make(map[string]bool),
		captures: make(map[string]string),
	}
}

func (m *mockTmux) Spawn(name, workDir string) error {
	if m.sessions[name] {
		return fmt.Errorf("session %s already exists", name)
	}
	m.sessions[name] = true
	return nil
}

func (m *mockTmux) Kill(name string) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockTmux) Capture(name string) (string, error) {
	if !m.sessions[name] {
		return "", fmt.Errorf("session %s not found", name)
	}
	if c, ok := m.captures[name]; ok {
		return c, nil
	}
	return "$ ", nil
}

func (m *mockTmux) Attach(name string) *exec.Cmd {
	return exec.Command("echo", "attached to", name)
}

func (m *mockTmux) ListSessions() ([]string, error) {
	var result []string
	for name := range m.sessions {
		result = append(result, name)
	}
	return result, nil
}

func (m *mockTmux) IsAlive(name string) (bool, error) {
	return m.sessions[name], nil
}

func newTestAgentService(t *testing.T) (AgentService, *store.Store, *mockTmux) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir())
	return svc, s, mt
}

func createTicket(t *testing.T, s *store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	err := s.CreateTicket(ctx, store.Ticket{
		ID:              id,
		Summary:         "Test " + id,
		Status:          "Doing",
		RemoteStatus:    "In Progress",
		CreatedAt:       "2024-01-01T00:00:00Z",
		UpdatedAt:       "2024-01-01T00:00:00Z",
		RemoteUpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSpawnAgentCreatesSessionAndDBRow(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}

	// Tmux session should exist
	if !mt.sessions["legato-REX-1238"] {
		t.Error("expected tmux session legato-REX-1238 to exist")
	}

	// DB row should exist
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].TicketID != "REX-1238" {
		t.Errorf("TicketID = %q, want %q", agents[0].TicketID, "REX-1238")
	}
	if agents[0].Status != "running" {
		t.Errorf("Status = %q, want %q", agents[0].Status, "running")
	}
}

func TestSpawnAgentDuplicateReturnsError(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "REX-1238"); err == nil {
		t.Error("expected error for duplicate spawn, got nil")
	}
}

func TestKillAgentDestroysSessionAndUpdatesDB(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}
	if err := svc.KillAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}

	// Tmux session should be gone
	if mt.sessions["legato-REX-1238"] {
		t.Error("expected tmux session to be killed")
	}

	// Dead sessions are cleaned up — list should be empty
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("got %d agents, want 0 (dead sessions cleaned up)", len(agents))
	}
}

func TestKillAlreadyDeadAgentNoError(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}
	if err := svc.KillAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}
	// Kill again — should not error
	if err := svc.KillAgent(ctx, "REX-1238"); err != nil {
		t.Errorf("expected no error killing dead agent, got %v", err)
	}
}

func TestReconcileSessionsMarksDeadSessions(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")
	createTicket(t, s, "REX-1239")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "REX-1239"); err != nil {
		t.Fatal(err)
	}

	// Simulate REX-1238's tmux session dying externally
	delete(mt.sessions, "legato-REX-1238")

	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	// ListAgents only returns running sessions — dead ones are filtered out
	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1 (only running)", len(agents))
	}
	if agents[0].TicketID != "REX-1239" {
		t.Errorf("expected REX-1239 (running), got %s", agents[0].TicketID)
	}
	if agents[0].Status != "running" {
		t.Errorf("REX-1239 status = %q, want running", agents[0].Status)
	}

	// REX-1238 should no longer be spawnable as duplicate (it's dead, will be cleaned on re-spawn)
	// Verify by re-spawning — should succeed since dead sessions are cleaned up
	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Errorf("re-spawn after reconcile should succeed, got %v", err)
	}
	mt.Kill("legato-REX-1238") // cleanup
}

func TestCaptureOutputReturnsContent(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}

	mt.captures["legato-REX-1238"] = "hello world\n$ "

	output, err := svc.CaptureOutput(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if output != "hello world\n$ " {
		t.Errorf("output = %q, want %q", output, "hello world\n$ ")
	}
}

func TestAttachCmdReturnsCommand(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTicket(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238"); err != nil {
		t.Fatal(err)
	}

	cmd, err := svc.AttachCmd(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}
