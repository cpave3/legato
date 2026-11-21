package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/transcript"
)

// claudeTranscript writes the transcript Claude Code would keep of
// conversation in cwd under home.
func claudeTranscript(t *testing.T, home, cwd, conversation string) string {
	t.Helper()
	path := filepath.Join(home, ".claude", "projects", transcript.ClaudeProjectFolder(cwd), conversation+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"type":"user","sessionId":"` + conversation + `","cwd":"` + cwd + `","timestamp":"2026-11-21T10:00:00Z","message":{"role":"user","content":"[legato brief] Read your brief."}}
{"type":"user","sessionId":"` + conversation + `","cwd":"` + cwd + `","timestamp":"2026-11-21T10:01:00Z","message":{"role":"user","content":"wire the GitHub app"}}
{"type":"assistant","sessionId":"` + conversation + `","cwd":"` + cwd + `","timestamp":"2026-11-21T10:09:00Z","message":{"role":"assistant","content":[{"type":"text","text":"The app is wired."}]}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheHistoryReadsATranscriptForWhatItLacks(t *testing.T) {
	home := t.TempDir()
	h, s := historyService(t, SessionHistoryOptions{Transcripts: &transcript.Reader{Home: home}})
	ctx := context.Background()
	cwd := t.TempDir()
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "claude-code", cwd, store.RunStopped)
	path := claudeTranscript(t, home, cwd, "conv-t-1")

	entry := entryOf(t, h, "t-1")
	if entry.TranscriptState != "present" || entry.FirstMessage != "wire the GitHub app" || entry.LastMessage != "The app is wired." {
		t.Fatalf("entry = %+v, want its transcript found and its messages filled", entry)
	}
	if got, _ := s.MatchAgentConversations(ctx, `"github"`, 5); len(got) != 1 {
		t.Fatalf("search by the transcript's words = %#v", got)
	}
	detail, err := h.Get(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TranscriptPath != path || len(detail.Preview) != 3 || detail.Preview[2].Text != "The app is wired." {
		t.Fatalf("detail = path %q preview %#v", detail.TranscriptPath, detail.Preview)
	}
}

func TestAConversationWhoseTranscriptIsGoneOffersAFreshStart(t *testing.T) {
	now := time.Now()
	home := t.TempDir()
	h, s := historyService(t, SessionHistoryOptions{Transcripts: &transcript.Reader{Home: home}, Now: func() time.Time { return now }})
	cwd := t.TempDir()
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "claude-code", cwd, store.RunStopped)

	entry := entryOf(t, h, "t-1")
	if entry.TranscriptState != "missing" || entry.Restorable || entry.Reason != "its transcript is gone" || !entry.CanStartFresh {
		t.Fatalf("entry = %+v, want its transcript gone, a fresh start offered", entry)
	}
	// Within the hour the check stands; after it the transcript is looked for again.
	claudeTranscript(t, home, cwd, "conv-t-1")
	if entry := entryOf(t, h, "t-1"); entry.TranscriptState != "missing" {
		t.Fatalf("re-checked within the hour: %+v", entry)
	}
	now = now.Add(61 * time.Minute)
	if entry := entryOf(t, h, "t-1"); entry.TranscriptState != "present" || !entry.Restorable {
		t.Fatalf("after an hour = %+v, want it found again", entry)
	}
}

func TestARunningConversationsTranscriptIsNotMarkedGone(t *testing.T) {
	h, s := historyService(t, SessionHistoryOptions{Transcripts: &transcript.Reader{Home: t.TempDir()}})
	createTask(t, s, "t-1")
	recordedAgent(t, s, "t-1", "claude-code", t.TempDir(), "")
	if entry := entryOf(t, h, "t-1"); entry.TranscriptState != "unknown" {
		t.Fatalf("running entry = %+v, want its transcript left unknown", entry)
	}
}
