package claudebridge

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

// Background shells: a Bash call Claude Code runs in the background (asked
// for with run_in_background, or moved there when it outlived its timeout)
// goes on after its call returns, writing its output to a file Claude Code
// names in the call's result. The bridge records each one, follows its file
// and serves both to the panel: in the snapshot (backgroundShells) and live
// (background_shell when one starts or ends, background_shell_output as it
// prints).
//
// The bridge is Claude Code's child, so inside a sandbox it sees the same
// /tmp as the session: it reads the file where Claude Code wrote it.
//
// A shell ends when Claude Code tells the session so, with a
// <task-notification> prompt that gives its status and exit code, or when
// the session stops it with TaskStop (KillShell before 2.1). Never from its
// output: Claude Code ends the file with "[exited with code N]", but the
// command can print that line too. Claude Code
// delivers a notification between the session's steps, so a shell that ends
// during a long turn is marked when that turn takes it in.

const (
	// shellTailLimit is how much of a shell's newest output is kept, for the
	// snapshot: a dev server prints for hours.
	shellTailLimit = 32 * 1024
	// shellChunkLimit bounds what one output event carries. More printed
	// between two reads is skipped up to its last shellChunkLimit bytes,
	// and the event says so (gap): the panel then starts over from it.
	shellChunkLimit = 8 * 1024
	// shellLimit bounds the shells kept; the oldest ended ones go first.
	shellLimit = 32
	// DefaultShellPoll is how often a running shell's file is read.
	DefaultShellPoll = 500 * time.Millisecond
)

// Shell states as the native protocol says them.
const (
	shellRunning = "running"
	shellExited  = "exited"
	shellKilled  = "killed"
)

// shellRecord is one background shell.
type shellRecord struct {
	id          string
	callID      string
	command     string
	description string
	// path is the output file; empty when the result named none the bridge
	// will read (see ownOutputFile).
	path      string
	status    string
	exitCode  *int
	startedAt int64
	endedAt   int64
	// tail is the newest output, at most shellTailLimit bytes; truncated
	// says older output was dropped.
	tail      []byte
	truncated bool
	// offset is how far the file has been read.
	offset int64
	// seq numbers the shell's output events; the snapshot carries the
	// newest, so a panel skips events its snapshot already holds.
	seq int64
	// watched is set once a watcher follows the file.
	watched bool
}

var (
	backgroundIDPattern   = regexp.MustCompile(`(?:running in background with ID: |moved to the background \(ID: )([A-Za-z0-9_-]+)`)
	backgroundPathPattern = regexp.MustCompile(`Output is being written to: (\S+?)\.?(?:\s|$)`)
	notificationPattern   = regexp.MustCompile(`(?s)<task-notification>(.*?)</task-notification>`)
	notificationField     = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`<` + name + `>\s*([^<]*?)\s*</` + name + `>`)
	}
	notificationTaskID  = notificationField("task-id")
	notificationStatus  = notificationField("status")
	notificationSummary = notificationField("summary")
	exitCodeInSummary   = regexp.MustCompile(`exit code (-?\d+)`)
)

// backgroundShellOf reads a Bash call's result for the background shell it
// started: its ID and output file. ok is false for a call that ran in the
// foreground.
func backgroundShellOf(result string) (id, path string, ok bool) {
	match := backgroundIDPattern.FindStringSubmatch(result)
	if match == nil {
		return "", "", false
	}
	if p := backgroundPathPattern.FindStringSubmatch(result); p != nil {
		path = p[1]
	}
	return match[1], path, true
}

// ownOutputFile is the path when it is the shell's own output file, as
// Claude Code names them (…/tasks/<id>.output), and "" otherwise: the bridge
// reads nothing else on the strength of a tool result.
func ownOutputFile(id, path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	clean := filepath.Clean(path)
	if filepath.Base(clean) != id+".output" || filepath.Base(filepath.Dir(clean)) != "tasks" {
		return ""
	}
	return clean
}

// recordShell starts a record for a background shell a Bash call started.
func (s *session) recordShell(call *toolCall, id, path string, now time.Time) []nativeEvent {
	if s.shells[id] != nil {
		return nil
	}
	shell := &shellRecord{
		id: id, callID: call.callID, command: stringArg(call.args, "command"), description: stringArg(call.args, "description"),
		path: ownOutputFile(id, path), status: shellRunning, startedAt: now.UnixMilli(),
	}
	s.shells[id] = shell
	s.shellOrder = append(s.shellOrder, id)
	s.pruneShells()
	return []nativeEvent{shell.event()}
}

// pruneShells drops the oldest ended shells beyond shellLimit.
func (s *session) pruneShells() {
	for over := len(s.shellOrder) - shellLimit; over > 0; over-- {
		dropped := false
		for index, id := range s.shellOrder {
			if s.shells[id].status != shellRunning {
				delete(s.shells, id)
				s.shellOrder = append(s.shellOrder[:index:index], s.shellOrder[index+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			return
		}
	}
}

// endShell marks a shell ended; a shell already ended keeps how it ended.
func (s *session) endShell(id, status string, exitCode *int, now time.Time) []nativeEvent {
	shell := s.shells[id]
	if shell == nil || shell.status != shellRunning {
		return nil
	}
	shell.status, shell.exitCode, shell.endedAt = status, exitCode, now.UnixMilli()
	return []nativeEvent{shell.event()}
}

// applyShellResult records what a settled call says about background
// shells: one it started, or one it stopped.
func (s *session) applyShellResult(call *toolCall, text string, isError bool, now time.Time) []nativeEvent {
	if isError {
		return nil
	}
	switch call.name {
	case nativeBash:
		if id, path, ok := backgroundShellOf(text); ok {
			return s.recordShell(call, id, path, now)
		}
	case "TaskStop", "KillShell":
		id := stringArg(call.args, "task_id")
		if id == "" {
			id = stringArg(call.args, "shell_id")
		}
		return s.endShell(id, shellKilled, nil, now)
	}
	return nil
}

// applyTaskNotifications ends the shells a <task-notification> prompt
// reports on. Several may come in one prompt.
func (s *session) applyTaskNotifications(text string, now time.Time) []nativeEvent {
	var out []nativeEvent
	for _, match := range notificationPattern.FindAllStringSubmatch(text, -1) {
		body := match[1]
		id := submatch(notificationTaskID, body)
		if s.shells[id] == nil {
			continue
		}
		status, code := shellEnd(submatch(notificationStatus, body), submatch(notificationSummary, body))
		out = append(out, s.endShell(id, status, code, now)...)
	}
	return out
}

func submatch(pattern *regexp.Regexp, text string) string {
	if match := pattern.FindStringSubmatch(text); match != nil {
		return match[1]
	}
	return ""
}

// shellEnd reads how a shell ended from its notification: killed, or exited
// with the code its summary gives (0 for one completed without a code, 1 for
// one failed without one).
func shellEnd(status, summary string) (string, *int) {
	if status == "killed" || status == "stopped" {
		return shellKilled, nil
	}
	code := 0
	if status == "failed" {
		code = 1
	}
	if match := exitCodeInSummary.FindStringSubmatch(summary); match != nil {
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			code = parsed
		}
	}
	return shellExited, &code
}

// shellsToWatch are the shells whose file no watcher follows yet; each is
// marked watched.
func (s *session) shellsToWatch() []string {
	var out []string
	for _, id := range s.shellOrder {
		shell := s.shells[id]
		if !shell.watched && shell.path != "" {
			shell.watched = true
			out = append(out, id)
		}
	}
	return out
}

// appendShellOutput adds what a shell printed since the last read.
func (s *session) appendShellOutput(id string, chunk []byte, gap, reset bool) []nativeEvent {
	shell := s.shells[id]
	if shell == nil || (len(chunk) == 0 && !reset) {
		return nil
	}
	if reset {
		shell.tail, shell.truncated = nil, false
	}
	// Output skipped between two reads leaves nothing to join the new to.
	if gap {
		shell.tail, shell.truncated = nil, true
	}
	shell.tail = append(shell.tail, chunk...)
	if over := len(shell.tail) - shellTailLimit; over > 0 {
		shell.tail = append([]byte(nil), trimToRune(shell.tail[over:])...)
		shell.truncated = true
	}
	shell.seq++
	event := nativeEvent{"type": "background_shell_output", "shellId": id, "chunk": string(chunk), "seq": shell.seq}
	if gap {
		event["gap"] = true
	}
	if reset {
		event["reset"] = true
	}
	return []nativeEvent{event}
}

// trimToRune drops the continuation bytes a cut left at the front.
func trimToRune(b []byte) []byte {
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	return b
}

// event is the background_shell event for a shell's start or end.
func (r *shellRecord) event() nativeEvent {
	return nativeEvent{"type": "background_shell", "shell": r.describe(false)}
}

// snapshotShell is a background shell as the snapshot and its events carry
// it. Output is absent from events, which carry it as chunks.
type snapshotShell struct {
	ShellID     string       `json:"shellId"`
	CallID      string       `json:"callId,omitempty"`
	Command     string       `json:"command"`
	Description string       `json:"description,omitempty"`
	Status      string       `json:"status"`
	ExitCode    *int         `json:"exitCode,omitempty"`
	StartedAt   int64        `json:"startedAt,omitempty"`
	EndedAt     int64        `json:"endedAt,omitempty"`
	Output      *shellOutput `json:"output,omitempty"`
}

// shellOutput is a shell's newest output. Available is false when the
// bridge cannot read it, and the panel then says so.
type shellOutput struct {
	Available bool   `json:"available"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
	// Seq is the newest output event the text holds.
	Seq int64 `json:"seq"`
}

func (r *shellRecord) describe(withOutput bool) snapshotShell {
	shell := snapshotShell{
		ShellID: r.id, CallID: r.callID, Command: r.command, Description: r.description,
		Status: r.status, ExitCode: r.exitCode, StartedAt: r.startedAt, EndedAt: r.endedAt,
	}
	if withOutput {
		shell.Output = &shellOutput{Available: r.path != "", Text: string(r.tail), Truncated: r.truncated, Seq: r.seq}
	} else {
		shell.Output = &shellOutput{Available: r.path != ""}
	}
	return shell
}

func (s *session) snapshotShells() []snapshotShell {
	out := make([]snapshotShell, 0, len(s.shellOrder))
	for _, id := range s.shellOrder {
		out = append(out, s.shells[id].describe(true))
	}
	return out
}

// hydrateShells records the background shells of a resumed transcript, and
// how those that ended ended.
func (s *session) hydrateShells(messages []seedMessage, now time.Time) []nativeEvent {
	var out []nativeEvent
	for _, message := range messages {
		switch message.Role {
		case "assistant":
			for _, use := range message.ToolUses {
				if call := s.tools[use.ToolUseID]; call != nil && use.Text != nil {
					out = append(out, s.applyShellResult(call, *use.Text, use.IsError, now)...)
				}
			}
		case "user":
			out = append(out, s.applyTaskNotifications(message.Text, now)...)
		}
	}
	return out
}

// readShellOutput reads what a file gained past offset: at most
// shellChunkLimit bytes, the newest, with gap set when it skipped some, and
// reset when the file shrank (it was started over). An incomplete UTF-8
// character at the end waits for the next read.
func readShellOutput(path string, offset int64) (chunk []byte, next int64, gap, reset bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, offset, false, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, offset, false, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, offset, false, false, errors.New("not a regular file")
	}
	size := info.Size()
	if size < offset {
		offset, reset = 0, true
	}
	if size == offset {
		return nil, offset, false, reset, nil
	}
	start := offset
	if size-start > shellChunkLimit {
		start, gap = size-shellChunkLimit, true
	}
	buf := make([]byte, size-start)
	n, err := file.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, offset, false, false, err
	}
	buf = buf[:n]
	if gap {
		buf = trimToRune(buf)
	}
	// Hold back a character cut at the end.
	cut := len(buf)
	for back := 1; back <= utf8.UTFMax && back <= len(buf); back++ {
		if utf8.RuneStart(buf[len(buf)-back]) {
			if !utf8.FullRune(buf[len(buf)-back:]) {
				cut = len(buf) - back
			}
			break
		}
	}
	return buf[:cut], start + int64(n) - int64(len(buf)-cut), gap, reset, nil
}

// watchShells starts a watcher for each shell whose file nobody follows yet.
// The caller holds b.mu.
func (b *Bridge) watchShells() {
	for _, id := range b.session.shellsToWatch() {
		go b.watchShell(id)
	}
}

// watchShell follows one shell's output file until the shell has ended and
// its file has been read once more, or the bridge closes.
func (b *Bridge) watchShell(id string) {
	ticker := time.NewTicker(b.shellPoll)
	defer ticker.Stop()
	for {
		select {
		case <-b.closed:
			return
		case <-ticker.C:
		}
		b.mu.Lock()
		shell := b.session.shells[id]
		if shell == nil {
			b.mu.Unlock()
			return
		}
		path, offset, ended := shell.path, shell.offset, shell.status != shellRunning
		b.mu.Unlock()

		chunk, next, gap, reset, err := readShellOutput(path, offset)
		b.mu.Lock()
		if shell := b.session.shells[id]; shell != nil && err == nil {
			shell.offset = next
			b.publish(b.session.appendShellOutput(id, chunk, gap, reset))
		}
		b.mu.Unlock()
		// One read takes the file to its end, so a shell that had ended
		// before it has nothing more to give.
		if ended {
			return
		}
	}
}
