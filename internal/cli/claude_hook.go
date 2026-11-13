package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Claude Code hook events Legato reads, and the one the native bridge adds.
const (
	ClaudeSessionStart      = "SessionStart"
	ClaudeUserPromptSubmit  = "UserPromptSubmit"
	ClaudePostToolUse       = "PostToolUse"
	ClaudePermissionRequest = "PermissionRequest"
	ClaudeStop              = "Stop"
	ClaudeStopFailure       = "StopFailure"
	ClaudePreCompact        = "PreCompact"
	ClaudePostCompact       = "PostCompact"
	ClaudeSessionEnd        = "SessionEnd"
)

// HookTurnEnded is no agent tool's own hook: a native bridge (Claude Code's
// or Codex's) sends it when a main turn ends without an answer (an
// interrupt, an error), which no hook reports. Reason "failed" or "error" is
// an error. See ApplyTurnEnded.
const HookTurnEnded = "TurnEnded"

// ClaudeHook is what Legato keeps of one Claude Code hook payload: only
// what the agent's state needs, so a large tool result never travels to the
// host.
type ClaudeHook struct {
	Event string `json:"event"`
	// AgentID is set when a subagent fired the hook, not the main loop.
	AgentID string `json:"agent_id,omitempty"`
	// Started is the background job a tool call started (PostToolUse).
	Started *store.BackgroundJob `json:"started,omitempty"`
	// Woke are the background jobs whose end woke the session
	// (UserPromptSubmit with task notifications).
	Woke []string `json:"woke,omitempty"`
	// Running is what still runs as the turn ends (Stop): background tasks
	// and session crons.
	Running []store.BackgroundJob `json:"running,omitempty"`
	// Trigger is a compaction's: "manual" or "auto".
	Trigger string `json:"trigger,omitempty"`
	// Reason is a SessionEnd's, or a TurnEnded's.
	Reason string `json:"reason,omitempty"`
	// Error is a StopFailure's.
	Error string `json:"error,omitempty"`
}

type claudeHookPayload struct {
	Event           string          `json:"hook_event_name"`
	AgentID         string          `json:"agent_id"`
	ToolName        string          `json:"tool_name"`
	ToolInput       json.RawMessage `json:"tool_input"`
	ToolResponse    json.RawMessage `json:"tool_response"`
	Prompt          string          `json:"prompt"`
	BackgroundTasks []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Command     string `json:"command"`
		AgentType   string `json:"agent_type"`
	} `json:"background_tasks"`
	SessionCrons []struct {
		ID       string `json:"id"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
	} `json:"session_crons"`
	Trigger string `json:"trigger"`
	Reason  string `json:"reason"`
	Error   string `json:"error"`
}

// ParseClaudeHook reads a Claude Code hook payload.
func ParseClaudeHook(raw []byte) (ClaudeHook, error) {
	var p claudeHookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return ClaudeHook{}, fmt.Errorf("reading the hook payload: %w", err)
	}
	if p.Event == "" {
		return ClaudeHook{}, fmt.Errorf("the hook payload names no hook_event_name")
	}
	hook := ClaudeHook{Event: p.Event, AgentID: p.AgentID, Trigger: p.Trigger, Reason: p.Reason, Error: p.Error}
	switch p.Event {
	case ClaudePostToolUse:
		hook.Started = startedJob(p.ToolName, p.ToolInput, p.ToolResponse)
	case ClaudeUserPromptSubmit:
		hook.Woke = wokenBy(p.Prompt)
	case ClaudeStop:
		for _, task := range p.BackgroundTasks {
			job := store.BackgroundJob{ID: task.ID, Kind: task.Type, Label: strings.TrimSpace(firstNonBlank(task.Description, task.Command))}
			switch task.Type {
			case "shell":
				job.Kind = store.JobBash
			case "subagent":
				job.Kind, job.Label = store.JobSubagent, subagentLabel(task.AgentType, task.Description)
			}
			hook.Running = append(hook.Running, job)
		}
		for _, cron := range p.SessionCrons {
			hook.Running = append(hook.Running, store.BackgroundJob{Kind: store.JobCron, ID: cron.ID, Label: cron.Prompt, Schedule: cron.Schedule})
		}
	}
	return hook, nil
}

// startedJob is the background job a tool call started, nil when it
// started none.
func startedJob(tool string, rawInput, rawResponse json.RawMessage) *store.BackgroundJob {
	var input struct {
		Command      string `json:"command"`
		Description  string `json:"description"`
		SubagentType string `json:"subagent_type"`
		Prompt       string `json:"prompt"`
		Cron         string `json:"cron"`
	}
	var response struct {
		BackgroundTaskID string `json:"backgroundTaskId"`
		TaskID           string `json:"taskId"`
		AgentID          string `json:"agentId"`
		IsAsync          bool   `json:"isAsync"`
		ID               string `json:"id"`
	}
	_ = json.Unmarshal(rawInput, &input)
	// A tool's response is an object, a string or a list; only an object
	// names a job.
	_ = json.Unmarshal(rawResponse, &response)
	label := strings.TrimSpace(firstNonBlank(input.Description, input.Command))
	switch {
	case tool == "Bash" && response.BackgroundTaskID != "":
		return &store.BackgroundJob{Kind: store.JobBash, ID: response.BackgroundTaskID, Label: label}
	case tool == "Monitor" && response.TaskID != "":
		return &store.BackgroundJob{Kind: store.JobMonitor, ID: response.TaskID, Label: label}
	case (tool == "Agent" || tool == "Task") && response.IsAsync && response.AgentID != "":
		return &store.BackgroundJob{Kind: store.JobSubagent, ID: response.AgentID, Label: subagentLabel(input.SubagentType, input.Description)}
	case tool == "CronCreate" && response.ID != "":
		return &store.BackgroundJob{Kind: store.JobCron, ID: response.ID, Label: input.Prompt, Schedule: input.Cron}
	}
	return nil
}

func subagentLabel(agentType, description string) string {
	if agentType == "" || description == "" {
		return strings.TrimSpace(firstNonBlank(description, agentType))
	}
	return agentType + ": " + description
}

var taskNotificationID = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)

// wokenBy are the background jobs a prompt reports the end of: Claude Code
// wakes a session with a prompt of <task-notification>s.
func wokenBy(prompt string) []string {
	if !strings.HasPrefix(strings.TrimSpace(prompt), "<task-notification>") {
		return nil
	}
	var ids []string
	for _, m := range taskNotificationID.FindAllStringSubmatch(prompt, -1) {
		ids = append(ids, strings.TrimSpace(m[1]))
	}
	return ids
}

// ClaudeHookReport is one hook of a Claude Code session, for the session's
// task and execution.
type ClaudeHookReport struct {
	TaskID      string
	ExecutionID string
	WorkingDir  string
	Hook        ClaudeHook
}

// ApplyClaudeHook moves a Claude Code agent's state on by one hook. See
// docs/ai/agent-states.md for what each event means.
func ApplyClaudeHook(s *store.Store, report ClaudeHookReport, pushNotifier, osNotifier service.Notifier) error {
	return applyClaudeHookAt(s, report, time.Now().UTC(), pushNotifier, osNotifier)
}

func applyClaudeHookAt(s *store.Store, report ClaudeHookReport, now time.Time, pushNotifier, osNotifier service.Notifier) error {
	ctx := context.Background()
	session, err := s.GetRunningAgentSession(ctx, report.TaskID, report.ExecutionID)
	if err != nil {
		return fmt.Errorf("getting agent session: %w", err)
	}
	if report.ExecutionID != "" && session.ExecutionID != report.ExecutionID {
		return fmt.Errorf("execution %q does not own running task session %s", report.ExecutionID, report.TaskID)
	}
	hook := cleanHook(report.Hook)
	jobs := append(store.BackgroundJobs(nil), session.BackgroundJobs...)
	activity, decided := session.Activity, false
	set := func(to string) { activity, decided = to, true }
	settled := func() string {
		if len(jobs) > 0 {
			return store.ActivityBackground
		}
		return ""
	}

	switch hook.Event {
	case ClaudeUserPromptSubmit:
		set(store.ActivityWorking)
		for _, id := range hook.Woke {
			jobs = withoutJob(jobs, id)
		}
	case ClaudePostToolUse:
		// A subagent's tool calls say nothing of the main loop's turn: a
		// background subagent makes them after that turn has ended. Only
		// its permission prompt, once answered, is over.
		switch {
		case hook.AgentID == "":
			set(store.ActivityWorking)
			if hook.Started != nil {
				jobs = withJob(jobs, *hook.Started, now)
			}
		case session.Activity == store.ActivityWaiting && hasJob(jobs, hook.AgentID):
			set(store.ActivityBackground)
		case session.Activity == store.ActivityWaiting:
			set(store.ActivityWorking)
		}
	case ClaudePermissionRequest:
		set(store.ActivityWaiting)
	case ClaudeStop:
		if hook.AgentID == "" {
			jobs = runningJobs(jobs, hook.Running, now)
			set(settled())
		}
	case ClaudeStopFailure:
		// A subagent's error is reported to the turn that runs it.
		if hook.AgentID == "" {
			set(store.ActivityError)
		}
	case ClaudePreCompact:
		if hook.Trigger == "manual" {
			set(store.ActivityWorking)
		}
	case ClaudePostCompact:
		if hook.Trigger == "manual" {
			set(settled())
		}
	case ClaudeSessionEnd:
		jobs = nil
		set("")
	case HookTurnEnded:
		// Only a turn no Stop or StopFailure has settled: the bridge's
		// report may come before or after theirs.
		if session.Activity == store.ActivityWorking || session.Activity == store.ActivityWaiting {
			if hook.Reason == "failed" || hook.Reason == "error" {
				set(store.ActivityError)
			} else {
				set(settled())
			}
		}
	}

	jobsChanged := !sameJobs(jobs, session.BackgroundJobs)
	// A bridge's report is not the plugin's: a Codex session's own hooks
	// must still count after it.
	source := store.HookSourcePlugin
	if hook.Event == HookTurnEnded {
		source = ""
	}
	firstReport := source != "" && session.HookSource != source
	if !decided && !jobsChanged && !firstReport {
		return nil
	}
	if err := s.SetAgentHookState(ctx, session.ID, activity, jobs, source); err != nil {
		return fmt.Errorf("updating agent state: %w", err)
	}
	if !decided {
		if jobsChanged {
			ipc.Broadcast(ipc.Message{Type: "agent_state", TaskID: report.TaskID, Status: activity})
		}
		return nil
	}
	// An interrupt is the operator's own doing, or another agent's for it:
	// it ends the turn quietly. An error is still news.
	if hook.Event == HookTurnEnded && activity != store.ActivityError {
		pushNotifier, osNotifier = nil, nil
	}
	return settleActivity(ctx, s, session, report.TaskID, report.ExecutionID, activity, report.WorkingDir, pushNotifier, osNotifier)
}

// ApplyTurnEnded reports that a session's main turn ended without an answer,
// as a bridge saw it: idle (or background, with jobs still running), or
// error for reason "failed" or "error". It changes only a session still
// working or waiting, and never marks it as reported by the plugin, so the
// session's own hooks keep counting.
func ApplyTurnEnded(s *store.Store, taskID, executionID, reason string, pushNotifier, osNotifier service.Notifier) error {
	report := ClaudeHookReport{TaskID: taskID, ExecutionID: executionID, Hook: ClaudeHook{Event: HookTurnEnded, Reason: reason}}
	return ApplyClaudeHook(s, report, pushNotifier, osNotifier)
}

// withJob adds a job the agent started, unless it is listed already.
func withJob(jobs store.BackgroundJobs, job store.BackgroundJob, now time.Time) store.BackgroundJobs {
	for _, j := range jobs {
		if j.ID == job.ID {
			return jobs
		}
	}
	job.StartedAt = now
	return append(jobs, job)
}

func hasJob(jobs store.BackgroundJobs, id string) bool {
	for _, j := range jobs {
		if j.ID == id {
			return true
		}
	}
	return false
}

func withoutJob(jobs store.BackgroundJobs, id string) store.BackgroundJobs {
	var out store.BackgroundJobs
	for _, j := range jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	return out
}

// runningJobs is what still runs at the end of a turn: the turn's own list,
// which is the whole truth, with what Legato knew of each job before. Claude
// Code lists a Monitor as a shell; its start said it was a Monitor.
func runningJobs(known store.BackgroundJobs, running []store.BackgroundJob, now time.Time) store.BackgroundJobs {
	var out store.BackgroundJobs
	for _, job := range running {
		job.StartedAt = now
		for _, k := range known {
			if k.ID != job.ID {
				continue
			}
			job.StartedAt = k.StartedAt
			if k.Kind == store.JobMonitor && job.Kind == store.JobBash {
				job.Kind = store.JobMonitor
			}
			if job.Label == "" {
				job.Label = k.Label
			}
		}
		out = append(out, job)
	}
	return out
}

func sameJobs(a, b store.BackgroundJobs) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Kind != y.Kind || x.ID != y.ID || x.Label != y.Label || x.Schedule != y.Schedule || !x.StartedAt.Equal(y.StartedAt) {
			return false
		}
	}
	return true
}

// Limits on what a session reports of its jobs. The agent writes these, and
// they reach the Reeve's messages, the API and the operator's terminal.
const (
	maxJobs        = 32
	maxJobLabel    = 200
	maxJobIDOrWhen = 64
)

// cleanHook keeps a hook's jobs to what Legato can show safely: a known
// kind ("other" for any other), each text one line without control
// characters and clipped, and at most maxJobs of them.
func cleanHook(hook ClaudeHook) ClaudeHook {
	if hook.Started != nil {
		job := cleanJob(*hook.Started)
		hook.Started = &job
	}
	if len(hook.Running) > maxJobs {
		hook.Running = hook.Running[:maxJobs]
	}
	running := make([]store.BackgroundJob, len(hook.Running))
	for i, job := range hook.Running {
		running[i] = cleanJob(job)
	}
	if hook.Running != nil {
		hook.Running = running
	}
	return hook
}

func cleanJob(job store.BackgroundJob) store.BackgroundJob {
	switch job.Kind {
	case store.JobBash, store.JobMonitor, store.JobSubagent, store.JobCron:
	default:
		job.Kind = "other"
	}
	job.ID = cleanText(job.ID, maxJobIDOrWhen)
	job.Label = cleanText(job.Label, maxJobLabel)
	job.Schedule = cleanText(job.Schedule, maxJobIDOrWhen)
	return job
}

// cleanText is s on one line, without control characters, at most n runes.
func cleanText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}
