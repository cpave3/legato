package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ChimeraHook is the state-only report carried from the ordered mod stream
// to the host. AgentID on agent.spawn/finish identifies the subject, not the emitter.
type ChimeraHook struct {
	Event       string `json:"event"`
	SessionID   string `json:"session_id,omitempty"`
	Description string `json:"description,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	IsSubagent  bool   `json:"is_subagent,omitempty"`
	Reason      string `json:"reason,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
	// PendingRequests is the number still open AFTER this event. A resolution
	// without a count cannot prove that the last request was answered.
	PendingRequests *int                 `json:"pending_requests,omitempty"`
	Started         *store.BackgroundJob `json:"started,omitempty"`
	// Running, when present, replaces the complete job list; nil preserves it.
	Running []store.BackgroundJob `json:"running"`
	Woke    []string              `json:"woke,omitempty"`
	// Shell is a background shell's job ID and command (background.start
	// and background.exit), and ExitCode how it exited, when it says.
	Shell    *store.BackgroundShell `json:"shell,omitempty"`
	ExitCode *int                   `json:"exit_code,omitempty"`
}

// ChimeraHookReport identifies the execution and slot receiving a mod report.
type ChimeraHookReport struct {
	TaskID      string
	ExecutionID string
	WorkingDir  string
	Hook        ChimeraHook
}

// ParseChimeraHook normalizes a mod event, discarding conversation content.
func ParseChimeraHook(raw []byte) (ChimeraHook, error) {
	var p struct {
		Type            string                `json:"type"`
		SessionID       string                `json:"sessionId"`
		AgentID         string                `json:"agentId"`
		JobID           string                `json:"jobId"`
		Command         string                `json:"command"`
		Description     string                `json:"description"`
		Background      bool                  `json:"background"`
		IsSubagent      bool                  `json:"isSubagent"`
		Reason          string                `json:"reason"`
		RequestID       string                `json:"requestId"`
		PendingRequests *int                  `json:"pendingRequests"`
		ExitCode        *int                  `json:"exitCode"`
		Started         *store.BackgroundJob  `json:"started"`
		Running         []store.BackgroundJob `json:"running"`
		Woke            []string              `json:"woke"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ChimeraHook{}, fmt.Errorf("reading Chimera event: %w", err)
	}
	if p.Type == "" {
		return ChimeraHook{}, fmt.Errorf("Chimera event names no type")
	}
	if p.PendingRequests != nil && *p.PendingRequests < 0 {
		return ChimeraHook{}, fmt.Errorf("Chimera pendingRequests cannot be negative")
	}
	h := ChimeraHook{Event: p.Type, SessionID: p.SessionID, Description: p.Description, AgentID: p.AgentID, IsSubagent: p.IsSubagent, Reason: p.Reason, RequestID: p.RequestID, PendingRequests: p.PendingRequests, Started: p.Started, Running: p.Running, Woke: p.Woke}
	switch p.Type {
	case "background.start":
		if p.JobID != "" {
			h.Started = &store.BackgroundJob{Kind: store.JobBash, ID: p.JobID, Label: p.Command}
			h.Shell = &store.BackgroundShell{ID: p.JobID, Command: p.Command}
		}
	case "background.exit":
		if p.JobID != "" {
			h.Woke = append(h.Woke, p.JobID)
			h.Shell = &store.BackgroundShell{ID: p.JobID, Command: p.Command}
			h.ExitCode = p.ExitCode
		}
	case "agent.spawn":
		if p.Background && p.AgentID != "" {
			h.Started = &store.BackgroundJob{Kind: store.JobSubagent, ID: p.AgentID, Label: p.Description}
		}
	case "agent.finish":
		if p.AgentID != "" {
			h.Woke = append(h.Woke, p.AgentID)
		}
	}
	return h, nil
}

// ApplyChimeraHook applies a main session's ordered mod report. Subagents'
// own turns never describe the parent's state.
func ApplyChimeraHook(s *store.Store, report ChimeraHookReport, pushNotifier, osNotifier service.Notifier) error {
	ctx := context.Background()
	session, err := s.GetRunningAgentSession(ctx, report.TaskID, report.ExecutionID)
	if err != nil {
		return fmt.Errorf("getting agent session: %w", err)
	}
	if report.ExecutionID != "" && session.ExecutionID != report.ExecutionID {
		return fmt.Errorf("execution %q does not own running task session %s", report.ExecutionID, report.TaskID)
	}
	h := report.Hook
	if h.IsSubagent {
		return nil
	}
	now := time.Now().UTC()
	if report.ExecutionID != "" {
		if err := recordChimeraSubagents(ctx, s, report.ExecutionID, h, now); err != nil {
			return fmt.Errorf("recording subagent activity: %w", err)
		}
	}
	cleaned := cleanHook(ClaudeHook{Started: h.Started, Running: h.Running})
	jobs := append(store.BackgroundJobs(nil), session.BackgroundJobs...)
	if h.Running != nil {
		jobs = runningJobs(jobs, cleaned.Running, now)
	}
	if cleaned.Started != nil {
		jobs = withJob(jobs, *cleaned.Started, now)
	}
	for _, id := range h.Woke {
		jobs = withoutJob(jobs, cleanText(id, maxJobIDOrWhen))
	}
	if len(jobs) > maxJobs {
		jobs = jobs[:maxJobs]
	}
	activity := session.Activity
	decided := false
	switch h.Event {
	case "session.start", "session.end":
		jobs = nil
		activity, decided = "", true
	case "turn.start":
		activity, decided = store.ActivityWorking, true
	case "question.request":
		activity, decided = store.ActivityWaiting, true
	case "permission.pending":
		if h.PendingRequests != nil {
			if *h.PendingRequests > 0 {
				activity, decided = store.ActivityWaiting, true
			} else if activity == store.ActivityWaiting {
				activity, decided = store.ActivityWorking, true
			}
		}
	case "permission.resolved", "question.resolved":
		if h.PendingRequests != nil && *h.PendingRequests == 0 {
			activity, decided = store.ActivityWorking, true
		}
	case "turn.complete":
		activity, decided = "", true
		switch h.Reason {
		case "stop":
			if len(jobs) > 0 {
				activity = store.ActivityBackground
			}
		case "error":
			activity = store.ActivityError
		case "waiting_for_input":
			activity = store.ActivityWaiting
		}
	case "background.exit", "agent.finish":
		if activity == store.ActivityBackground && len(jobs) == 0 {
			activity, decided = "", true
		}
	}
	children, err := chimeraChildren(session.ChimeraSubagents, h, now.UnixMilli())
	if err != nil {
		return err
	}
	shells := chimeraShells(session.BackgroundShells, h, now)
	if err := s.SetChimeraHookState(ctx, session.ID, activity, jobs, children, shells); err != nil {
		if errors.Is(err, store.ErrAgentStateIgnored) {
			return nil
		}
		return fmt.Errorf("updating agent state: %w", err)
	}
	if !decided {
		if !sameJobs(jobs, session.BackgroundJobs) || children != session.ChimeraSubagents || !sameShells(shells, session.BackgroundShells) {
			ipc.Broadcast(ipc.Message{Type: "agent_state", TaskID: report.TaskID, Status: activity})
		}
		return nil
	}
	return settleActivity(ctx, s, session, report.TaskID, report.ExecutionID, activity, report.WorkingDir, now, pushNotifier, osNotifier)
}

// recordChimeraSubagents times the session's subagents, for task time: each
// working from its agent.spawn to its agent.finish, foreground or
// background. The mod reports only the main session's own subagents (it does
// nothing inside a subagent), so a subagent's own subagents and its waits on
// the operator are not seen. The session's end ends them all.
func recordChimeraSubagents(ctx context.Context, s *store.Store, executionID string, h ChimeraHook, now time.Time) error {
	id := cleanText(h.AgentID, maxJobIDOrWhen)
	var err error
	switch h.Event {
	case "agent.spawn":
		if id != "" {
			err = s.RecordSubagentActivityAt(ctx, executionID, id, "", store.ActivityWorking, now)
		}
	case "agent.finish":
		if id != "" {
			err = s.RecordSubagentActivityAt(ctx, executionID, id, "", "", now)
		}
	case "session.end":
		err = s.EndSubagentsAt(ctx, executionID, nil, now)
	}
	if errors.Is(err, store.ErrExecutionTerminal) {
		return nil
	}
	return err
}

type chimeraChild struct {
	ID        string `json:"subagentId"`
	SessionID string `json:"sessionId"`
	Purpose   string `json:"purpose,omitempty"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	StartedAt int64  `json:"startedAt,omitempty"`
	EndedAt   int64  `json:"endedAt,omitempty"`
}

func chimeraChildren(raw string, h ChimeraHook, now int64) (string, error) {
	children := []chimeraChild{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &children); err != nil {
			return "", fmt.Errorf("reading Chimera children: %w", err)
		}
	}
	if h.Event == "session.start" {
		kept := children[:0]
		for _, child := range children {
			if child.SessionID == h.SessionID {
				kept = append(kept, child)
			}
		}
		children = kept
	}
	if (h.Event == "agent.spawn" || h.Event == "agent.finish") && h.AgentID != "" {
		index := -1
		for i := range children {
			if children[i].ID == h.AgentID && children[i].SessionID == h.SessionID {
				index = i
				break
			}
		}
		if index < 0 {
			children = append(children, chimeraChild{ID: h.AgentID, SessionID: h.SessionID, Purpose: cleanText(h.Description, maxJobLabel), Status: "running"})
			index = len(children) - 1
		}
		child := &children[index]
		if h.Event == "agent.spawn" && child.StartedAt == 0 {
			child.StartedAt = now
		}
		if h.Event == "agent.finish" {
			child.Status, child.Reason, child.EndedAt = "error", h.Reason, now
			if h.Reason == "stop" {
				child.Status = "finished"
			}
			if h.Reason == "interrupted" {
				child.Status = "interrupted"
			}
		}
	}
	if h.Event == "session.end" {
		for i := range children {
			if children[i].Status == "running" {
				children[i].Status, children[i].Reason, children[i].EndedAt = "interrupted", "interrupted", now
			}
		}
	}
	// Bound completion history without discarding active children.
	for len(children) > 1000 {
		removed := false
		for i := range children {
			if children[i].Status != "running" {
				children = append(children[:i], children[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			break
		}
	}
	encoded, err := json.Marshal(children)
	return string(encoded), err
}

// maxShellCommand bounds a shell's command as kept: the tile shows its
// first line, the open tile the rest.
const maxShellCommand = 1000

// chimeraShells is the session's background shells after one mod event: a
// background.start adds one running, a background.exit marks it exited
// with its code (none when the event gives none), and the process's exit
// (session.end with reason exit) marks those still running killed, as they
// end with it. Ended shells stay listed, so the panel shows how they ended;
// at most maxJobs are kept, the oldest ended going first.
func chimeraShells(known store.BackgroundShells, h ChimeraHook, now time.Time) store.BackgroundShells {
	shells := append(store.BackgroundShells(nil), known...)
	index := func(id string) int {
		for i := range shells {
			if shells[i].ID == id {
				return i
			}
		}
		return -1
	}
	switch {
	case h.Event == "background.start" && h.Shell != nil:
		id := cleanText(h.Shell.ID, maxJobIDOrWhen)
		if id == "" || index(id) >= 0 {
			return shells
		}
		shells = append(shells, store.BackgroundShell{ID: id, Command: cleanShellCommand(h.Shell.Command), Status: store.ShellRunning, StartedAt: now})
	case h.Event == "background.exit" && h.Shell != nil:
		id := cleanText(h.Shell.ID, maxJobIDOrWhen)
		i := index(id)
		if id == "" {
			return shells
		}
		if i < 0 {
			// Its start was missed: it is known from its end alone.
			shells = append(shells, store.BackgroundShell{ID: id, Command: cleanShellCommand(h.Shell.Command), Status: store.ShellRunning, StartedAt: now})
			i = len(shells) - 1
		}
		if shells[i].Status == store.ShellRunning {
			shells[i].Status, shells[i].EndedAt = store.ShellExited, now
			if h.ExitCode != nil {
				code := *h.ExitCode
				shells[i].ExitCode = &code
			}
		}
	case h.Event == "session.end" && h.Reason == "exit":
		for i := range shells {
			if shells[i].Status == store.ShellRunning {
				shells[i].Status, shells[i].EndedAt = store.ShellKilled, now
			}
		}
	}
	for len(shells) > maxJobs {
		dropped := false
		for i := range shells {
			if shells[i].Status != store.ShellRunning {
				shells = append(shells[:i], shells[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			shells = shells[1:]
		}
	}
	return shells
}

// cleanShellCommand keeps a command's lines but drops other control
// characters, and clips it.
func cleanShellCommand(command string) string {
	command = strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(command))
	if r := []rune(command); len(r) > maxShellCommand {
		command = string(r[:maxShellCommand-1]) + "…"
	}
	return command
}

func sameShells(a, b store.BackgroundShells) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.ID != y.ID || x.Command != y.Command || x.Status != y.Status || !x.StartedAt.Equal(y.StartedAt) || !x.EndedAt.Equal(y.EndedAt) ||
			(x.ExitCode == nil) != (y.ExitCode == nil) || (x.ExitCode != nil && *x.ExitCode != *y.ExitCode) {
			return false
		}
	}
	return true
}
