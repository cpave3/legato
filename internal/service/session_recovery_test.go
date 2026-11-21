package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/transcript"
)

// pastRun is an ended interactive execution of claude-code on taskID in
// cwd from start to end that never reported a conversation.
func pastRun(t *testing.T, s *store.Store, id, taskID, cwd string, start, end time.Time) {
	t.Helper()
	if err := s.CreateExecution(context.Background(), store.Execution{
		ID: id, Kind: store.ExecutionInteractive, Status: store.ExecutionLost, TaskID: taskID, ExecutionTaskID: taskID,
		TaskTitle: "Title of " + taskID, AgentKind: "claude-code", WorkingDir: cwd, StartedAt: start, EndedAt: &end,
	}); err != nil {
		t.Fatal(err)
	}
}

// transcriptAt writes a Claude Code transcript of conversation in cwd that
// starts at start.
func transcriptAt(t *testing.T, home, cwd, conversation string, start time.Time) {
	t.Helper()
	path := filepath.Join(home, ".claude", "projects", transcript.ClaudeProjectFolder(cwd), conversation+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":%q,"timestamp":%q,"message":{"role":"user","content":"work on %s"}}`+"\n",
		conversation, cwd, start.UTC().Format(time.RFC3339), conversation)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func recovered(t *testing.T, s *store.Store) map[string][]string {
	t.Helper()
	rows, err := s.ListAgentConversations(context.Background(), store.ConversationFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, row := range rows {
		runs, _ := s.ListConversationRuns(context.Background(), row.ID)
		for _, run := range runs {
			out[row.ProviderSessionID] = append(out[row.ProviderSessionID], row.Source+":"+run.ExecutionID)
		}
	}
	return out
}

func TestRecoveryFindsTheConversationsOfRunsThatNeverReportedOne(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	h, s := historyService(t, SessionHistoryOptions{Transcripts: &transcript.Reader{Home: home}})
	start := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	cwd, shared := t.TempDir(), t.TempDir()
	createTask(t, s, "t-1")
	createTask(t, s, "t-2")
	createTask(t, s, "t-3")

	// One run, one transcript that began just as it did.
	pastRun(t, s, "ex-1", "t-1", cwd, start, start.Add(time.Hour))
	transcriptAt(t, home, cwd, "conv-1", start.Add(30*time.Second))
	// One that began an hour before the run, and one after it: neither is its.
	transcriptAt(t, home, cwd, "before", start.Add(-time.Hour))
	transcriptAt(t, home, cwd, "after", start.Add(2*time.Hour))
	// Two agents in one folder at once: each run lists both.
	pastRun(t, s, "ex-2", "t-2", shared, start, start.Add(time.Hour))
	pastRun(t, s, "ex-3", "t-3", shared, start.Add(5*time.Minute), start.Add(time.Hour))
	transcriptAt(t, home, shared, "conv-a", start.Add(6*time.Minute))
	transcriptAt(t, home, shared, "conv-b", start.Add(10*time.Minute))

	if _, err := h.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got := recovered(t, s)
	if fmt.Sprint(got["conv-1"]) != "[recovered:ex-1]" || got["before"] != nil || got["after"] != nil {
		t.Fatalf("recovered = %v, want conv-1 on ex-1 only", got)
	}
	if fmt.Sprint(got["conv-a"]) != "[recovered:ex-2 recovered:ex-3]" || fmt.Sprint(got["conv-b"]) != "[recovered:ex-2 recovered:ex-3]" {
		t.Fatalf("recovered = %v, want both of the shared folder's on both runs", got)
	}
	entry := entryOf(t, h, "t-1")
	if entry.Source != "recovered" || entry.TaskTitle != "Test t-1" || entry.FirstMessage != "work on conv-1" || !entry.Restorable {
		t.Fatalf("entry = %+v", entry)
	}

	// A second pass reads nothing again.
	if n, err := h.Recover(ctx); err != nil || n != 0 {
		t.Fatalf("second pass = %d, %v; want nothing new", n, err)
	}
}

func TestRecoveryNeverListsARecordedConversationAgain(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	h, s := historyService(t, SessionHistoryOptions{Transcripts: &transcript.Reader{Home: home}})
	cwd := t.TempDir()
	createTask(t, s, "t-1")
	createTask(t, s, "t-2")
	recordedAgent(t, s, "t-1", "claude-code", cwd, store.RunStopped) // conv-t-1, recorded
	start := time.Now().Add(-time.Hour)
	pastRun(t, s, "ex-later", "t-2", cwd, start, start.Add(30*time.Minute))
	transcriptAt(t, home, cwd, "conv-t-1", start.Add(time.Minute))

	if _, err := h.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got := recovered(t, s); fmt.Sprint(got["conv-t-1"]) != "[recorded:ex-t-1]" {
		t.Fatalf("recovered = %v, want the recorded one left as it was", got)
	}
}
