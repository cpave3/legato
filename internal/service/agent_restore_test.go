package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// reportAs records conversation as the one the running agent of taskID
// reported, as its bridge does.
func (f restartFixture) reportAs(t *testing.T, taskID, cwd, conversation string) {
	t.Helper()
	ctx := context.Background()
	session, err := f.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RecordTaskNativeSession(ctx, taskID, session.ID, session.ExecutionID, "claude-code", conversation, cwd, ""); err != nil {
		t.Fatal(err)
	}
}

// ranAndStopped spawns a Claude Code agent on taskID in cwd that reports
// conversation, then stops it.
func (f restartFixture) ranAndStopped(t *testing.T, taskID, cwd, conversation string) {
	t.Helper()
	ctx := context.Background()
	if err := f.svc.SpawnAgent(ctx, taskID, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportAs(t, taskID, cwd, conversation)
	if err := f.svc.KillAgent(ctx, taskID); err != nil {
		t.Fatal(err)
	}
}

func (f restartFixture) history(opts ...SessionHistoryOptions) *SessionHistoryService {
	opt := SessionHistoryOptions{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Restorer == nil {
		opt.Restorer = f.svc.(ConversationRestorer)
	}
	return NewSessionHistoryService(f.store, opt)
}

// conversationID is the history entry of a provider conversation.
func (f restartFixture) conversationID(t *testing.T, taskID, conversation string) int64 {
	t.Helper()
	list, err := f.store.ListTaskConversations(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ProviderSessionID == conversation {
			return c.ID
		}
	}
	t.Fatalf("no conversation %s on %s in %#v", conversation, taskID, list)
	return 0
}

func refusal(t *testing.T, err error) *RestoreRefusal {
	t.Helper()
	var refused *RestoreRefusal
	if !errors.As(err, &refused) {
		t.Fatalf("restore = %v, want a refusal", err)
	}
	return refused
}

func TestRestoringAnOlderConversationResumesItAndSaysItWasRestored(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-1")
	cwd := t.TempDir()
	f.ranAndStopped(t, "task-1", cwd, "conv-old")
	f.ranAndStopped(t, "task-1", cwd, "conv-new")
	f.clearSent()

	old := f.conversationID(t, "task-1", "conv-old")
	if err := f.history().Restore(ctx, old, RestoreIntoTask); err != nil {
		t.Fatal(err)
	}
	lines := f.linesEventually(t, "task-1", 2)
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "claude --resume conv-old") {
		t.Fatalf("lines = %q, want the older conversation resumed", lines)
	}
	if !strings.HasPrefix(lines[1], "[legato restored] This conversation was restored from Legato's session history. It last ran ") ||
		!strings.Contains(lines[1], " in "+cwd+";") {
		t.Fatalf("note = %q, want Legato's restored note naming the folder", lines[1])
	}

	// Its bridge reports it, and the task resumes it by default again.
	f.reportAs(t, "task-1", cwd, "conv-old")
	if saved, err := f.store.GetTaskNativeSession(ctx, "task-1", "claude-code"); err != nil || saved.ProviderSessionID != "conv-old" {
		t.Fatalf("saved conversation = %#v, %v; want conv-old", saved, err)
	}
	if runs, _ := f.store.ListConversationRuns(ctx, old); len(runs) != 2 || runs[1].EndReason != nil {
		t.Fatalf("runs of the restored conversation = %#v, want a second, live run", runs)
	}
}

func TestARestoreIsRefusedWhileTheTasksAgentRuns(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-1")
	cwd := t.TempDir()
	f.ranAndStopped(t, "task-1", cwd, "conv-old")
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportAs(t, "task-1", cwd, "conv-new")
	history := f.history()

	refused := refusal(t, history.Restore(ctx, f.conversationID(t, "task-1", "conv-old"), RestoreIntoTask))
	if !refused.NewTaskPossible || !strings.Contains(refused.Reason, "running") {
		t.Fatalf("refusal = %+v, want the running agent named and a new task offered", refused)
	}
	refused = refusal(t, history.Restore(ctx, f.conversationID(t, "task-1", "conv-new"), RestoreIntoTask))
	if refused.Reason != "it is running" {
		t.Fatalf("restore of the running one = %+v", refused)
	}
}

func TestARestoreRefusesWhatCannotBeRestored(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		prepare func(f restartFixture, cwd string)
		reason  string
		opts    SessionHistoryOptions
	}{
		"task archived": {func(f restartFixture, _ string) { _ = f.store.ArchiveTask(ctx, "task-1") }, "its task is archived, so it can be restored into a new task only", SessionHistoryOptions{}},
		"task deleted":  {func(f restartFixture, _ string) { _ = f.store.DeleteTask(ctx, "task-1") }, "its task is gone, so it can be restored into a new task only", SessionHistoryOptions{}},
		"folder gone":   {func(_ restartFixture, cwd string) {}, "is gone", SessionHistoryOptions{}},
		"away":          {func(restartFixture, string) {}, "its task is away on a worker", SessionHistoryOptions{Away: func(context.Context, string) bool { return true }}},
		"no tool":       {func(restartFixture, string) {}, "claude-code is not configured", SessionHistoryOptions{ToolConfigured: func(string) bool { return false }}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRestartFixture(t)
			createTask(t, f.store, "task-1")
			cwd := filepath.Join(t.TempDir(), "work")
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			f.ranAndStopped(t, "task-1", cwd, "conv-1")
			id := f.conversationID(t, "task-1", "conv-1")
			if name == "folder gone" {
				if err := os.RemoveAll(cwd); err != nil {
					t.Fatal(err)
				}
			}
			tc.prepare(f, cwd)
			f.clearSent()
			refused := refusal(t, f.history(tc.opts).Restore(ctx, id, RestoreIntoTask))
			if !strings.Contains(refused.Reason, tc.reason) {
				t.Fatalf("reason = %q, want %q", refused.Reason, tc.reason)
			}
			if running, _ := f.svc.ListAgents(ctx); len(running) != 0 {
				t.Fatalf("a refused restore started %#v", running)
			}
		})
	}
}

func TestRestoringTheInterruptedAgentsConversationResumesIt(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	cwd := t.TempDir()
	f.interruptedAgent(t, "task-i", cwd, true)
	f.clearSent()
	if err := f.history().Restore(ctx, f.conversationID(t, "task-i", "conversation-1"), RestoreIntoTask); err != nil {
		t.Fatal(err)
	}
	lines := f.linesEventually(t, "task-i", 2)
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "claude --resume conversation-1") || !strings.HasPrefix(lines[1], "[legato interrupted] ") {
		t.Fatalf("lines = %q, want the interrupted agent resumed with its note", lines)
	}
	if list, _ := f.interrupted().ListInterruptedAgents(ctx); len(list) != 0 {
		t.Fatalf("interrupted after restore = %#v", list)
	}
}

func TestRestoringAnotherConversationDismissesTheInterruptedAgent(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-i")
	cwd := t.TempDir()
	f.ranAndStopped(t, "task-i", cwd, "conv-old")
	if err := f.svc.SpawnAgent(ctx, "task-i", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportAs(t, "task-i", cwd, "conv-new")
	f.crash()
	if _, err := f.interrupted().SweepInterruptedAgents(ctx); err != nil {
		t.Fatal(err)
	}
	f.clearSent()

	if err := f.history().Restore(ctx, f.conversationID(t, "task-i", "conv-old"), RestoreIntoTask); err != nil {
		t.Fatal(err)
	}
	if lines := f.linesEventually(t, "task-i", 2); len(lines) < 2 || !strings.HasPrefix(lines[0], "claude --resume conv-old") || !strings.HasPrefix(lines[1], "[legato restored] ") {
		t.Fatalf("lines = %q, want conv-old restored", lines)
	}
	if runs, _ := f.store.ListConversationRuns(ctx, f.conversationID(t, "task-i", "conv-new")); len(runs) != 1 || runs[0].EndReason == nil || *runs[0].EndReason != store.RunDismissed {
		t.Fatalf("runs of the interrupted conversation = %#v, want dismissed", runs)
	}
}

// blockingRestorer holds a restore until released.
type blockingRestorer struct {
	started, release chan struct{}
}

func (b blockingRestorer) RestoreConversation(context.Context, ConversationRestore) error {
	b.started <- struct{}{}
	<-b.release
	return nil
}

func TestOneRestoreOfAConversationAtATime(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-1")
	f.ranAndStopped(t, "task-1", t.TempDir(), "conv-1")
	id := f.conversationID(t, "task-1", "conv-1")
	restorer := blockingRestorer{started: make(chan struct{}), release: make(chan struct{})}
	history := f.history(SessionHistoryOptions{Restorer: restorer})
	done := make(chan error)
	go func() { done <- history.Restore(ctx, id, RestoreIntoTask) }()
	<-restorer.started
	if refused := refusal(t, history.Restore(ctx, id, RestoreIntoTask)); !strings.Contains(refused.Reason, "already being restored") {
		t.Fatalf("second restore = %+v", refused)
	}
	close(restorer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAReeveConversationIsRestoredByTheReeve(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	reeve, err := f.store.CreateReeveTask(ctx, "Reeve")
	if err != nil {
		t.Fatal(err)
	}
	// The Reeve runs only in the sandbox, which this fixture has not: its
	// conversation is recorded as its bridge would.
	recordedAgent(t, f.store, reeve, "claude-code", t.TempDir(), store.RunStopped)
	var resumed string
	history := f.history(SessionHistoryOptions{ReeveResume: func(_ context.Context, taskID string) error { resumed = taskID; return nil }})
	if err := history.Restore(ctx, f.conversationID(t, reeve, "conv-"+reeve), RestoreIntoTask); err != nil || resumed != reeve {
		t.Fatalf("restore = %v, Reeve resumed %q", err, resumed)
	}
	if refused := refusal(t, history.Restore(ctx, f.conversationID(t, reeve, "conv-"+reeve), RestoreIntoNewTask)); !strings.Contains(refused.Reason, "Reeve") {
		t.Fatalf("new task for a Reeve = %+v", refused)
	}
}
