package cli_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

func seedParent(t *testing.T, s *store.Store, parentID string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:        parentID,
		Title:     "parent " + parentID,
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func seedSubtask(t *testing.T, s *store.Store, id, parentID, status string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateSubtask(ctx, store.Subtask{
		ID:           id,
		ParentTaskID: parentID,
		Title:        "sub " + id,
		Role:         "worker",
		Status:       status,
	}); err != nil {
		t.Fatal(err)
	}
}

func spawnSwarmAgent(t *testing.T, s *store.Store, taskID, parentID string) {
	t.Helper()
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:       taskID,
		TmuxSession:  "legato-" + taskID,
		Command:      "shell",
		Status:       "running",
		ParentTaskID: &parentID,
		SubtaskID:    &taskID,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
}

// TestAgentStatus_SwarmParticipant returns swarm-aware markup.
func TestAgentStatus_SwarmParticipant(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	parentID := "parent-swarm-01"
	seedParent(t, s, parentID)
	seedSubtask(t, s, "st-01", parentID, "in_progress")
	seedSubtask(t, s, "st-02", parentID, "done")
	seedSubtask(t, s, "st-03", parentID, "queued")

	spawnSwarmAgent(t, s, "st-01", parentID)

	out, err := cli.AgentStatus(s, "st-01", "tmux")
	if err != nil {
		t.Fatalf("AgentStatus: %v", err)
	}
	if !strings.Contains(out, "1/3 done") {
		t.Errorf("output missing progress: %q", out)
	}
	if !strings.Contains(out, "1 workers") {
		t.Errorf("output missing sibling count: %q", out)
	}
}

// TestAgentStatus_SoloAgent_FallsBackToSummary returns the legacy summary
// when the task is not a swarm participant.
func TestAgentStatus_SoloAgent_FallsBackToSummary(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task-solo", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "task-solo",
		TmuxSession: "legato-task-solo",
		Command:     "shell",
		Status:      "running",
	})

	out, err := cli.AgentStatus(s, "task-solo", "tmux")
	if err != nil {
		t.Fatalf("AgentStatus: %v", err)
	}
	// Solo agents fall back to the summary output.
	if !strings.Contains(out, "idle") {
		t.Errorf("output missing idle count (legacy summary): %q", out)
	}
}

// TestAgentStatus_ScopeWarning_Icon emitted when a scope_warning event exists.
func TestAgentStatus_ScopeWarning_Icon(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	parentID := "parent-swarm-02"
	seedParent(t, s, parentID)
	seedSubtask(t, s, "st-01", parentID, "in_progress")
	spawnSwarmAgent(t, s, "st-01", parentID)

	ctx := context.Background()
	_, _ = s.InsertSwarmEvent(ctx, store.SwarmEvent{
		ParentTaskID: parentID,
		Kind:         "scope_warning",
		WorkerTitle:  "worker",
		Payload:      "conflict",
	})

	out, err := cli.AgentStatus(s, "st-01", "tmux")
	if err != nil {
		t.Fatalf("AgentStatus: %v", err)
	}
	if !strings.Contains(out, "⚠") {
		t.Errorf("output missing scope-warning icon: %q", out)
	}
}

// TestAgentStatus_LastEvent_Age formats event kind + age.
func TestAgentStatus_LastEvent_Age(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	parentID := "parent-swarm-03"
	seedParent(t, s, parentID)
	seedSubtask(t, s, "st-01", parentID, "in_progress")
	spawnSwarmAgent(t, s, "st-01", parentID)

	ctx := context.Background()
	id, _ := s.InsertSwarmEvent(ctx, store.SwarmEvent{
		ParentTaskID: parentID,
		Kind:         "built",
		WorkerTitle:  "worker",
		Payload:      "ready",
	})
	// InsertSwarmEvent ignores CreatedAt (column default `datetime('now')`), so
	// backdate the row directly to exercise the age-formatting branch.
	if _, err := s.DB().ExecContext(ctx, `UPDATE swarm_events SET created_at = datetime('now', '-2 minutes') WHERE id = ?`, id); err != nil {
		t.Fatalf("backdate event: %v", err)
	}

	out, err := cli.AgentStatus(s, "st-01", "tmux")
	if err != nil {
		t.Fatalf("AgentStatus: %v", err)
	}
	if !strings.Contains(out, "built") {
		t.Errorf("output missing built event: %q", out)
	}
	if !strings.Contains(out, "2m") {
		t.Errorf("output missing 2m age: %q", out)
	}
}

// TestAgentStatus_NoSwarmSession_FallsBackToSummary covers when a swarm agent
// has no DB session record.
func TestAgentStatus_NoSwarmSession_FallsBackToSummary(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	parentID := "parent-swarm-04"
	seedParent(t, s, parentID)
	seedSubtask(t, s, "st-01", parentID, "in_progress")
	// No agent session inserted.

	out, err := cli.AgentStatus(s, "st-01", "tmux")
	if err != nil {
		t.Fatalf("AgentStatus: %v", err)
	}
	if !strings.Contains(out, "idle") {
		t.Errorf("output missing idle count (fallback summary): %q", out)
	}
}

// TestAgentStatus_InvalidFormat returns error for unsupported formats.
func TestAgentStatus_InvalidFormat(t *testing.T) {
	s := newTestStore(t)
	_, err := cli.AgentStatus(s, "any", "json")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// BenchmarkLegatoAgentStatus simulates the CLI hot path.
//
// Measured on 2026-05-12 (AMD Ryzen 9 7900X, SSD, SQLite):
//
//	goos: linux
//	goarch: amd64
//	pkg: github.com/cpave3/legato/internal/cli
//	BenchmarkLegatoAgentStatus-24    ~47000    ~26000 ns/op    (cold, one SQLite query)
//
// The actual `time legato agent status <id> --format tmux` shell invocation
// adds ~3–4 ms of process spawn overhead, putting real-world latency well
// under the 10 ms budget.
func BenchmarkLegatoAgentStatus(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")
	s, err := store.New(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	_ = s.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: "Doing", SortOrder: 0})
	_ = s.CreateTask(ctx, store.Task{ID: "task1", Title: "Test", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"})
	_ = s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cli.AgentStatus(s, "task1", "tmux")
	}
}
