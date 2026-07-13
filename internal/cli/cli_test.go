package cli_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedColumns(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	for i, col := range []string{"Backlog", "Doing", "Review", "Done"} {
		if err := s.CreateColumnMapping(ctx, store.ColumnMapping{
			ColumnName: col,
			SortOrder:  i,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func seedTask(t *testing.T, s *store.Store, id, status string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:        id,
		Title:     "Test " + id,
		Status:    status,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskShow_DefaultsToDescription(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:            "abc123",
		Title:         "Fetch task content",
		DescriptionMD: "Use this content in an agent prompt.",
		Status:        "Backlog",
		CreatedAt:     "2024-01-01T00:00:00Z",
		UpdatedAt:     "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := cli.TaskShow(s, "abc123", "")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "## abc123: Fetch task content") {
		t.Errorf("output missing heading: %q", out)
	}
	if !strings.Contains(out, "Use this content in an agent prompt.") {
		t.Errorf("output missing description: %q", out)
	}
}

func TestTaskShow_FullFormat(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID:            "abc123",
		Title:         "Fetch task content",
		DescriptionMD: "Full context body.",
		Status:        "Backlog",
		Priority:      "High",
		CreatedAt:     "2024-01-01T00:00:00Z",
		UpdatedAt:     "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := cli.TaskShow(s, "abc123", "full")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(out, "# Task: abc123") {
		t.Errorf("output missing task heading: %q", out)
	}
	if !strings.Contains(out, "**Priority:** High") {
		t.Errorf("output missing priority: %q", out)
	}
	if !strings.Contains(out, "---") {
		t.Errorf("output missing separator: %q", out)
	}
}

func TestTaskShow_JSONFormat(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	ctx := context.Background()
	remoteMeta := `{"issue_type":"Story","url":"https://example.test/T-1"}`
	provider := "jira"
	remoteID := "T-1"
	if err := s.CreateTask(ctx, store.Task{
		ID:            "abc123",
		Title:         "Fetch task content",
		DescriptionMD: "JSON body.",
		Status:        "Backlog",
		Priority:      "Medium",
		Provider:      &provider,
		RemoteID:      &remoteID,
		RemoteMeta:    &remoteMeta,
		CreatedAt:     "2024-01-01T00:00:00Z",
		UpdatedAt:     "2024-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := cli.TaskShow(s, "abc123", "json")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if got["id"] != "abc123" {
		t.Errorf("id = %v, want abc123", got["id"])
	}
	if got["description_md"] != "JSON body." {
		t.Errorf("description_md = %v, want JSON body.", got["description_md"])
	}
	if got["provider"] != "jira" {
		t.Errorf("provider = %v, want jira", got["provider"])
	}
	meta, ok := got["remote_meta"].(map[string]any)
	if !ok {
		t.Fatalf("remote_meta = %T, want object", got["remote_meta"])
	}
	if meta["issue_type"] != "Story" {
		t.Errorf("remote_meta.issue_type = %v, want Story", meta["issue_type"])
	}
	if _, ok := got["swarm_step_names"].([]any); !ok {
		t.Errorf("swarm_step_names = %T, want array", got["swarm_step_names"])
	}
}

func TestTaskShow_InvalidFormat(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := func() error {
		_, err := cli.TaskShow(s, "abc123", "xml")
		return err
	}()
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "valid formats") {
		t.Errorf("error = %q, want valid formats", err)
	}
}

func TestTaskShow_NonexistentTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	_, err := cli.TaskShow(s, "missing", "description")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestTaskUpdate_MovesTaskToColumn(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskUpdate(s, "abc123", "Done")
	if err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	task, err := s.GetTask(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != "Done" {
		t.Errorf("task status = %q, want %q", task.Status, "Done")
	}
}

func TestTaskUpdate_CaseInsensitiveStatus(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskUpdate(s, "abc123", "done")
	if err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	task, _ := s.GetTask(context.Background(), "abc123")
	if task.Status != "Done" {
		t.Errorf("task status = %q, want %q", task.Status, "Done")
	}
}

func TestTaskUpdate_InvalidStatus(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskUpdate(s, "abc123", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestTaskUpdate_NonexistentTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	err := cli.TaskUpdate(s, "nonexistent", "Done")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestAgentState_UpdatesActivity(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "abc123",
		TmuxSession: "legato-abc123",
		Command:     "shell",
		Status:      "running",
	})

	err := cli.AgentState(s, "abc123", "working", "", nil, nil)
	if err != nil {
		t.Fatalf("AgentState: %v", err)
	}

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetAgentSessionByTaskID: %v", err)
	}
	if session.Activity != "working" {
		t.Errorf("activity = %q, want %q", session.Activity, "working")
	}
}

func TestAgentState_ClearsActivity(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "abc123",
		TmuxSession: "legato-abc123",
		Command:     "shell",
		Status:      "running",
	})

	cli.AgentState(s, "abc123", "working", "", nil, nil)
	cli.AgentState(s, "abc123", "", "", nil, nil)

	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "" {
		t.Errorf("activity = %q, want empty", session.Activity)
	}
}

func TestAgentState_RecordsStateInterval(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "abc123",
		TmuxSession: "legato-abc123",
		Command:     "shell",
		Status:      "running",
	})

	cli.AgentState(s, "abc123", "working", "", nil, nil)

	durations, err := s.GetStateDurations(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetStateDurations: %v", err)
	}
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration to be recorded")
	}
}

func TestAgentState_TransitionClosesAndOpensInterval(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{
		TaskID:      "abc123",
		TmuxSession: "legato-abc123",
		Command:     "shell",
		Status:      "running",
	})

	cli.AgentState(s, "abc123", "working", "", nil, nil)
	cli.AgentState(s, "abc123", "waiting", "", nil, nil)

	durations, err := s.GetStateDurations(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetStateDurations: %v", err)
	}
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration after transition")
	}
	if _, ok := durations["waiting"]; !ok {
		t.Error("expected waiting duration after transition")
	}
}

func TestAgentSummary_MixedStates(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")
	seedTask(t, s, "task2", "Doing")
	seedTask(t, s, "task3", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task3", TmuxSession: "legato-task3", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "waiting")
	// task3 idle

	out, err := cli.AgentSummary(s, "")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	// Should contain tmux style markup and counts
	if !strings.Contains(out, "1 working") {
		t.Errorf("output missing working count: %q", out)
	}
	if !strings.Contains(out, "1 waiting") {
		t.Errorf("output missing waiting count: %q", out)
	}
	if !strings.Contains(out, "1 idle") {
		t.Errorf("output missing idle count: %q", out)
	}
	if !strings.Contains(out, "#[fg=") {
		t.Errorf("output missing tmux style markup: %q", out)
	}
}

func TestAgentSummary_ZeroCountsOmitted(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")

	out, err := cli.AgentSummary(s, "")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	if !strings.Contains(out, "1 working") {
		t.Errorf("output missing working count: %q", out)
	}
	// Zero-count waiting should be omitted
	if strings.Contains(out, "waiting") {
		t.Errorf("output should omit zero waiting: %q", out)
	}
	// Idle always shown
	if !strings.Contains(out, "idle") {
		t.Errorf("output should always show idle: %q", out)
	}
}

func TestAgentSummary_NoSessions(t *testing.T) {
	s := newTestStore(t)

	out, err := cli.AgentSummary(s, "")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty output even with no sessions")
	}
}

func TestAgentSummary_ExcludeTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")
	seedTask(t, s, "task2", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "working")

	out, err := cli.AgentSummary(s, "task1")
	if err != nil {
		t.Fatalf("AgentSummary: %v", err)
	}
	if !strings.Contains(out, "1 working") {
		t.Errorf("output should show 1 working (task1 excluded): %q", out)
	}
}

func TestTaskNote_AppendsNote(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskNote(s, "abc123", "Fixed the auth bug")
	if err != nil {
		t.Fatalf("TaskNote: %v", err)
	}

	task, _ := s.GetTask(context.Background(), "abc123")
	if task.Description == "" {
		t.Error("expected task description to be updated with note")
	}
}

func TestTaskLink_WithSHARecordsAnchor(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")

	before := time.Now().UTC().Add(-time.Second)
	if err := cli.TaskLink(s, "task1", "fix-tests", "o/r", "abc123"); err != nil {
		t.Fatal(err)
	}

	task, err := s.GetTask(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Branch != "fix-tests" {
		t.Errorf("Branch = %q, want fix-tests", meta.Branch)
	}
	if meta.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want abc123", meta.HeadSHA)
	}
	if meta.LinkedAt == "" {
		t.Fatal("LinkedAt should be set when SHA is provided")
	}
	linked, err := time.Parse(time.RFC3339, meta.LinkedAt)
	if err != nil {
		t.Fatalf("LinkedAt %q not RFC3339: %v", meta.LinkedAt, err)
	}
	if linked.Before(before) || linked.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("LinkedAt = %v, want ~now", linked)
	}
}

func TestTaskLink_WithoutSHALeavesAnchorEmpty(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")

	if err := cli.TaskLink(s, "task1", "fix-tests", "o/r", ""); err != nil {
		t.Fatal(err)
	}

	task, _ := s.GetTask(context.Background(), "task1")
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.HeadSHA != "" || meta.LinkedAt != "" {
		t.Errorf("HeadSHA/LinkedAt should be empty without --sha, got %q/%q", meta.HeadSHA, meta.LinkedAt)
	}
}

func TestTaskLink_NewBranchResetsStalePRData(t *testing.T) {
	// An agent spawned on a stack parent branch may have auto-linked its PR.
	// When the staccato hook links a NEW branch for the same task, the stale
	// PR fields must be cleared so discovery finds the right PR.
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")

	ctx := context.Background()
	stale := `{"branch":"parent-branch","repo":"o/r","pr_number":17,"pr_url":"https://github.com/o/r/pull/17","state":"OPEN"}`
	if err := s.UpdatePRMeta(ctx, "task1", &stale); err != nil {
		t.Fatal(err)
	}

	if err := cli.TaskLink(s, "task1", "new-branch", "o/r", "abc123"); err != nil {
		t.Fatal(err)
	}

	task, _ := s.GetTask(ctx, "task1")
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Branch != "new-branch" {
		t.Errorf("Branch = %q, want new-branch", meta.Branch)
	}
	if meta.PRNumber != 0 {
		t.Errorf("PRNumber = %d, want 0 (stale PR data must be reset on branch change)", meta.PRNumber)
	}
	if meta.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want abc123", meta.HeadSHA)
	}
}

func TestTaskLink_SameBranchKeepsPRData(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")

	ctx := context.Background()
	existing := `{"branch":"fix-tests","repo":"o/r","pr_number":42,"pr_url":"https://github.com/o/r/pull/42","state":"OPEN"}`
	if err := s.UpdatePRMeta(ctx, "task1", &existing); err != nil {
		t.Fatal(err)
	}

	if err := cli.TaskLink(s, "task1", "fix-tests", "o/r", "def456"); err != nil {
		t.Fatal(err)
	}

	task, _ := s.GetTask(ctx, "task1")
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42 (same-branch re-link must keep PR data)", meta.PRNumber)
	}
	if meta.HeadSHA != "def456" {
		t.Errorf("HeadSHA = %q, want def456", meta.HeadSHA)
	}
}
