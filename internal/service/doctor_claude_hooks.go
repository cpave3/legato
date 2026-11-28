package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// How far back the claude-code.hooks check looks, and how much it reads:
// the newest transcripts only, and the end of each.
const (
	claudeHooksWindow     = 7 * 24 * time.Hour
	claudeHooksTranscript = 20
	claudeHooksTailBytes  = 1 << 20
)

// claudeHookSkipped is the notice Claude Code writes into a transcript when
// a mod ahead of a plugin passes one of its hooks over, for one of Legato's
// plugins: the plugin, the event and the mod that passed it over.
var claudeHookSkipped = regexp.MustCompile(`^(legato|legato-state|legato-tools|legato-manticore): its (\S+) hook was skipped\. "([^"]+)" runs first`)

// checkClaudeHooks reads the newest Claude Code transcripts under dirs (each
// a CLAUDE_CONFIG_DIR) for notices that Claude Code skipped one of Legato's
// hooks, as its bundled security mod did to the classic ones (LEG-684). Only
// notices recorded since the mod under modDir was written count: the ones an
// older Legato left behind say nothing of this one.
//
// It is never required: a skipped hook costs the native panel a feature, not
// the agent its work.
func checkClaudeHooks(dirs []string, modDir string, now time.Time) DoctorCheck {
	c := DoctorCheck{ID: "claude-code.hooks", Title: "Claude Code runs Legato's hooks"}
	since := now.Add(-claudeHooksWindow)
	if installed := newestModTime(modDir); installed.After(since) {
		since = installed
	}

	type transcript struct {
		path     string
		modified time.Time
	}
	var transcripts []transcript
	for _, dir := range dirs {
		paths, _ := filepath.Glob(filepath.Join(dir, "projects", "*", "*.jsonl"))
		for _, path := range paths {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && !info.ModTime().Before(since) {
				transcripts = append(transcripts, transcript{path, info.ModTime()})
			}
		}
	}
	if len(transcripts) == 0 {
		c.Result = DoctorSkipped
		c.Detail = "no Claude Code session since Legato's mod was installed, or in the last week"
		return c
	}
	sort.Slice(transcripts, func(i, j int) bool { return transcripts[i].modified.After(transcripts[j].modified) })
	if len(transcripts) > claudeHooksTranscript {
		transcripts = transcripts[:claudeHooksTranscript]
	}

	skipped := map[string]bool{}
	for _, t := range transcripts {
		for _, notice := range skippedHookNotices(t.path, since) {
			skipped[notice] = true
		}
	}
	if len(skipped) == 0 {
		c.Result = DoctorOK
		return c
	}
	notices := make([]string, 0, len(skipped))
	for notice := range skipped {
		notices = append(notices, notice)
	}
	sort.Strings(notices)
	c.Result = DoctorFailed
	c.Detail = "Claude Code skipped Legato's hooks: " + strings.Join(notices, "; ")
	c.Fix = "Update Legato. Until then the native panel may miss what these hooks report, such as questions and permission requests."
	return c
}

// skippedHookNotices returns each of Legato's hooks the transcript at path
// says was skipped since then, as "<plugin>: <event> (<mod> runs first)".
func skippedHookNotices(path string, since time.Time) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > claudeHooksTailBytes {
		if _, err := f.Seek(info.Size()-claudeHooksTailBytes, io.SeekStart); err != nil {
			return nil
		}
	}
	var notices []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), claudeHooksTailBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), "hook was skipped") {
			continue
		}
		var row struct {
			Type      string    `json:"type"`
			Subtype   string    `json:"subtype"`
			Content   string    `json:"content"`
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(line, &row) != nil || row.Type != "system" || row.Subtype != "informational" || row.Timestamp.Before(since) {
			continue
		}
		if match := claudeHookSkipped.FindStringSubmatch(row.Content); match != nil {
			notices = append(notices, fmt.Sprintf("%s: %s (%s runs first)", match[1], match[2], match[3]))
		}
	}
	return notices
}

// newestModTime is when the newest file under the installed mods was
// written. Legato rewrites a file there only when its content changed.
func newestModTime(modDir string) time.Time {
	var newest time.Time
	if modDir == "" {
		return newest
	}
	_ = filepath.WalkDir(modDir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
