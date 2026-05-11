package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
	"github.com/cpave3/legato/internal/service"
)

func TestSwarmIsConductor(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{"", true},
		{"conductor", true},
		{"worker", false},
		{"backend", false},
	}
	for _, tc := range cases {
		t.Run("role="+tc.role, func(t *testing.T) {
			t.Setenv("LEGATO_AGENT_ROLE", tc.role)
			if got := cli.SwarmIsConductor(); got != tc.want {
				t.Errorf("SwarmIsConductor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSwarmIsWorker(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{"", false},
		{"conductor", false},
		{"worker", true},
		{"backend", true},
	}
	for _, tc := range cases {
		t.Run("role="+tc.role, func(t *testing.T) {
			t.Setenv("LEGATO_AGENT_ROLE", tc.role)
			if got := cli.SwarmIsWorker(); got != tc.want {
				t.Errorf("SwarmIsWorker() = %v, want %v", got, tc.want)
			}
		})
	}
}

// newTestSwarmServiceForCLI builds a real swarm service backed by an in-memory
// SQLite + a no-op tmux. Used by CLI tests that exercise verbs end-to-end.
func newTestSwarmServiceForCLI(t *testing.T) (service.SwarmService, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	mt := &noopTmux{sessions: map[string]bool{}}
	agentSvc := service.NewAgentService(s, mt, t.TempDir())
	bus := events.New()
	cfg := service.SwarmConfig{MaxConcurrentAgents: 4, MaxSubtasksPerPlan: 10}
	sw := service.NewSwarmService(s, agentSvc, bus, cfg, t.TempDir())
	return sw, s
}

func TestSwarmProposePlan_AutoApprovePersistsSubtasks(t *testing.T) {
	sw, s := newTestSwarmServiceForCLI(t)
	ctx := context.Background()
	// Seed parent task.
	if err := s.CreateTask(ctx, store.Task{
		ID:        "parent-1",
		Title:     "Parent",
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	planDir := t.TempDir()
	planPath := filepath.Join(planDir, "plan.yaml")
	plan := `swarm:
  parent_task_id: parent-1
  working_dir: ` + planDir + `
  summary: test
steps:
  - name: step1
    subtasks:
      - title: Backend
        role: backend
      - title: Frontend
        role: frontend
`
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatal(err)
	}

	// Capture stdout — emitVerdict prints a JSON result.
	stdout := captureStdout(t, func() {
		if err := cli.SwarmProposePlan(sw, planPath, true, 0, swarm.ValidateOptions{MaxSubtasks: 10, MaxSteps: 10}); err != nil {
			t.Fatalf("SwarmProposePlan: %v", err)
		}
	})

	subs, err := s.ListSubtasksByParent(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 {
		t.Errorf("got %d subtasks, want 2", len(subs))
	}

	if !strings.Contains(stdout, `"status":"approved"`) {
		t.Errorf("stdout missing approved status: %q", stdout)
	}
}

func TestSwarmInbox_PrintsAndAcksEvents(t *testing.T) {
	sw, s := newTestSwarmServiceForCLI(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:        "parent-1",
		Title:     "Parent",
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertSwarmEvent(ctx, store.SwarmEvent{
		ParentTaskID: "parent-1",
		Kind:         "progress",
		WorkerTitle:  "API",
		Payload:      "starting",
	}); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cli.SwarmInbox(sw, "parent-1"); err != nil {
			t.Fatalf("SwarmInbox: %v", err)
		}
	})
	if !strings.Contains(out, "progress") {
		t.Errorf("output missing kind: %q", out)
	}
	if !strings.Contains(out, "starting") {
		t.Errorf("output missing payload: %q", out)
	}

	// After draining, inbox should be empty.
	left, err := s.ListUnackedSwarmEvents(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("after SwarmInbox, %d events still unacked, want 0", len(left))
	}
}

func TestSwarmNextStep_HappyPath(t *testing.T) {
	sw, s := newTestSwarmServiceForCLI(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:              "parent-1",
		Title:           "Parent",
		Status:          "Doing",
		SwarmActiveStep: 0,
		CreatedAt:       "2024-01-01T00:00:00Z",
		UpdatedAt:       "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Step 0 subtask completed.
	if err := s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-a1",
		ParentTaskID: "parent-1",
		Title:        "A",
		Status:       "done",
		StepIndex:    0,
	}); err != nil {
		t.Fatal(err)
	}
	// Step 1 subtask queued.
	if err := s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-a2",
		ParentTaskID: "parent-1",
		Title:        "B",
		Status:       "queued",
		StepIndex:    1,
	}); err != nil {
		t.Fatal(err)
	}

	if err := cli.SwarmNextStep(sw, "parent-1"); err != nil {
		t.Fatalf("SwarmNextStep: %v", err)
	}

	parent, err := s.GetTask(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if parent.SwarmActiveStep != 1 {
		t.Errorf("SwarmActiveStep = %d, want 1", parent.SwarmActiveStep)
	}
}

func TestSwarmNextStep_Blocked(t *testing.T) {
	sw, s := newTestSwarmServiceForCLI(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:              "parent-1",
		Title:           "Parent",
		Status:          "Doing",
		SwarmActiveStep: 0,
		CreatedAt:       "2024-01-01T00:00:00Z",
		UpdatedAt:       "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Step 0 subtask still queued — not terminal.
	if err := s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-a1",
		ParentTaskID: "parent-1",
		Title:        "A",
		Status:       "queued",
		StepIndex:    0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-a2",
		ParentTaskID: "parent-1",
		Title:        "B",
		Status:       "queued",
		StepIndex:    1,
	}); err != nil {
		t.Fatal(err)
	}

	if err := cli.SwarmNextStep(sw, "parent-1"); err == nil {
		t.Fatal("expected error when current step is not terminal")
	} else if !strings.Contains(err.Error(), "not terminal") {
		t.Errorf("expected 'not terminal' in error, got %v", err)
	}
}

func TestSwarmNextStep_NoMoreSteps(t *testing.T) {
	sw, s := newTestSwarmServiceForCLI(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:              "parent-1",
		Title:           "Parent",
		Status:          "Doing",
		SwarmActiveStep: 0,
		CreatedAt:       "2024-01-01T00:00:00Z",
		UpdatedAt:       "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Only step 0, done.
	if err := s.CreateSubtask(ctx, store.Subtask{
		ID:           "st-a1",
		ParentTaskID: "parent-1",
		Title:        "A",
		Status:       "done",
		StepIndex:    0,
	}); err != nil {
		t.Fatal(err)
	}

	if err := cli.SwarmNextStep(sw, "parent-1"); err == nil {
		t.Fatal("expected error when no more steps remain")
	} else if !strings.Contains(err.Error(), "no more steps") {
		t.Errorf("expected 'no more steps' in error, got %v", err)
	}
}

func TestSwarmStatus_PrintsValidJSON(t *testing.T) {
	sw, s := newTestSwarmServiceForCLI(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:        "parent-1",
		Title:     "Parent",
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cli.SwarmStatus(sw, "parent-1"); err != nil {
			t.Fatalf("SwarmStatus: %v", err)
		}
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("SwarmStatus output is not valid JSON: %v\n%s", err, out)
	}
}

func TestSwarmInbox_EmptyPrintsNoneMessage(t *testing.T) {
	sw, _ := newTestSwarmServiceForCLI(t)
	out := captureStdout(t, func() {
		if err := cli.SwarmInbox(sw, "parent-empty"); err != nil {
			t.Fatalf("SwarmInbox: %v", err)
		}
	})
	if !strings.Contains(out, "no pending events") {
		t.Errorf("expected 'no pending events' in output, got %q", out)
	}
}

// captureStdout swaps os.Stdout for a pipe, runs fn, restores, returns
// captured output. Tests that exercise CLI verbs printing to stdout use this.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	return <-done
}

// noopTmux is a TmuxManager that records spawn/kill but no-ops on output.
// CLI tests that don't need to introspect tmux state use this.
type noopTmux struct {
	sessions map[string]bool
}

func (m *noopTmux) Spawn(name, workDir string, w, h int, env ...string) error {
	m.sessions[name] = true
	return nil
}
func (m *noopTmux) Kill(name string) error                          { delete(m.sessions, name); return nil }
func (m *noopTmux) Capture(name string) (string, error)             { return "", nil }
func (m *noopTmux) CaptureWithEscapes(name string) (string, error)  { return "", nil }
func (m *noopTmux) Attach(name string) *exec.Cmd                   { return exec.Command("true") }
func (m *noopTmux) ListSessions() ([]string, error)                 { return nil, nil }
func (m *noopTmux) IsAlive(name string) (bool, error)               { return m.sessions[name], nil }
func (m *noopTmux) PaneCommands() (map[string]string, error)        { return nil, nil }
func (m *noopTmux) SetOption(s, k, v string) error                  { return nil }
func (m *noopTmux) SendKeys(name, keys string) error                { return nil }
func (m *noopTmux) SendKey(name, key string) error                  { return nil }
func (m *noopTmux) SendKeysLine(name, line string) error            { return nil }
func (m *noopTmux) SendKeysMultiline(name, payload string) error    { return nil }
func (m *noopTmux) SendKeysShellCommand(name, cmd string) error     { return nil }
func (m *noopTmux) PipeOutput(name string) (io.Reader, func(), error) {
	return strings.NewReader(""), func() {}, nil
}
func (m *noopTmux) SetEnv(s, k, v string) error { return nil }
