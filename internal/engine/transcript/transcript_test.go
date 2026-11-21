package transcript

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Lines as Claude Code 2.1 writes them.
var claudeLines = []string{
	`{"type":"mode","mode":"default","sessionId":"conv-1"}`,
	`{"type":"user","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:00:00.000Z","isSidechain":false,"message":{"role":"user","content":"[legato brief] Read your brief."}}`,
	`{"type":"user","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:00:05.000Z","isSidechain":false,"message":{"role":"user","content":"<command-name>/model</command-name>"}}`,
	`not json at all`,
	`{"type":"assistant","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:00:10.000Z","isSidechain":false,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"tool_use","name":"Read"}]}}`,
	`{"type":"user","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:00:11.000Z","isSidechain":false,"message":{"role":"user","content":[{"type":"tool_result","content":"file text"}]}}`,
	`{"type":"user","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:01:00.000Z","isSidechain":true,"message":{"role":"user","content":"a sub-agent's prompt"}}`,
	`{"type":"user","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:02:00.000Z","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"fix the flaky test"}]}}`,
	`{"type":"assistant","sessionId":"conv-1","cwd":"/home/u/.local/work","timestamp":"2026-11-21T10:05:00.000Z","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"Fixed: it waited on a timer."}]}}`,
}

func TestAClaudeCodeTranscriptIsFoundByItsFolderAndRead(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "projects", "-home-u--local-work", "conv-1.jsonl")
	write(t, path, claudeLines...)
	r := Reader{Home: home}

	found, err := r.Find(ClaudeCode, "", "/home/u/.local/work", "conv-1")
	if err != nil || found != path {
		t.Fatalf("find = %q, %v; want %q", found, err, path)
	}
	got, err := Read(ClaudeCode, found, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "conv-1" || got.CWD != "/home/u/.local/work" {
		t.Errorf("identity = %q in %q", got.SessionID, got.CWD)
	}
	if strings.Join(got.FirstUser, "|") != "[legato brief] Read your brief.|fix the flaky test" {
		t.Errorf("first user messages = %q", got.FirstUser)
	}
	if got.LastAssistant != "Fixed: it waited on a timer." {
		t.Errorf("last assistant = %q", got.LastAssistant)
	}
	if !got.StartedAt.Equal(time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)) || !got.EndedAt.Equal(time.Date(2026, 11, 21, 10, 5, 0, 0, time.UTC)) {
		t.Errorf("times = %v to %v", got.StartedAt, got.EndedAt)
	}
	if len(got.Recent) != 3 || got.Recent[2].Role != "assistant" {
		t.Errorf("recent = %#v, want the brief, the prompt and the answer", got.Recent)
	}
}

func TestAClaudeCodeTranscriptInAnotherFolderIsFoundByItsID(t *testing.T) {
	home := t.TempDir()
	storage := filepath.Join(home, "claude-config")
	// A shortened folder name, as for a long working directory.
	path := filepath.Join(storage, "projects", "-home-u-a-very-long-na-1a2b3c", "conv-1.jsonl")
	write(t, path, claudeLines...)
	// A sub-agent's transcript of the same session is never taken for it.
	write(t, filepath.Join(storage, "projects", "-home-u-a-very-long-na-1a2b3c", "conv-1", "subagents", "agent-a1.jsonl"), claudeLines...)

	found, err := Reader{Home: home}.Find(ClaudeCode, storage, "/home/u/a/very/long/name", "conv-1")
	if err != nil || found != path {
		t.Fatalf("find = %q, %v; want %q", found, err, path)
	}
}

func TestAMissingTranscriptIsNotFound(t *testing.T) {
	r := Reader{Home: t.TempDir()}
	for _, kind := range []string{ClaudeCode, Codex} {
		if _, err := r.Find(kind, "", "/repo", "gone"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: find = %v, want ErrNotFound", kind, err)
		}
	}
	if _, err := r.Find(ClaudeCode, "", "/repo", "../escape"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an ID with a path in it = %v, want ErrNotFound", err)
	}
	if _, err := r.Find("chimera", "", "/repo", "s-1"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("chimera = %v, want ErrUnsupported", err)
	}
	if _, err := Read(ClaudeCode, filepath.Join(t.TempDir(), "gone.jsonl"), 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("read of a gone file = %v, want ErrNotFound", err)
	}
}

var codexLines = []string{
	`{"timestamp":"2026-11-21T09:00:00.000Z","type":"session_meta","payload":{"id":"thread-9","cwd":"/repo","timestamp":"2026-11-21T09:00:00.000Z"}}`,
	`{"timestamp":"2026-11-21T09:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>cwd</environment_context>"}]}}`,
	`{"timestamp":"2026-11-21T09:00:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add a --json flag"}]}}`,
	`{"timestamp":"2026-11-21T09:00:03.000Z","type":"response_item","payload":{"type":"function_call","name":"shell"}}`,
	`{"timestamp":"2026-11-21T09:03:00.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The flag is in."}]}}`,
}

func TestACodexRolloutIsFoundByItsThreadAndRead(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "sessions", "2026", "11", "21", "rollout-2026-11-21T09-00-00-thread-9.jsonl")
	write(t, path, codexLines...)
	r := Reader{Home: home}
	found, err := r.Find(Codex, "", "/repo", "thread-9")
	if err != nil || found != path {
		t.Fatalf("find = %q, %v", found, err)
	}
	got, err := Read(Codex, found, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "thread-9" || got.CWD != "/repo" || strings.Join(got.FirstUser, "|") != "add a --json flag" || got.LastAssistant != "The flag is in." || got.Recent != nil {
		t.Fatalf("rollout = %#v", got)
	}
}

func TestScanListsTheTranscriptsAFolderStartedInATime(t *testing.T) {
	home := t.TempDir()
	at := func(id, ts string) string {
		return `{"type":"user","sessionId":"` + id + `","cwd":"/repo","timestamp":"` + ts + `","message":{"role":"user","content":"go"}}`
	}
	folder := filepath.Join(home, ".claude", "projects", "-repo")
	write(t, filepath.Join(folder, "before.jsonl"), at("before", "2026-11-21T08:00:00Z"))
	write(t, filepath.Join(folder, "during-2.jsonl"), at("during-2", "2026-11-21T10:30:00Z"))
	write(t, filepath.Join(folder, "during-1.jsonl"), at("during-1", "2026-11-21T10:00:00Z"))
	write(t, filepath.Join(folder, "during-1", "subagents", "agent-x.jsonl"), at("during-1", "2026-11-21T10:10:00Z"))
	from, to := time.Date(2026, 11, 21, 9, 59, 0, 0, time.UTC), time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC)

	got, err := Reader{Home: home}.Scan(ClaudeCode, "", "/repo", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "during-1" || got[1].SessionID != "during-2" {
		t.Fatalf("scan = %#v, want the two that started during the run", got)
	}

	codex := filepath.Join(home, ".codex", "sessions", "2026", "11", "21")
	write(t, filepath.Join(codex, "rollout-x-thread-9.jsonl"), strings.Replace(codexLines[0], "09:00:00.000Z\"}}", "10:15:00.000Z\"}}", 1))
	if got, err := (Reader{Home: home}).Scan(Codex, "", "/repo", from, to); err != nil || len(got) != 1 || got[0].SessionID != "thread-9" {
		t.Fatalf("codex scan = %#v, %v", got, err)
	}
}

// filler is a file of size bytes whose middle is padding, counting what is
// read of it.
type filler struct {
	head, tail []byte
	size       int64
	read       atomic.Int64
}

func (f *filler) ReadAt(p []byte, off int64) (int, error) {
	f.read.Add(int64(len(p)))
	for i := range p {
		at := off + int64(i)
		switch {
		case at >= f.size:
			return i, io.EOF
		case at < int64(len(f.head)):
			p[i] = f.head[at]
		case at >= f.size-int64(len(f.tail)):
			p[i] = f.tail[at-(f.size-int64(len(f.tail)))]
		case at%200 == 199:
			p[i] = '\n'
		default:
			p[i] = 'x'
		}
	}
	return len(p), nil
}

func TestAHugeTranscriptIsReadOnlyAtItsEnds(t *testing.T) {
	f := &filler{
		head: []byte(strings.Join(claudeLines[:2], "\n") + "\n"),
		tail: []byte("\n" + claudeLines[len(claudeLines)-1] + "\n"),
		size: 50 << 20,
	}
	got, err := read(ClaudeCode, f, f.size, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FirstUser) != 1 || got.LastAssistant != "Fixed: it waited on a timer." {
		t.Fatalf("read = %#v", got)
	}
	if n := f.read.Load(); n > headBytes+tailBytes {
		t.Fatalf("read %d bytes of a 50 MB file, want at most %d", n, headBytes+tailBytes)
	}
}
