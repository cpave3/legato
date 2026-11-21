package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// historyOf records conversations on three tasks: a live Claude Code one,
// an ended Codex one in a job clone, and one whose task is deleted.
func historyOf(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, Task{ID: "task-a", Title: "Docker in the sandbox", Status: "Doing", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	createTestTask(t, s, "task-b")
	createTestTask(t, s, "task-c")
	if _, err := s.CreateJobClone(ctx, JobClone{ExecutionID: "ex-b", TaskID: "task-b", Repo: "cpave3/legato", Branch: "feat", Path: "/jobs/b", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	record := func(task, kind, execution, provider, cwd string, sandboxed bool) {
		owner := nativeSessionOwner(t, s, task, kind, execution, func(a *AgentSession, _ *Execution) { a.Sandboxed = sandboxed })
		if _, err := s.RecordTaskNativeSession(ctx, task, owner, execution, kind, provider, cwd, ""); err != nil {
			t.Fatal(err)
		}
	}
	record("task-a", "claude-code", "ex-a", "conv-a", "/repo", true)
	record("task-b", "codex", "ex-b", "thread-b", "/jobs/b", false)
	if err := s.TerminalizeExecution(ctx, "ex-b", ExecutionCancelled); err != nil {
		t.Fatal(err)
	}
	record("task-c", "claude-code", "ex-c", "conv-c", "/repo", false)
	if err := s.TerminalizeExecutionEndingRuns(ctx, "ex-c", ExecutionLost, RunInterrupted); err != nil {
		t.Fatal(err)
	}
	return s
}

func listedIDs(rows []AgentConversationRow) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ProviderSessionID
	}
	return ids
}

func TestTheHistoryListsWithItsFilters(t *testing.T) {
	s := historyOf(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		filter ConversationFilter
		want   []string
	}{
		{"all, newest first", ConversationFilter{}, []string{"conv-c", "thread-b", "conv-a"}},
		{"by tool", ConversationFilter{AgentKind: "codex"}, []string{"thread-b"}},
		{"by task", ConversationFilter{TaskID: "task-a"}, []string{"conv-a"}},
		{"by repository", ConversationFilter{Repo: "cpave3/legato"}, []string{"thread-b"}},
		{"running", ConversationFilter{State: "running"}, []string{"conv-a"}},
		{"interrupted", ConversationFilter{State: "interrupted"}, []string{"conv-c"}},
		{"ended", ConversationFilter{State: "ended"}, []string{"thread-b"}},
		{"search", ConversationFilter{Match: `"docker"*`}, []string{"conv-a"}},
		{"paged", ConversationFilter{Limit: 1, Offset: 1}, []string{"thread-b"}},
		{"since", ConversationFilter{Since: time.Now().Add(time.Hour)}, []string{}},
	}
	for _, tc := range cases {
		got, err := s.ListAgentConversations(ctx, tc.filter)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if ids := listedIDs(got); len(ids) != len(tc.want) || (len(ids) > 0 && ids[0] != tc.want[0]) || len(ids) > 1 && ids[len(ids)-1] != tc.want[len(tc.want)-1] {
			t.Errorf("%s = %v, want %v", tc.name, ids, tc.want)
		}
	}
}

func TestAListedConversationSaysHowItStands(t *testing.T) {
	s := historyOf(t)
	ctx := context.Background()
	if err := s.DeleteTask(ctx, "task-c"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListAgentConversations(ctx, ConversationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]AgentConversationRow{}
	for _, row := range rows {
		by[row.ProviderSessionID] = row
	}
	if a := by["conv-a"]; !a.Live || a.RunCount != 1 || !a.TaskExists || a.TaskTitle != "Docker in the sandbox" {
		t.Errorf("live row = %#v", a)
	}
	if b := by["thread-b"]; b.Live || b.LastEndReason != RunStopped || b.Repo != "cpave3/legato" || b.Branch != "feat" {
		t.Errorf("ended row = %#v", b)
	}
	if c := by["conv-c"]; c.TaskExists || c.LastEndReason != RunInterrupted || c.TaskTitle != "Test task task-c" {
		t.Errorf("row of a deleted task = %#v", c)
	}
}

func TestARunKeepsWhetherItWasSandboxed(t *testing.T) {
	s := historyOf(t)
	ctx := context.Background()
	rows, _ := s.ListAgentConversations(ctx, ConversationFilter{TaskID: "task-a"})
	runs, err := s.ListConversationRuns(ctx, rows[0].ID)
	if err != nil || len(runs) != 1 || runs[0].Sandboxed == nil || !*runs[0].Sandboxed {
		t.Fatalf("runs = %#v, %v; want sandboxed", runs, err)
	}
}

func TestForgettingAConversation(t *testing.T) {
	s := historyOf(t)
	ctx := context.Background()
	rows, _ := s.ListAgentConversations(ctx, ConversationFilter{})
	ids := map[string]int64{}
	for _, row := range rows {
		ids[row.ProviderSessionID] = row.ID
	}
	for _, busy := range []string{"conv-a", "conv-c"} {
		if err := s.ForgetAgentConversation(ctx, ids[busy]); !errors.Is(err, ErrConflict) {
			t.Fatalf("forget %s = %v, want ErrConflict", busy, err)
		}
	}
	if err := s.ForgetAgentConversation(ctx, ids["thread-b"]); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetAgentConversation(ctx, ids["thread-b"]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forget again = %v, want ErrNotFound", err)
	}
	if got, _ := s.GetAgentConversation(ctx, ids["thread-b"]); got != nil {
		t.Fatalf("forgotten conversation = %#v", got)
	}
	if runs, _ := s.ListConversationRuns(ctx, ids["thread-b"]); len(runs) != 0 {
		t.Fatalf("forgotten runs = %#v", runs)
	}
	if got, _ := s.MatchAgentConversations(ctx, `"jobs"`, 5); len(got) != 0 {
		t.Fatalf("forgotten search row = %#v", got)
	}
	// The task's own resume record stays.
	if saved, err := s.GetTaskNativeSession(ctx, "task-b", "codex"); err != nil || saved == nil {
		t.Fatalf("resume record after forget = %#v, %v", saved, err)
	}
}
