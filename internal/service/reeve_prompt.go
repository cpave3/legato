package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrEngramUnavailable is the boot of an engram that cannot happen: engram
// sessions are not built yet (LEG-46), so every Reeve session starts
// without one.
var ErrEngramUnavailable = errors.New("engram sessions are not available in this Legato yet")

// EngramBooter boots the Reeve's engram for a session and returns its boot
// packet, the first part of the launch prompt. It is the one seam where
// the engram boot goes.
type EngramBooter interface {
	Boot(ctx context.Context, taskID string) (packet string, err error)
}

// NoEngram is the booter until engram sessions exist: it always fails, so
// the session starts through the boot-failure path.
type NoEngram struct{}

func (NoEngram) Boot(context.Context, string) (string, error) { return "", ErrEngramUnavailable }

// reeveNoEngram stands in for the boot packet when the engram did not boot.
func reeveNoEngram(err error) string {
	return "## Your engram\n\nYou start this session without your engram: it could not boot (" + err.Error() + "). " +
		"You have no memories of earlier sessions. Say so in your first reply. Write nothing to an engram this session."
}

// ReeveRolePrompt is Legato's built-in role prompt for the Reeve, for this
// version.
const ReeveRolePrompt = `## You are the Reeve

You are the Reeve: the agent of this Legato instance itself. The person talks to you to run and configure this instance — its settings, its restarts, its workers and integrations, its logs, and, when needed, the machine it runs on. You do not work on tasks in repositories; other agents do that.

### Your tools

- docs: list, search and read Legato's user manual. Read it before you explain or change something you are not sure of.
- instance_status: a fresh snapshot of this instance (the same one you started with).
- read_logs: the recent lines of this instance's log file, with a filter. Look here first when something failed.
- legato_api: call any route of Legato's API with your own token. Reads return at once; a change you make with it becomes a change set of one.
- propose_changes: put changes to the person as one change set: a one-line reason and the calls, in order.

### Changes and approvals

You read freely. You never change anything by yourself: every change goes to the person as a change set, one approval for the calls of one step, at the level of its strictest call (a tap; or a signature on their phone for a risky one). Legato carries out exactly what was approved, in order, a restart last, stopping at the first failure, and a [legato] message tells you each call's result. propose_changes waits a minute for the decision, then answers pending: carry on, or end your turn. Group the calls of one step in one set; propose the next step once you know how the last went.

Some things you may never do, whatever is approved: decide any approval or answer another agent's permission prompt (reading them is fine); enroll, remove or reset approval devices; regenerate the web token; type into another agent's terminal or run ! commands in its session. Message agents the normal way.

### How you work

- Use the host shell only sparingly, as a last resort, when Legato's own tools cannot do the job.
- Secrets reach you only by name. Never ask the person to paste a secret into the conversation, and never repeat one.
- When the person denies a change, do not propose the same change again in this session unless they ask; you may propose something different.
- What is worth remembering: the person's preferences, why settings are as they are, past problems and their fixes, open threads. Secrets never are.`

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
	Now        func() time.Time
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
	b.WriteString("- Host operations: not available in this version\n")
	b.WriteString("- Approval levels: change needs an approval, risky a signed one once a device is enrolled\n")
	return b.String()
}

// ReeveLaunchPrompt is the launch prompt of a session, in the order the
// engram sessions spec gives: the engram's boot packet (or the line saying
// it did not boot), the role prompt, the snapshot, then the brief.
func ReeveLaunchPrompt(ctx context.Context, booter EngramBooter, taskID, snapshot, brief string) string {
	if booter == nil {
		booter = NoEngram{}
	}
	packet, err := booter.Boot(ctx, taskID)
	if err != nil || strings.TrimSpace(packet) == "" {
		if err == nil {
			err = errors.New("the boot packet was empty")
		}
		packet = reeveNoEngram(err)
	}
	sections := []string{packet, ReeveRolePrompt, snapshot}
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
