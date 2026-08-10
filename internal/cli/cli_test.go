package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestTaskList_BoardOrderFiltersAndLimit(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	ctx := context.Background()
	personalID, err := s.CreateWorkspace(ctx, store.Workspace{Name: "Personal"})
	if err != nil {
		t.Fatal(err)
	}
	tasks := []store.Task{
		{ID: "backlog-2", Title: "Second backlog", Status: "Backlog", SortOrder: 2, WorkspaceID: &personalID, CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"},
		{ID: "doing-1", Title: "Doing first", Status: "Doing", SortOrder: 1, CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"},
		{ID: "backlog-1", Title: "First backlog", Status: "Backlog", SortOrder: 1, WorkspaceID: &personalID, CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"},
	}
	for _, task := range tasks {
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}

	got, err := cli.TaskList(s, cli.TaskListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "backlog-1" || got[1].ID != "backlog-2" || got[2].ID != "doing-1" {
		t.Fatalf("TaskList() order = %+v", got)
	}

	got, err = cli.TaskList(s, cli.TaskListOptions{Status: "backlog", Workspace: "personal", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "backlog-1" || got[0].Workspace != "Personal" {
		t.Fatalf("filtered TaskList() = %+v", got)
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
		ID:                  "abc123",
		Title:               "Fetch task content",
		DescriptionMD:       "Local JSON body.",
		RemoteDescriptionMD: "Provider JSON body.",
		Status:              "Backlog",
		Priority:            "Medium",
		Provider:            &provider,
		RemoteID:            &remoteID,
		RemoteMeta:          &remoteMeta,
		CreatedAt:           "2024-01-01T00:00:00Z",
		UpdatedAt:           "2024-01-01T00:00:00Z",
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
	if got["description_md"] != "Provider JSON body.\n\n## Legato details\n\nLocal JSON body." {
		t.Errorf("description_md = %v", got["description_md"])
	}
	if got["local_description_md"] != "Local JSON body." || got["remote_description_md"] != "Provider JSON body." {
		t.Errorf("raw descriptions = %v / %v", got["local_description_md"], got["remote_description_md"])
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

func TestTaskShowFullAndJSONIncludeNotes(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	if err := s.CreateTask(context.Background(), store.Task{ID: "abc123", Title: "Implement", DescriptionMD: "Task description", Status: "Backlog", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertTaskNote(context.Background(), store.TaskNote{ID: "tn-1", TaskID: "abc123", Body: "## Triage\n\nUse bounded retries.", Source: "workflow", CreatedAt: "2026-07-27 10:00:00"}); err != nil {
		t.Fatal(err)
	}
	full, err := cli.TaskShow(s, "abc123", "full")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full, "## Notes") || !strings.Contains(full, "Use bounded retries.") {
		t.Fatalf("full = %q", full)
	}
	raw, err := cli.TaskShow(s, "abc123", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Notes []store.TaskNote `json:"notes"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 || got.Notes[0].ID != "tn-1" {
		t.Fatalf("notes = %#v", got.Notes)
	}
	notes, err := cli.TaskNotes(s, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Body != "## Triage\n\nUse bounded retries." {
		t.Fatalf("notes = %#v", notes)
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

func TestTaskCreate_CreatesLocalTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	card, err := cli.TaskCreate(s, "New CLI task", "Task details", "backlog", "High")
	if err != nil {
		t.Fatalf("TaskCreate: %v", err)
	}
	if card.ID == "" {
		t.Fatal("created task has empty ID")
	}

	task, err := s.GetTask(context.Background(), card.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Title != "New CLI task" || task.DescriptionMD != "Task details" {
		t.Fatalf("created task = %#v", task)
	}
	if task.Status != "Backlog" {
		t.Errorf("status = %q, want Backlog", task.Status)
	}
	if task.Provider != nil {
		t.Errorf("provider = %v, want local task", task.Provider)
	}
}

func TestTaskCreate_AssignsWorkspaceByName(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	workspaceID, err := s.CreateWorkspace(context.Background(), store.Workspace{Name: "Personal"})
	if err != nil {
		t.Fatal(err)
	}

	card, err := cli.TaskCreate(s, "Personal task", "", "Backlog", "", "personal")
	if err != nil {
		t.Fatalf("TaskCreate: %v", err)
	}
	task, err := s.GetTask(context.Background(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.WorkspaceID == nil || *task.WorkspaceID != workspaceID {
		t.Errorf("workspace ID = %v, want %d", task.WorkspaceID, workspaceID)
	}
}

func TestTaskCreate_RequiresTitle(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)

	if _, err := cli.TaskCreate(s, "  ", "", "Backlog", ""); err == nil {
		t.Fatal("expected empty title error")
	}
}

func TestTaskDescription_UpdatesLocalTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	if err := cli.TaskDescription(s, "abc123", "Updated details"); err != nil {
		t.Fatalf("TaskDescription: %v", err)
	}
	task, err := s.GetTask(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if task.Description != "Updated details" || task.DescriptionMD != "Updated details" {
		t.Errorf("descriptions = %q / %q", task.Description, task.DescriptionMD)
	}
}

func TestTaskDescription_UpdatesJiraTaskLegatoDetails(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	provider := "jira"
	if err := s.CreateTask(context.Background(), store.Task{
		ID: "JIRA-1", Title: "Remote", RemoteDescriptionMD: "Provider", Status: "Backlog", Provider: &provider,
	}); err != nil {
		t.Fatal(err)
	}

	if err := cli.TaskDescription(s, "JIRA-1", "Legato detail"); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(context.Background(), "JIRA-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.DescriptionMD != "Legato detail" || task.RemoteDescriptionMD != "Provider" {
		t.Fatalf("descriptions = %q / %q", task.DescriptionMD, task.RemoteDescriptionMD)
	}
}

func TestTaskTitle_UpdatesLocalTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	if err := cli.TaskTitle(s, "abc123", "Updated title"); err != nil {
		t.Fatalf("TaskTitle: %v", err)
	}
	task, err := s.GetTask(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Updated title" {
		t.Errorf("title = %q, want Updated title", task.Title)
	}
}

func TestTaskTitle_RejectsJiraTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	provider := "jira"
	if err := s.CreateTask(context.Background(), store.Task{
		ID: "JIRA-3", Title: "Remote", Status: "Backlog", Provider: &provider,
	}); err != nil {
		t.Fatal(err)
	}

	err := cli.TaskTitle(s, "JIRA-3", "Do not update")
	if err == nil || !strings.Contains(err.Error(), "cannot edit title") {
		t.Fatalf("error = %v, want remote edit rejection", err)
	}
}

func TestTaskWorkspace_AssignsAndClearsByName(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	workspaceID, err := s.CreateWorkspace(context.Background(), store.Workspace{Name: "Personal"})
	if err != nil {
		t.Fatal(err)
	}
	seedTask(t, s, "abc123", "Backlog")

	if err := cli.TaskWorkspace(s, "abc123", "PERSONAL"); err != nil {
		t.Fatalf("TaskWorkspace assign: %v", err)
	}
	task, _ := s.GetTask(context.Background(), "abc123")
	if task.WorkspaceID == nil || *task.WorkspaceID != workspaceID {
		t.Fatalf("workspace ID = %v, want %d", task.WorkspaceID, workspaceID)
	}

	if err := cli.TaskWorkspace(s, "abc123", "unassigned"); err != nil {
		t.Fatalf("TaskWorkspace clear: %v", err)
	}
	task, _ = s.GetTask(context.Background(), "abc123")
	if task.WorkspaceID != nil {
		t.Errorf("workspace ID = %v, want nil", task.WorkspaceID)
	}
}

func TestTaskWorkspace_RejectsUnknownName(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Backlog")

	err := cli.TaskWorkspace(s, "abc123", "Missing")
	if err == nil || !strings.Contains(err.Error(), "valid workspaces") {
		t.Fatalf("error = %v, want valid workspace list", err)
	}
}

func TestWorkspaceList_TextAndJSON(t *testing.T) {
	s := newTestStore(t)
	color := "#7BC47F"
	if _, err := s.CreateWorkspace(context.Background(), store.Workspace{Name: "Personal", Color: &color}); err != nil {
		t.Fatal(err)
	}

	text, err := cli.WorkspaceList(s, "text")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Personal" {
		t.Errorf("text = %q, want Personal", text)
	}

	out, err := cli.WorkspaceList(s, "json")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["name"] != "Personal" || got[0]["color"] != color {
		t.Errorf("JSON = %#v", got)
	}
}

func TestTaskNote_RejectsJiraTask(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	provider := "jira"
	if err := s.CreateTask(context.Background(), store.Task{
		ID: "JIRA-2", Title: "Remote", Description: "Original", Status: "Backlog", Provider: &provider,
	}); err != nil {
		t.Fatal(err)
	}

	if err := cli.TaskNote(s, "JIRA-2", "Do not append"); err == nil {
		t.Fatal("expected Jira-backed note rejection")
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

func linkTestExecution(t *testing.T, s *store.Store, taskID string) string {
	t.Helper()
	ctx := context.Background()
	session, err := s.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	id := "exec-" + taskID
	now := time.Now().UTC()
	if err := s.CreateExecution(ctx, store.Execution{ID: id, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: "Test " + taskID, AgentSessionID: &session.ID, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAgentState_LegacyHookUpdatesOperationalStatusWithoutReporting(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	linkTestExecution(t, s, "abc123")

	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", Activity: "working"}, nil, nil); err != nil {
		t.Fatalf("legacy AgentState: %v", err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != "working" {
		t.Fatalf("activity = %q, want working", session.Activity)
	}
	var activityRows int
	if err := s.DB().Get(&activityRows, `SELECT COUNT(*) FROM execution_activity WHERE execution_id='exec-abc123'`); err != nil {
		t.Fatal(err)
	}
	if activityRows != 0 {
		t.Fatalf("legacy hook created %d reporting rows", activityRows)
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

	linkTestExecution(t, s, "abc123")
	err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "working"}, nil, nil)
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

type recordingNotifier struct {
	calls []struct{ title, message string }
	keys  []string
}

func (n *recordingNotifier) Notify(title, message string) error {
	n.calls = append(n.calls, struct{ title, message string }{title, message})
	return nil
}
func (n *recordingNotifier) Configured() bool { return true }
func (n *recordingNotifier) CanNotify(key string) bool {
	n.keys = append(n.keys, key)
	return true
}

func TestAgentState_OnErrorPersistsAndNotifiesOnce(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, "abc123")
	if err := s.UpdateTaskNotifyEnabled(ctx, "abc123", true); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	report := cli.AgentStateReport{TaskID: "abc123", ExecutionID: executionID, Activity: store.ActivityError, Event: "OnError"}
	if err := cli.AgentState(s, report, notifier, notifier); err != nil {
		t.Fatalf("AgentState OnError: %v", err)
	}
	if err := cli.AgentState(s, report, notifier, notifier); err != nil {
		t.Fatalf("repeated AgentState OnError: %v", err)
	}

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != store.ActivityError {
		t.Fatalf("activity = %q, want error", session.Activity)
	}
	var open int
	if err := s.DB().Get(&open, `SELECT COUNT(*) FROM execution_activity WHERE execution_id=? AND state='error' AND ended_at IS NULL`, executionID); err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("open error spans = %d, want 1", open)
	}
	if len(notifier.calls) != 2 {
		t.Fatalf("notifications = %+v, want one per channel", notifier.calls)
	}
	for _, call := range notifier.calls {
		if call.title != "Agent error" || call.message != "Agent Test abc123 had an error" {
			t.Fatalf("notification = %+v", call)
		}
	}
}

func TestAgentState_UserInputRequiredMarksParentWaiting(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, "abc123")
	if err := s.RecordExecutionActivity(ctx, executionID, store.ActivityWorking, "/repo"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "abc123", "parent-1", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskNotifyEnabled(ctx, "abc123", true); err != nil {
		t.Fatal(err)
	}

	notifier := &recordingNotifier{}
	report := cli.AgentStateReport{TaskID: "abc123", ExecutionID: executionID, Activity: store.ActivityWaiting, WorkingDir: "/repo", SessionID: "parent-1", Event: "UserInputRequired"}
	if err := cli.AgentState(s, report, notifier, notifier); err != nil {
		t.Fatalf("AgentState UserInputRequired: %v", err)
	}

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != store.ActivityWaiting {
		t.Fatalf("activity = %q, want waiting", session.Activity)
	}
	var workingClosed, waitingOpen int
	if err := s.DB().Get(&workingClosed, `SELECT COUNT(*) FROM execution_activity WHERE execution_id=? AND state='working' AND ended_at IS NOT NULL`, executionID); err != nil {
		t.Fatal(err)
	}
	if workingClosed != 1 {
		t.Fatalf("closed working spans = %d, want 1", workingClosed)
	}
	if err := s.DB().Get(&waitingOpen, `SELECT COUNT(*) FROM execution_activity WHERE execution_id=? AND state='waiting' AND ended_at IS NULL`, executionID); err != nil {
		t.Fatal(err)
	}
	if waitingOpen != 1 {
		t.Fatalf("open waiting spans = %d, want 1", waitingOpen)
	}
	if len(notifier.calls) != 2 {
		t.Fatalf("notifications = %+v, want one per channel", notifier.calls)
	}
	for _, call := range notifier.calls {
		if call.title != "Agent ready" || call.message != "Agent Test abc123 is now waiting" {
			t.Fatalf("notification = %+v", call)
		}
	}
}

func TestAgentState_ChildUserInputRequiredDoesNotMutateParent(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, "abc123")
	if err := s.RecordExecutionActivity(ctx, executionID, store.ActivityWorking, "/repo"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"subagentId":"sub-1","sessionId":"child-1","purpose":"Review tests","status":"running"}]`)
	}))
	defer srv.Close()
	if err := s.UpdateChimeraDiscovery(ctx, "abc123", "parent-1", srv.URL); err != nil {
		t.Fatal(err)
	}

	notifier := &recordingNotifier{}
	report := cli.AgentStateReport{TaskID: "abc123", ExecutionID: executionID, Activity: store.ActivityWaiting, WorkingDir: "/repo", SessionID: "child-1", Event: "UserInputRequired"}
	if err := cli.AgentState(s, report, notifier, notifier); err != nil {
		t.Fatalf("AgentState child UserInputRequired: %v", err)
	}

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != store.ActivityWorking {
		t.Fatalf("parent activity = %q, want working", session.Activity)
	}
	var waitingRows, workingOpen int
	if err := s.DB().Get(&waitingRows, `SELECT COUNT(*) FROM execution_activity WHERE execution_id=? AND state='waiting'`, executionID); err != nil {
		t.Fatal(err)
	}
	if waitingRows != 0 {
		t.Fatalf("waiting spans = %d, want 0", waitingRows)
	}
	if err := s.DB().Get(&workingOpen, `SELECT COUNT(*) FROM execution_activity WHERE execution_id=? AND state='working' AND ended_at IS NULL`, executionID); err != nil {
		t.Fatal(err)
	}
	if workingOpen != 1 {
		t.Fatalf("open working spans = %d, want 1", workingOpen)
	}
	if len(notifier.calls) != 0 {
		t.Fatalf("notifications = %+v, want none", notifier.calls)
	}
}

func TestAgentState_ChildOnErrorNotifiesWithoutMutatingParent(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, "abc123")
	if err := s.UpdateTaskNotifyEnabled(ctx, "abc123", true); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"subagentId":"sub-1","sessionId":"child-1","purpose":"Review tests","status":"running"}]`)
	}))
	defer srv.Close()
	if err := s.UpdateChimeraDiscovery(ctx, "abc123", "parent-1", srv.URL); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: executionID, Activity: store.ActivityError, SessionID: "child-1", Event: "OnError"}, notifier, notifier); err != nil {
		t.Fatalf("AgentState child OnError: %v", err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != store.ActivityWorking {
		t.Fatalf("parent activity = %q, want working", session.Activity)
	}
	if len(notifier.calls) != 2 || notifier.calls[0].title != "Agent error" || notifier.calls[0].message != "Agent Review tests for Test abc123 had an error" {
		t.Fatalf("notifications = %+v", notifier.calls)
	}
}

func TestAgentState_SubagentCompletionNotifiesWithoutClearingParent(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", "working"); err != nil {
		t.Fatal(err)
	}
	executionID := linkTestExecution(t, s, "abc123")
	if err := s.RecordExecutionActivity(ctx, executionID, store.ActivityWorking, "/repo"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateTaskNotifyEnabled(ctx, "abc123", true); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"subagentId":"sub-1","sessionId":"child-1","purpose":"Review tests","status":"running"}]`)
	}))
	defer srv.Close()
	if err := s.UpdateChimeraDiscovery(ctx, "abc123", "parent-1", srv.URL); err != nil {
		t.Fatal(err)
	}

	notifier := &recordingNotifier{}
	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "", WorkingDir: "/repo", SessionID: "child-1", Event: "Stop"}, notifier, notifier); err != nil {
		t.Fatalf("AgentState Stop: %v", err)
	}
	if len(notifier.calls) != 0 {
		t.Fatalf("Stop notifications = %+v", notifier.calls)
	}
	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "", WorkingDir: "/repo", SessionID: "child-1", Event: "SessionEnd"}, notifier, notifier); err != nil {
		t.Fatalf("AgentState SessionEnd: %v", err)
	}

	session, err := s.GetAgentSessionByTaskID(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if session.Activity != "working" {
		t.Fatalf("parent activity = %q, want working", session.Activity)
	}
	if len(notifier.calls) != 2 {
		t.Fatalf("notifications = %+v", notifier.calls)
	}
	if len(notifier.keys) != 1 || notifier.keys[0] != "abc123" {
		t.Fatalf("notification keys = %v", notifier.keys)
	}
	if want := "Agent Review tests for Test abc123 is now idle"; notifier.calls[0].message != want {
		t.Fatalf("message = %q, want %q", notifier.calls[0].message, want)
	}
}

func TestAgentState_ParentSessionStillClearsActivity(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", "working"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "abc123", "parent-1", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}

	linkTestExecution(t, s, "abc123")
	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "", SessionID: "parent-1"}, nil, nil); err != nil {
		t.Fatalf("AgentState: %v", err)
	}
	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "" {
		t.Fatalf("activity = %q, want idle", session.Activity)
	}
}

func TestAgentState_UnresolvedSessionDoesNotMutateParent(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", "working"); err != nil {
		t.Fatal(err)
	}

	linkTestExecution(t, s, "abc123")
	err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "", SessionID: "unknown-child"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown-child") || !strings.Contains(err.Error(), "abc123") {
		t.Fatalf("error = %v", err)
	}
	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "working" {
		t.Fatalf("parent activity = %q, want working", session.Activity)
	}
}

func TestAgentState_SubagentLookupFailureDoesNotMutateParent(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "abc123", "Doing")
	ctx := context.Background()
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "abc123", TmuxSession: "legato-abc123", AgentKind: "chimera", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAgentActivity(ctx, "abc123", "working"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if err := s.UpdateChimeraDiscovery(ctx, "abc123", "parent-1", srv.URL); err != nil {
		t.Fatal(err)
	}

	linkTestExecution(t, s, "abc123")
	err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "", SessionID: "child-1", Event: "SessionEnd"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "child-1") {
		t.Fatalf("error = %v", err)
	}
	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "working" {
		t.Fatalf("parent activity = %q, want working", session.Activity)
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

	linkTestExecution(t, s, "abc123")
	cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "working"}, nil, nil)
	cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: ""}, nil, nil)

	session, _ := s.GetAgentSessionByTaskID(ctx, "abc123")
	if session.Activity != "" {
		t.Errorf("activity = %q, want empty", session.Activity)
	}
}

func TestAgentState_RecordsExecutionActivity(t *testing.T) {
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

	linkTestExecution(t, s, "abc123")
	cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "working"}, nil, nil)

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

	linkTestExecution(t, s, "abc123")
	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "working"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := cli.AgentState(s, cli.AgentStateReport{TaskID: "abc123", ExecutionID: "exec-abc123", Activity: "waiting"}, nil, nil); err != nil {
		t.Fatal(err)
	}

	durations, err := s.GetStateDurations(ctx, "abc123")
	if err != nil {
		t.Fatalf("GetStateDurations: %v", err)
	}
	if _, ok := durations["working"]; !ok {
		t.Error("expected working duration after transition")
	}
	var waitingOpen int
	if err := s.DB().Get(&waitingOpen, `SELECT COUNT(*) FROM execution_activity WHERE execution_id='exec-abc123' AND state='waiting' AND ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if waitingOpen != 1 {
		t.Fatalf("open waiting spans = %d, want 1", waitingOpen)
	}
}

func TestAgentSummary_MixedStates(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "task1", "Doing")
	seedTask(t, s, "task2", "Doing")
	seedTask(t, s, "task3", "Doing")
	seedTask(t, s, "task4", "Doing")

	ctx := context.Background()
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task1", TmuxSession: "legato-task1", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task2", TmuxSession: "legato-task2", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task3", TmuxSession: "legato-task3", Command: "shell", Status: "running"})
	s.InsertAgentSession(ctx, store.AgentSession{TaskID: "task4", TmuxSession: "legato-task4", Command: "shell", Status: "running"})
	s.UpdateAgentActivity(ctx, "task1", "working")
	s.UpdateAgentActivity(ctx, "task2", "waiting")
	s.UpdateAgentActivity(ctx, "task4", "error")
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
	if !strings.Contains(out, "#[fg=red]1 error") {
		t.Errorf("output missing red error count: %q", out)
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
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if codeContext.Branch != "fix-tests" {
		t.Errorf("Branch = %q, want fix-tests", codeContext.Branch)
	}
	if codeContext.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want abc123", codeContext.HeadSHA)
	}
	if codeContext.LinkedAt == "" {
		t.Fatal("LinkedAt should be set when SHA is provided")
	}
	linked, err := time.Parse(time.RFC3339, codeContext.LinkedAt)
	if err != nil {
		t.Fatalf("LinkedAt %q not RFC3339: %v", codeContext.LinkedAt, err)
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
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if codeContext.HeadSHA != "" || codeContext.LinkedAt == "" {
		t.Errorf("HeadSHA should be empty and LinkedAt set, got %q/%q", codeContext.HeadSHA, codeContext.LinkedAt)
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
	if err := s.SetCodeContext(ctx, "task1", &store.CodeContext{Repo: "o/r", Branch: "parent-branch"}); err != nil {
		t.Fatal(err)
	}

	if err := cli.TaskLink(s, "task1", "new-branch", "o/r", "abc123"); err != nil {
		t.Fatal(err)
	}

	task, _ := s.GetTask(ctx, "task1")
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if codeContext.Branch != "new-branch" || codeContext.HeadSHA != "abc123" {
		t.Fatalf("code context = %#v", codeContext)
	}
	if task.PRMeta != nil {
		t.Errorf("stale PR data was not reset: %s", *task.PRMeta)
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
	if err := s.SetCodeContext(ctx, "task1", &store.CodeContext{Repo: "o/r", Branch: "fix-tests"}); err != nil {
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
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if codeContext.HeadSHA != "def456" {
		t.Errorf("HeadSHA = %q, want def456", codeContext.HeadSHA)
	}
}
