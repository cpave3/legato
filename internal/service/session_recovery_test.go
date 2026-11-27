package service

import (
	"context"
	"fmt"
	"github.com/cpave3/legato/config"
	"os"
	"path/filepath"
	"strings"
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

func TestRecoveryScansOnlyTheClaudeRunsLaunchAccount(t *testing.T) {
	ctx := context.Background()
	home, namedHome, otherHome := t.TempDir(), t.TempDir(), t.TempDir()
	h, s := historyService(t, SessionHistoryOptions{Transcripts: &transcript.Reader{Home: home}})
	cwd := t.TempDir()
	start := time.Now().Add(-time.Hour)
	end := start.Add(30 * time.Minute)
	createTask(t, s, "named")
	createTask(t, s, "other")
	namedRoot, otherRoot := filepath.Join(namedHome, ".claude"), filepath.Join(otherHome, ".claude")
	pastRun(t, s, "ex-named", "named", cwd, start, end)
	identity := store.AgentAccountIdentity{Provider: "claude", Account: "work", ConfigDir: namedRoot, ResolvedDir: namedRoot, ConfigEnvSet: true}
	if err := s.PutExecutionAccount(ctx, "ex-named", identity); err != nil {
		t.Fatal(err)
	}
	// Make the other root known to history, with the same provider session ID.
	pastRun(t, s, "ex-other", "other", cwd, start, end)
	if _, err := s.RecordRecoveredConversation(ctx, store.UnrecordedExecution{ID: "ex-other", TaskID: "other", Status: store.ExecutionLost, StartedAt: start, EndedAt: &end}, store.RecoveredConversation{AgentKind: "claude-code", ProviderSessionID: "shared", StorageDir: otherRoot, CWD: cwd, StartedAt: start}); err != nil {
		t.Fatal(err)
	}
	transcriptAt(t, namedHome, cwd, "shared", start)
	transcriptAt(t, otherHome, cwd, "shared", start)
	transcriptAt(t, otherHome, cwd, "other-only", start)
	transcriptAt(t, home, cwd, "default-only", start)
	if n, err := h.Recover(ctx); err != nil || n != 1 {
		t.Fatalf("Recover = %d, %v", n, err)
	}
	rows, err := s.ListTaskConversations(ctx, "named")
	if err != nil || len(rows) != 1 || rows[0].ProviderSessionID != "shared" || rows[0].StorageDir != namedRoot {
		t.Fatalf("named recovery = %+v, %v", rows, err)
	}
	other, err := s.ListTaskConversations(ctx, "other")
	if err != nil || len(other) != 1 {
		t.Fatalf("other history = %+v, %v", other, err)
	}
	runs, err := s.ListConversationRuns(ctx, other[0].ID)
	if err != nil || len(runs) != 1 || runs[0].ExecutionID != "ex-other" {
		t.Fatalf("other runs = %+v, %v", runs, err)
	}
}

func TestResumingHistoricalConversationKeepsItsNamedAccount(t *testing.T) {
	for _, configured := range []bool{true, false} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			f := newRestartFixture(t)
			ctx := context.Background()
			for _, key := range namedAccountAuthVars {
				t.Setenv(key, "")
			}
			cwd, root := t.TempDir(), t.TempDir()
			// The current default points to the same folder: it must not replace
			// the historical named identity, even if that account was removed.
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			if err := os.WriteFile(filepath.Join(root, ".credentials.json"), []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fixture-only","expiresAt":%d}}`, time.Now().Add(time.Hour).UnixMilli())), 0600); err != nil {
				t.Fatal(err)
			}
			createTask(t, f.store, "history-account")
			start := time.Now().Add(-time.Hour)
			end := start.Add(time.Minute)
			pastRun(t, f.store, "ex-history", "history-account", cwd, start, end)
			identity := store.AgentAccountIdentity{Provider: "claude", Account: "work", ConfigDir: root, ResolvedDir: root, ConfigEnvSet: true}
			if err := f.store.PutExecutionAccount(ctx, "ex-history", identity); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.RecordRecoveredConversation(ctx, store.UnrecordedExecution{ID: "ex-history", TaskID: "history-account", Status: store.ExecutionLost, StartedAt: start, EndedAt: &end}, store.RecoveredConversation{AgentKind: "claude-code", ProviderSessionID: "old-named", StorageDir: root, CWD: cwd, StartedAt: start}); err != nil {
				t.Fatal(err)
			}
			if configured {
				t.Setenv("CLAUDE_CONFIG_DIR", "")
				f.svc.(*agentService).claudeAccounts = []config.ClaudeAccount{{Name: "work", ConfigDir: root}}
			}
			err := f.svc.SpawnAgent(ctx, "history-account", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", SessionPolicy: "resume", ResumeSessionID: "old-named", Unsandboxed: true})
			if !configured {
				if err == nil {
					t.Fatal("resumed a removed named account as the current default")
				}
				if !strings.Contains(err.Error(), "work") {
					t.Fatalf("refusal does not identify missing account: %v", err)
				}
				if got := f.tmux.sentLinesFor("legato-history-account"); len(got) != 0 {
					t.Fatalf("launched after refusal: %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			session, err := f.store.GetAgentSessionByTaskID(ctx, "history-account")
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.store.ExecutionAccount(ctx, session.ExecutionID)
			if err != nil || got == nil || *got != identity {
				t.Fatalf("resumed identity = %+v, %v; want %+v", got, err, identity)
			}
		})
	}
}
