package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// ReeveEngram is the Reeve's own engram: its name (reeve-<the first 8 of
// the instance ID>), its store (ENGRAM_HOME, inside the Reeve's folder,
// never the person's) and the seed it is issued with when the store does
// not have it yet. The spawn's engram steps boot it (docs/ai/reeve.md).
type ReeveEngram struct {
	Name string
	Home string
	Seed string
}

// reeveNoEngram stands in for the boot packet when the engram did not
// boot, or the Reeve has none.
func reeveNoEngram(reason string) string {
	return "## Your engram\n\nYou start this session without your engram: it could not boot (" + reason + "). " +
		"You have no memories of earlier sessions. Say so in your first reply. Write nothing to an engram this session."
}

// ReeveRolePrompt is Legato's built-in role prompt for the Reeve, for this
// version.
const ReeveRolePrompt = `## You are the Reeve

You are the Reeve: the agent of this Legato instance itself. The operator talks to you to run and configure this instance — its settings, its restarts, its workers and integrations, its logs, and, when needed, the machine it runs on. You do not work on tasks in repositories; other agents do that.

### Your tools

- docs: list, search and read Legato's user manual. Read it before you explain or change something you are not sure of.
- instance_status: a fresh snapshot of this instance (the same one you started with).
- read_logs: the recent lines of this instance's log file, with a filter. Look here first when something failed.
- legato: run a legato subcommand on the host, for what only the command line does.
- host_shell, edit_file, manage_service (while host operations are on): a command, a file or a systemd service on this machine. A last resort.
- request_secret: ask the operator to paste a secret into a form; you get its name.
- restart_legato, propose_config_edit: a restart, and an edit to config.yaml.
- legato_api: call any route of Legato's API with your own token. Reads return at once; a change you make with it becomes a change set of one.
  Every route, with its method, body and class, is on the manual page reference/api (docs read reference/api): look a route up there rather than guess it.
- propose_changes: put changes to the operator as one change set: a one-line reason and the calls, in order.

### Changes and approvals

You read freely. You never change anything by yourself: every change goes to the operator as a change set, one approval for the calls of one step, at the level of its strictest call (a tap; or a signature on their phone for a risky one). Legato carries out exactly what was approved, in order, a restart last, stopping at the first failure, and a [legato] message tells you each call's result. propose_changes waits a minute for the decision, then answers pending: carry on, or end your turn. Group the calls of one step in one set; propose the next step once you know how the last went.

The operator may let some sets run at once, with no approval asked (the auto level: for changes, for some rules, or for host operations not as root). Such a set may run at once: propose_changes answers approved and the results follow as a [legato] message. Work exactly as before: one step at a time, and read its result before you propose the next. Never bundle steps into one set because it would run unattended, and never split a call off to make it run unattended. Some calls always wait for the operator whatever is set: deletions, the Reeve's own rules, security settings, pairing, secrets and config.yaml edits, and anything as root.

Some things you may never do, whatever is approved: decide any approval or answer another agent's permission prompt (reading them is fine); enroll, remove or reset approval devices; regenerate the web token; read or open a pairing request (its phrase); type into another agent's terminal or run ! commands in its session. Message agents the normal way.

### How you work

- Use the host shell only sparingly, as a last resort, when Legato's own tools cannot do the job.
- Secrets reach you only by name. Never ask the operator to paste a secret into the conversation, and never repeat one.
- When the operator denies a change, do not propose the same change again in this session unless they ask; you may propose something different.
- Your engram keeps what you learn from one session to the next, with the engram command. What is worth remembering: the operator's preferences, why settings are as they are, past problems and their fixes, open threads. Secrets never are.`

// ReeveSnapshotSource is what the snapshot of an instance is read from.
type ReeveSnapshotSource struct {
	Store   *store.Store
	Version string
	// Runs says how this Legato runs: the TUI, serve, under systemd.
	Runs string
	// LogPath is the log file whose recent errors are listed.
	LogPath string
	// UpdateMode is the update mode setting, "" for ask.
	UpdateMode string
	// Reeve is the Reeve's settings: host operations and levels.
	Reeve *config.ReeveConfig
	Now   func() time.Time
}

// ReeveSnapshot is a short snapshot of the instance, about a screen.
func ReeveSnapshot(ctx context.Context, src ReeveSnapshotSource) string {
	if src.Now == nil {
		src.Now = time.Now
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## This instance, as of %s\n\n", src.Now().UTC().Format("2006-01-02 15:04 UTC"))
	mode := src.UpdateMode
	if mode == "" {
		mode = "ask"
	}
	fmt.Fprintf(&b, "- Version: %s (updates: %s)\n", src.Version, mode)
	if src.Runs != "" {
		fmt.Fprintf(&b, "- Runs as: %s\n", src.Runs)
	}
	if src.Store != nil {
		if id, err := InstanceID(ctx, src.Store); err == nil {
			fmt.Fprintf(&b, "- Instance ID: %s\n", id)
		}
		if workers, err := src.Store.ListWorkers(ctx); err == nil && len(workers) > 0 {
			fmt.Fprintf(&b, "- Main of %d worker(s):", len(workers))
			for _, w := range workers {
				state := "reachable"
				if w.UnreachableSince != nil {
					state = "unreachable since " + *w.UnreachableSince
				}
				fmt.Fprintf(&b, " %s (%s);", w.Name, state)
			}
			b.WriteString("\n")
		}
		if mains, err := src.Store.ListMainIntegrationTokens(ctx); err == nil && len(mains) > 0 {
			fmt.Fprintf(&b, "- Worker of %d main instance(s)\n", len(mains))
		}
		if items, err := src.Store.ListAttentionItems(ctx, "open", nil); err == nil {
			fmt.Fprintf(&b, "- Open Attention items: %d\n", len(items))
			for i, item := range items {
				if i == 8 {
					fmt.Fprintf(&b, "  - … and %d more\n", len(items)-8)
					break
				}
				fmt.Fprintf(&b, "  - %s (%s)\n", item.Title, item.SignalKind)
			}
		}
		if sessions, err := src.Store.ListAgentSessions(ctx); err == nil {
			running := 0
			for _, s := range sessions {
				if s.Status == "running" {
					running++
				}
			}
			fmt.Fprintf(&b, "- Running agents: %d\n", running)
		}
	}
	if src.LogPath != "" {
		if lines, err := ReadLogLines(src.LogPath, "(?i)error|fail|panic", 5); err == nil && len(lines) > 0 {
			b.WriteString("- Recent errors in the log:\n")
			for _, line := range lines {
				fmt.Fprintf(&b, "  - %s\n", clip(line, 200))
			}
		}
	}
	if src.Reeve != nil {
		if v := src.Reeve.Voice; v.Enabled {
			now := time.Now()
			if src.Now != nil {
				now = src.Now()
			}
			switch {
			case v.QuietAt(now):
				fmt.Fprintf(&b, "- Voice: on, but quiet hours now (%s): reply in writing, except to a [legato voice] message\n", v.QuietHours)
			case v.QuietHours != "":
				fmt.Fprintf(&b, "- Voice: on (quiet hours %s)\n", v.QuietHours)
			default:
				b.WriteString("- Voice: on\n")
			}
		}
		switch {
		case !src.Reeve.HostOperationsOn():
			b.WriteString("- Host operations: off\n")
		case src.Reeve.HostOperationsAutoOn():
			b.WriteString("- Host operations: on, run without asking (not as root) (host_shell, edit_file, manage_service)\n")
		default:
			b.WriteString("- Host operations: on (host_shell, edit_file, manage_service)\n")
		}
		change, risky := src.Reeve.Levels.Change, src.Reeve.Levels.Risky
		if change == "" {
			change = "approve"
		}
		if risky == "" || risky == "default" {
			risky = "default (signed once a device is enrolled, else approve)"
		}
		fmt.Fprintf(&b, "- Approval levels: change %s, risky %s, %d rule(s); a host call as root always signed and verified\n", change, risky, len(src.Reeve.Rules))
		var auto []string
		if change == string(LevelAuto) {
			auto = append(auto, "changes")
		}
		if risky == string(LevelAuto) {
			auto = append(auto, "risky changes")
		}
		if src.Reeve.HostOperationsAutoOn() {
			auto = append(auto, "host operations not as root")
		}
		for _, r := range src.Reeve.Rules {
			if ReeveLevel(strings.TrimSpace(r.Level)) == LevelAuto {
				auto = append(auto, r.Pattern)
			}
		}
		if len(auto) > 0 {
			fmt.Fprintf(&b, "- Run without asking: %s (deletions, your own rules, security, secrets, config.yaml edits and anything as root still wait)\n", strings.Join(auto, ", "))
		}
	}
	return b.String()
}

// ReeveVoicePrompt is the Voice section of the Reeve's prompt, given while
// reeve.voice.enabled: what a spoken message asks of it. Voice mode lasts
// one message, so the Reeve keeps no state for it.
const ReeveVoicePrompt = `## Voice

A message that starts with ` + "`" + VoiceMarker + "`" + ` was spoken, and the person is listening, probably not looking at the screen. Answer it with ` + "`speak`" + `: one call for each reply, the outcome first, at most three short sentences. Keep the written reply to what cannot be said aloud (a path, a link, code); write nothing more. When the work will take more than a few seconds, first say in one short line what you are doing. A message without the marker was typed: answer in writing, and speak only if asked.`

// ReeveLaunchPrompt is a Reeve session's launch prompt after its engram's
// part: the role prompt, the snapshot and the brief. noEngram, when not
// empty, says why the session has no engram and comes first; otherwise the
// spawn puts the boot packet (or the line that stands in for it) first.
func ReeveLaunchPrompt(noEngram, snapshot, brief string, voice bool) string {
	var sections []string
	if noEngram != "" {
		sections = append(sections, reeveNoEngram(noEngram))
	}
	sections = append(sections, ReeveRolePrompt)
	if voice {
		sections = append(sections, ReeveVoicePrompt)
	}
	sections = append(sections, snapshot)
	if strings.TrimSpace(brief) != "" {
		sections = append(sections, "## The brief\n\n"+strings.TrimSpace(brief))
	}
	return strings.Join(sections, "\n\n")
}

// ReeveResumeMessage is the first message a resumed session is sent: a
// fresh snapshot.
func ReeveResumeMessage(snapshot string) string {
	return "[legato] since you were last here:\n\n" + snapshot
}

// ReadLogLines returns the last n lines of the log file at path that match
// filter (a regular expression; "" matches every line), oldest first.
func ReadLogLines(path, filter string, n int) ([]string, error) {
	var re *regexp.Regexp
	if filter != "" {
		var err error
		if re, err = regexp.Compile(filter); err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
	}
	if n <= 0 {
		n = 100
	}
	var lines []string
	for _, p := range []string{path + ".1", path} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if re != nil && !re.MatchString(line) {
				continue
			}
			lines = append(lines, line)
			if len(lines) > n {
				lines = lines[1:]
			}
		}
		f.Close()
	}
	return lines, nil
}
