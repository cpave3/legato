package service

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type mockTmux struct {
	sessions        map[string]bool
	captures        map[string]string
	envVars         map[string]map[string]string // session -> key -> value
	spawnDims       map[string][2]int            // session -> [width, height]
	paneCommands    map[string]string             // session -> command
	paneCommandsErr error
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions:  make(map[string]bool),
		captures:  make(map[string]string),
		envVars:   make(map[string]map[string]string),
		spawnDims: make(map[string][2]int),
	}
}

func (m *mockTmux) Spawn(name, workDir string, width, height int, envVars ...string) error {
	if m.sessions[name] {
		return fmt.Errorf("session %s already exists", name)
	}
	m.sessions[name] = true
	m.spawnDims[name] = [2]int{width, height}
	// Store env vars passed at spawn time.
	if len(envVars) > 0 {
		if m.envVars[name] == nil {
			m.envVars[name] = make(map[string]string)
		}
		for _, e := range envVars {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				m.envVars[name][parts[0]] = parts[1]
			}
		}
	}
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

func (m *mockTmux) PaneCommands() (map[string]string, error) {
	if m.paneCommands != nil {
		return m.paneCommands, m.paneCommandsErr
	}
	return map[string]string{}, m.paneCommandsErr
}

func (m *mockTmux) SetEnv(sessionName, key, value string) error {
	if !m.sessions[sessionName] {
		return fmt.Errorf("session %s not found", sessionName)
	}
	if m.envVars[sessionName] == nil {
		m.envVars[sessionName] = make(map[string]string)
	}
	m.envVars[sessionName][key] = value
	return nil
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

func createTask(t *testing.T, s *store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	err := s.CreateTask(ctx, store.Task{
		ID:        id,
		Title:     "Test " + id,
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSpawnAgentCreatesSessionAndDBRow(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
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
	if agents[0].TaskID != "REX-1238" {
		t.Errorf("TaskID = %q, want %q", agents[0].TaskID, "REX-1238")
	}
	if agents[0].Status != "running" {
		t.Errorf("Status = %q, want %q", agents[0].Status, "running")
	}
}

func TestSpawnAgentDuplicateReturnsError(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err == nil {
		t.Error("expected error for duplicate spawn, got nil")
	}
}

func TestKillAgentDestroysSessionAndUpdatesDB(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
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
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
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
	createTask(t, s, "REX-1238")
	createTask(t, s, "REX-1239")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SpawnAgent(ctx, "REX-1239", 0, 0); err != nil {
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
	if agents[0].TaskID != "REX-1239" {
		t.Errorf("expected REX-1239 (running), got %s", agents[0].TaskID)
	}
	if agents[0].Status != "running" {
		t.Errorf("REX-1239 status = %q, want running", agents[0].Status)
	}

	// REX-1238 should no longer be spawnable as duplicate (it's dead, will be cleaned on re-spawn)
	// Verify by re-spawning — should succeed since dead sessions are cleaned up
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Errorf("re-spawn after reconcile should succeed, got %v", err)
	}
	mt.Kill("legato-REX-1238") // cleanup
}

func TestSpawnAgentWithStaleDBSession(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	// Spawn an agent, then simulate the tmux session dying without reconciliation
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}
	// Kill tmux session externally (DB still says "running")
	delete(mt.sessions, "legato-REX-1238")

	// Re-spawn should succeed — stale DB record should be cleaned up
	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Errorf("re-spawn with stale DB session should succeed, got %v", err)
	}

	// Tmux session should exist again
	if !mt.sessions["legato-REX-1238"] {
		t.Error("expected tmux session legato-REX-1238 to exist after re-spawn")
	}
}

func TestCaptureOutputReturnsContent(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
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

type testAdapter struct{}

func (a *testAdapter) Name() string                                    { return "test-tool" }
func (a *testAdapter) InstallHooks(projectDir string) error            { return nil }
func (a *testAdapter) UninstallHooks(projectDir string) error          { return nil }
func (a *testAdapter) EnvVars(taskID, socketPath string) map[string]string {
	return map[string]string{
		"LEGATO_TASK_ID": taskID,
	}
}

func TestSpawnAgentInjectsEnvVarsWhenAdapterConfigured(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := newMockTmux()
	svc := NewAgentService(s, mt, t.TempDir(), AgentServiceOptions{
		Adapter:    &testAdapter{},
		SocketPath: "/tmp/legato.sock",
	})

	ctx := context.Background()
	createTask(t, s, "task1")

	if err := svc.SpawnAgent(ctx, "task1", 0, 0); err != nil {
		t.Fatal(err)
	}

	envs := mt.envVars["legato-task1"]
	if envs == nil {
		t.Fatal("no env vars set on tmux session")
	}
	if envs["LEGATO_TASK_ID"] != "task1" {
		t.Errorf("LEGATO_TASK_ID = %q, want %q", envs["LEGATO_TASK_ID"], "task1")
	}
	if _, ok := envs["LEGATO_SOCKET"]; ok {
		t.Error("LEGATO_SOCKET should not be set (CLI uses broadcast)")
	}
}

func TestSpawnAgentSkipsEnvVarsWithoutAdapter(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task2")

	if err := svc.SpawnAgent(ctx, "task2", 0, 0); err != nil {
		t.Fatal(err)
	}

	if len(mt.envVars["legato-task2"]) != 0 {
		t.Errorf("expected no env vars without adapter, got %v", mt.envVars["legato-task2"])
	}
}

func TestSpawnAgentPassesDimensionsToTmux(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 90, 40); err != nil {
		t.Fatal(err)
	}

	dims := mt.spawnDims["legato-REX-1238"]
	if dims[0] != 90 {
		t.Errorf("width = %d, want 90", dims[0])
	}
	if dims[1] != 40 {
		t.Errorf("height = %d, want 40", dims[1])
	}
}

func TestAttachCmdReturnsCommand(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
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

func TestListAgentsOverridesCommandWithLiveValue(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Set live pane command
	mt.paneCommands = map[string]string{
		"legato-REX-1238": "claude",
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Command != "claude" {
		t.Errorf("Command = %q, want %q", agents[0].Command, "claude")
	}
}

func TestListAgentsKeepsDBCommandForDeadSessions(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Kill tmux session externally and reconcile
	delete(mt.sessions, "legato-REX-1238")
	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	// PaneCommands returns empty (session is dead)
	mt.paneCommands = map[string]string{}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Dead sessions still in DB should keep stored command
	for _, a := range agents {
		if a.TaskID == "REX-1238" {
			if a.Command != "shell" {
				t.Errorf("dead session Command = %q, want %q", a.Command, "shell")
			}
		}
	}
}

func TestReconcileClosesOrphanedIntervals(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Record a working interval
	if err := s.RecordStateTransition(ctx, "REX-1238", "working"); err != nil {
		t.Fatal(err)
	}

	// Simulate tmux session dying
	delete(mt.sessions, "legato-REX-1238")

	if err := svc.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}

	// The open interval should be closed
	durations, err := s.GetStateDurations(ctx, "REX-1238")
	if err != nil {
		t.Fatal(err)
	}
	// Duration should exist and be very small (just created)
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration to exist after reconcile")
	}

	// Verify no open intervals remain
	var openCount int
	if err := s.DB().GetContext(ctx, &openCount,
		"SELECT COUNT(*) FROM state_intervals WHERE task_id = ? AND ended_at IS NULL", "REX-1238"); err != nil {
		t.Fatal(err)
	}
	if openCount != 0 {
		t.Errorf("got %d open intervals, want 0 after reconcile", openCount)
	}
}

func TestGetTaskDurations(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "task1")
	createTask(t, s, "task2")

	// Insert intervals with known durations directly
	s.DB().ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'working', datetime('now', '-60 seconds'), datetime('now', '-30 seconds'))",
		"task1")
	s.DB().ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at) VALUES (?, 'waiting', datetime('now', '-10 seconds'))",
		"task1")
	s.DB().ExecContext(ctx,
		"INSERT INTO state_intervals (task_id, state, started_at, ended_at) VALUES (?, 'working', datetime('now', '-120 seconds'), datetime('now', '-60 seconds'))",
		"task2")

	result, err := svc.GetTaskDurations(ctx, []string{"task1", "task2"})
	if err != nil {
		t.Fatal(err)
	}

	// task1 should have both working and waiting
	if _, ok := result["task1"]; !ok {
		t.Fatal("expected task1 in results")
	}
	if result["task1"].Working == 0 {
		t.Error("expected task1 to have working duration")
	}
	if result["task1"].Waiting == 0 {
		t.Error("expected task1 to have waiting duration")
	}

	// task2 should have working
	if _, ok := result["task2"]; !ok {
		t.Fatal("expected task2 in results")
	}
	if result["task2"].Working == 0 {
		t.Error("expected task2 to have working duration")
	}
}

func TestListAgentsPopulatesTaskTitle(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Title != "Test REX-1238" {
		t.Errorf("Title = %q, want %q", agents[0].Title, "Test REX-1238")
	}
}

func TestListAgentsEmptyTitleWhenTaskMissing(t *testing.T) {
	svc, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Delete the task from the store so lookup fails
	s.DB().ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", "REX-1238")

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Title != "" {
		t.Errorf("Title = %q, want empty string for missing task", agents[0].Title)
	}
}

func TestListAgentsFallsBackOnPaneCommandsError(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "REX-1238")

	if err := svc.SpawnAgent(ctx, "REX-1238", 0, 0); err != nil {
		t.Fatal(err)
	}

	// Simulate PaneCommands failure
	mt.paneCommandsErr = fmt.Errorf("tmux not available")

	agents, err := svc.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	// Should fall back to DB value
	if agents[0].Command != "shell" {
		t.Errorf("Command = %q, want %q (fallback)", agents[0].Command, "shell")
	}
}
