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

func TestAFreshStartHandsOverWhatTheHistoryKnows(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: "task-1", Title: "scn testing gh app", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	f.ranAndStopped(t, "task-1", old, "conv-1")
	session, _ := f.store.ListTaskConversations(ctx, "task-1")

	long := strings.Repeat("é", conversationTextLimit)
	if _, err := f.store.DB().ExecContext(ctx, `UPDATE agent_conversations SET first_message = ?, last_message = 'It is half done.' WHERE id = ?`, long, session[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	history := f.history()
	id := f.conversationID(t, "task-1", "conv-1")
	entry := entryOf(t, history, "task-1")
	if entry.Restorable || !entry.CanStartFresh {
		t.Fatalf("entry = %+v, want only a fresh start", entry)
	}
	if _, err := history.StartFresh(ctx, id, RestoreIntoTask, ""); !errors.Is(err, ErrBadSessionQuery) {
		t.Fatalf("fresh start with no folder = %v, want ErrBadSessionQuery", err)
	}
	f.clearSent()

	elsewhere := t.TempDir()
	taskID, err := history.StartFresh(ctx, id, RestoreIntoTask, elsewhere)
	if err != nil || taskID != "task-1" {
		t.Fatalf("fresh start = %q, %v", taskID, err)
	}
	lines := f.linesEventually(t, "task-1", 2)
	if len(lines) < 2 || strings.Contains(lines[0], "--resume") || !strings.HasPrefix(lines[0], "env -u CLAUDE_CONFIG_DIR claude") {
		t.Fatalf("lines = %q, want a new conversation", lines)
	}
	// A new conversation's first message is in its prompt file, which the
	// kickoff tells the agent to read.
	kickoff := strings.Join(lines[1:], "\n")
	path, _, ok := strings.Cut(kickoff[strings.Index(kickoff, "Read ")+len("Read "):], " in full")
	if !ok {
		t.Fatalf("kickoff = %q, want it to name the prompt file", kickoff)
	}
	prompt, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	note := string(prompt)
	for _, want := range []string{"[legato handover] ", "scn testing gh app", old, "stopped", long, "It is half done."} {
		if !strings.Contains(note, want) {
			t.Errorf("hand-over lacks %q:\n%s", want[:min(len(want), 40)], note)
		}
	}
	if !strings.HasPrefix(note, "[legato handover] ") {
		t.Errorf("note = %q, want it to open with Legato's tag", note[:min(len(note), 80)])
	}
	if strings.Contains(note, long+"é") {
		t.Error("the clipped first prompt grew in the hand-over")
	}
	agent, err := f.store.GetAgentSessionByTaskID(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if execution, _ := f.store.GetExecution(ctx, agent.ExecutionID); execution.WorkingDir != elsewhere {
		t.Fatalf("fresh agent's folder = %q, want %q", execution.WorkingDir, elsewhere)
	}
}

func TestAFreshStartIsRefusedWhileTheConversationRuns(t *testing.T) {
	f := newRestartFixture(t)
	ctx := context.Background()
	createTask(t, f.store, "task-1")
	cwd := t.TempDir()
	if err := f.svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: cwd}); err != nil {
		t.Fatal(err)
	}
	f.reportAs(t, "task-1", cwd, "conv-1")
	history := f.history()
	if entry := entryOf(t, history, "task-1"); entry.CanStartFresh {
		t.Fatalf("running entry = %+v, want no fresh start", entry)
	}
	if refused := refusal(t, freshErr(history, ctx, f.conversationID(t, "task-1", "conv-1"), cwd)); refused.Reason != "it is running" {
		t.Fatalf("refusal = %+v", refused)
	}
}

func TestAChimeraConversationCanAlwaysStartFresh(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{})
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "chimera", t.TempDir(), store.RunStopped)
	if entry := entryOf(t, h, "t-1"); !entry.Restorable || !entry.CanStartFresh {
		t.Fatalf("chimera entry = %+v, want restorable and a fresh start offered", entry)
	}
}

func freshErr(h *SessionHistoryService, ctx context.Context, id int64, dir string) error {
	_, err := h.StartFresh(ctx, id, RestoreIntoTask, dir)
	return err
}
