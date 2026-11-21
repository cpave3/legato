// Package transcript finds and reads the transcript an agent tool keeps of
// a conversation, read only: it never writes into a tool's store. It reads
// a bounded head and tail of a file, never all of a multi-megabyte one.
//
// Where the transcripts are, as found on 2026-11-21:
//
//   - Claude Code (2.1.322): <storage>/projects/<folder>/<session id>.jsonl,
//     where <storage> is CLAUDE_CONFIG_DIR or ~/.claude and <folder> is the
//     conversation's working directory with every character that is not a
//     letter or a digit replaced by "-" (/home/u/.local/x is
//     -home-u--local-x). Folder names up to 108 characters were seen
//     unshortened; a longer one may be shortened, so a missing path falls
//     back to projects/*/<session id>.jsonl. Each line is a JSON object; the
//     conversation's are "type": "user" and "assistant", with "message"
//     {role, content} (a string, or parts of type text, tool_use,
//     tool_result, thinking), "timestamp", "cwd", "sessionId" and
//     "isSidechain". A sub-agent's transcript is
//     <folder>/<session id>/subagents/agent-*.jsonl, which the fallback
//     never matches. A sandboxed agent shares the host's ~/.claude at the
//     same path, so its transcript is read the same way.
//   - Codex: <storage>/sessions/YYYY/MM/DD/rollout-*-<thread id>.jsonl, where
//     <storage> is CODEX_HOME or ~/.codex. Its first line is
//     {"type": "session_meta", "payload": {id, cwd, timestamp}}; messages are
//     {"type": "response_item", "payload": {"type": "message", role,
//     content: [{type: input_text | output_text, text}]}}. This layout is
//     Codex's documented one; no Codex was installed to check it against.
//   - Chimera keeps its sessions in a store of its own, which nothing here
//     reads: ErrUnsupported.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// The agent tools whose transcripts can be read.
const (
	ClaudeCode = "claude-code"
	Codex      = "codex"
)

// ErrNotFound is a transcript that is not where its tool keeps it.
var ErrNotFound = errors.New("transcript not found")

// ErrUnsupported is a tool whose transcripts are not read.
var ErrUnsupported = errors.New("transcripts of this agent tool are not read")

// How much of a file is read: its head, for how it started, and its tail,
// for how it stands.
var (
	headBytes int64 = 256 << 10
	tailBytes int64 = 512 << 10
)

// firstUsers is how many user messages of the head are kept.
const firstUsers = 5

// Message is one thing the operator or the agent said.
type Message struct {
	Role string    `json:"role"` // user or assistant
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Transcript is what a transcript says of its conversation.
type Transcript struct {
	Path      string
	SessionID string
	CWD       string
	StartedAt time.Time
	EndedAt   time.Time
	// FirstUser are the first user messages, up to five, oldest first.
	FirstUser []string
	// LastAssistant is the newest assistant text.
	LastAssistant string
	// Recent are the newest messages, oldest first, as many as asked.
	Recent []Message
}

// Reader reads transcripts under a home directory.
type Reader struct {
	// Home is the home directory a storage folder defaults under; the
	// user's when empty.
	Home string
}

func (r Reader) storage(kind, storageDir string) (string, error) {
	if storageDir != "" {
		return storageDir, nil
	}
	home := r.Home
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	}
	switch kind {
	case ClaudeCode:
		return filepath.Join(home, ".claude"), nil
	case Codex:
		return filepath.Join(home, ".codex"), nil
	}
	return "", ErrUnsupported
}

// ClaudeProjectFolder is the name Claude Code gives a working directory's
// folder under projects/.
func ClaudeProjectFolder(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return '-'
	}, cwd)
}

// Find returns the path of a conversation's transcript: ErrNotFound when
// it is not there, ErrUnsupported for a tool whose transcripts are not read.
func (r Reader) Find(kind, storageDir, cwd, sessionID string) (string, error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return "", ErrNotFound
	}
	root, err := r.storage(kind, storageDir)
	if err != nil {
		return "", err
	}
	var candidates []string
	switch kind {
	case ClaudeCode:
		if cwd != "" {
			exact := filepath.Join(root, "projects", ClaudeProjectFolder(cwd), sessionID+".jsonl")
			if isFile(exact) {
				return exact, nil
			}
		}
		candidates, _ = filepath.Glob(filepath.Join(root, "projects", "*", sessionID+".jsonl"))
	case Codex:
		candidates, _ = filepath.Glob(filepath.Join(root, "sessions", "*", "*", "*", "rollout-*-"+sessionID+".jsonl"))
	default:
		return "", ErrUnsupported
	}
	for _, path := range candidates {
		if isFile(path) {
			return path, nil
		}
	}
	return "", ErrNotFound
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Read reads a transcript's head and tail, keeping the newest recent
// messages.
func Read(kind, path string, recent int) (*Transcript, error) {
	if kind != ClaudeCode && kind != Codex {
		return nil, ErrUnsupported
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	t, err := read(kind, f, info.Size(), recent)
	if err != nil {
		return nil, err
	}
	t.Path = path
	return t, nil
}

// read reads the head and tail of a file of size bytes.
func read(kind string, f io.ReaderAt, size int64, recent int) (*Transcript, error) {
	t := &Transcript{}
	var headLines, tailLines []entry
	if size <= headBytes+tailBytes {
		// Small enough to read whole: its head and tail are all of it.
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		headLines = lines(buf, false, false)
		tailLines = headLines
	} else {
		head := make([]byte, headBytes)
		if _, err := f.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		tail := make([]byte, tailBytes)
		if _, err := f.ReadAt(tail, size-tailBytes); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		headLines = lines(head, true, false)
		tailLines = lines(tail, false, true)
	}
	for _, line := range headLines {
		e := parse(kind, line)
		if e.sessionID != "" && t.SessionID == "" {
			t.SessionID = e.sessionID
		}
		if e.cwd != "" && t.CWD == "" {
			t.CWD = e.cwd
		}
		if !e.at.IsZero() && t.StartedAt.IsZero() {
			t.StartedAt = e.at
		}
		if e.role == "user" && e.text != "" && len(t.FirstUser) < firstUsers {
			t.FirstUser = append(t.FirstUser, e.text)
		}
	}
	var messages []Message
	for _, line := range tailLines {
		e := parse(kind, line)
		if !e.at.IsZero() {
			t.EndedAt = e.at
		}
		if e.text == "" {
			continue
		}
		if e.role == "assistant" {
			t.LastAssistant = e.text
		}
		messages = append(messages, Message{Role: e.role, Text: e.text, At: e.at})
	}
	if recent > 0 && len(messages) > recent {
		messages = messages[len(messages)-recent:]
	}
	if recent > 0 {
		t.Recent = messages
	}
	return t, nil
}

type entry []byte

// lines splits buf into whole lines: the last is dropped when the buffer
// was cut (cutEnd), the first when it starts inside a line (cutStart).
func lines(buf []byte, cutEnd, cutStart bool) []entry {
	parts := bytes.Split(buf, []byte("\n"))
	if cutStart && len(parts) > 0 {
		parts = parts[1:]
	}
	if cutEnd && len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}
	out := make([]entry, 0, len(parts))
	for _, p := range parts {
		if p = bytes.TrimSpace(p); len(p) > 0 {
			out = append(out, entry(p))
		}
	}
	return out
}

type parsed struct {
	role, text, cwd, sessionID string
	at                         time.Time
}

// parse reads one line; a malformed one says nothing.
func parse(kind string, line entry) parsed {
	switch kind {
	case ClaudeCode:
		return parseClaude(line)
	case Codex:
		return parseCodex(line)
	}
	return parsed{}
}

func parseClaude(line entry) parsed {
	var o struct {
		Type        string `json:"type"`
		Timestamp   string `json:"timestamp"`
		CWD         string `json:"cwd"`
		SessionID   string `json:"sessionId"`
		IsSidechain bool   `json:"isSidechain"`
		IsMeta      bool   `json:"isMeta"`
		Message     struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &o) != nil {
		return parsed{}
	}
	p := parsed{cwd: o.CWD, sessionID: o.SessionID}
	if at, err := time.Parse(time.RFC3339Nano, o.Timestamp); err == nil {
		p.at = at
	}
	if (o.Type != "user" && o.Type != "assistant") || o.IsSidechain || o.IsMeta {
		return p
	}
	p.role = o.Type
	p.text = contentText(o.Message.Content, "text")
	if p.role == "user" && isToolNoise(p.text) {
		p.text = ""
	}
	return p
}

func parseCodex(line entry) parsed {
	var o struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &o) != nil {
		return parsed{}
	}
	var p parsed
	if at, err := time.Parse(time.RFC3339Nano, o.Timestamp); err == nil {
		p.at = at
	}
	switch o.Type {
	case "session_meta":
		var meta struct {
			ID        string `json:"id"`
			CWD       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(o.Payload, &meta) == nil {
			p.sessionID, p.cwd = meta.ID, meta.CWD
			if at, err := time.Parse(time.RFC3339Nano, meta.Timestamp); err == nil {
				p.at = at
			}
		}
	case "response_item":
		var item struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(o.Payload, &item) == nil && item.Type == "message" && (item.Role == "user" || item.Role == "assistant") {
			p.role = item.Role
			p.text = contentText(item.Content, "input_text", "output_text", "text")
			// Codex puts its environment and instructions in user turns.
			if p.role == "user" && strings.HasPrefix(p.text, "<") {
				p.text = ""
			}
		}
	}
	return p
}

// contentText is the text of a message's content: a string, or its parts
// of the given types joined.
func contentText(raw json.RawMessage, types ...string) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		for _, want := range types {
			if part.Type == want && part.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(part.Text)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// isToolNoise is a user line Claude Code writes that the operator did not:
// a slash command's echo or its output.
func isToolNoise(text string) bool {
	return strings.HasPrefix(text, "<command-") || strings.HasPrefix(text, "<local-command-") || strings.HasPrefix(text, "Caveat: ")
}

// Scan lists the transcripts of a tool whose working directory is cwd and
// whose first timestamp falls in [from, to], oldest first: the
// conversations a run in that folder may have had. Sub-agents' are never
// listed.
func (r Reader) Scan(kind, storageDir, cwd string, from, to time.Time) ([]Transcript, error) {
	root, err := r.storage(kind, storageDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	switch kind {
	case ClaudeCode:
		paths, _ = filepath.Glob(filepath.Join(root, "projects", ClaudeProjectFolder(cwd), "*.jsonl"))
	case Codex:
		for day := from.UTC().Truncate(24 * time.Hour); !day.After(to.UTC()); day = day.Add(24 * time.Hour) {
			found, _ := filepath.Glob(filepath.Join(root, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
			paths = append(paths, found...)
		}
	default:
		return nil, ErrUnsupported
	}
	var out []Transcript
	for _, path := range paths {
		t, err := readHead(kind, path)
		if err != nil || t.CWD != cwd || t.StartedAt.IsZero() || t.StartedAt.Before(from) || t.StartedAt.After(to) || t.SessionID == "" {
			continue
		}
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// readHead reads only as far as a transcript says where and when it began.
func readHead(kind, path string) (*Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t := &Transcript{Path: path}
	scanner := bufio.NewScanner(io.LimitReader(f, headBytes))
	scanner.Buffer(make([]byte, 64<<10), int(headBytes))
	for scanner.Scan() {
		e := parse(kind, scanner.Bytes())
		if e.sessionID != "" && t.SessionID == "" {
			t.SessionID = e.sessionID
		}
		if e.cwd != "" && t.CWD == "" {
			t.CWD = e.cwd
		}
		if !e.at.IsZero() && t.StartedAt.IsZero() {
			t.StartedAt = e.at
		}
		if e.role == "user" && e.text != "" && len(t.FirstUser) < firstUsers {
			t.FirstUser = append(t.FirstUser, e.text)
		}
		if t.SessionID != "" && t.CWD != "" && !t.StartedAt.IsZero() && len(t.FirstUser) > 0 {
			break
		}
	}
	return t, nil
}
