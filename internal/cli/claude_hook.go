package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Claude Code hook events Legato reads, and the one the native bridge adds.
const (
	ClaudeSessionStart     = "SessionStart"
	ClaudeUserPromptSubmit = "UserPromptSubmit"
	ClaudePostToolUse      = "PostToolUse"
	ClaudePermissionReq    = "PermissionRequest"
	ClaudeStop             = "Stop"
	ClaudeStopFailure      = "StopFailure"
	ClaudePreCompact       = "PreCompact"
	ClaudePostCompact      = "PostCompact"
	ClaudeSessionEnd       = "SessionEnd"
	// ClaudeTurnEnded is not Claude Code's: a native bridge (Claude Code's
	// or Codex's) sends it when a main turn ends without an answer (an
	// interrupt, an error), which no hook reports. Reason "failed" or
	// "error" is an error.
	ClaudeTurnEnded = "TurnEnded"
)

// ClaudeHook is what Legato keeps of one Claude Code hook payload: only
// what the agent's state needs, so a large tool result never travels to the
// host.
type ClaudeHook struct {
	Event string `json:"event"`
	// AgentID is set when a subagent fired the hook, not the main loop.
	AgentID string `json:"agent_id,omitempty"`
	// Started is the background job a tool call started (PostToolUse).
	Started *store.BackgroundJob `json:"started,omitempty"`
	// Woke is the background job whose end woke the session
	// (UserPromptSubmit with a task notification).
	Woke string `json:"woke,omitempty"`
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
			job := store.BackgroundJob{ID: task.ID, Kind: task.Type, Label: firstText(task.Description, task.Command)}
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
	label := firstText(input.Description, input.Command)
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
		return firstText(description, agentType)
	}
	return agentType + ": " + description
}

var taskNotificationID = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)

// wokenBy is the background job a prompt reports the end of: Claude Code
// wakes a session for one with a prompt that is a <task-notification>.
func wokenBy(prompt string) string {
	if !strings.HasPrefix(strings.TrimSpace(prompt), "<task-notification>") {
		return ""
	}
	if m := taskNotificationID.FindStringSubmatch(prompt); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func firstText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
	hook := report.Hook
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
		if hook.Woke != "" {
			jobs = withoutJob(jobs, hook.Woke)
		}
	case ClaudePostToolUse:
		// A subagent's tool calls say nothing of the main loop's turn: a
		// background subagent makes them after that turn has ended.
		if hook.AgentID == "" {
			set(store.ActivityWorking)
			if hook.Started != nil {
				jobs = withJob(jobs, *hook.Started, now)
			}
		}
	case ClaudePermissionReq:
		set(store.ActivityWaiting)
	case ClaudeStop:
		if hook.AgentID == "" {
			jobs = runningJobs(jobs, hook.Running, now)
			set(settled())
		}
	case ClaudeStopFailure:
		set(store.ActivityError)
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
	case ClaudeTurnEnded:
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
	if hook.Event == ClaudeTurnEnded {
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
	return settleActivity(ctx, s, session, report.TaskID, report.ExecutionID, activity, report.WorkingDir, pushNotifier, osNotifier)
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
