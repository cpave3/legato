package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	// A subagent's start and end, which time it (see recordClaudeSubagents).
	ClaudeSubagentStart = "SubagentStart"
	ClaudeSubagentStop  = "SubagentStop"
)

// HookTurnEnded is no agent tool's own hook: a native bridge (Claude Code's
// or Codex's) sends it when a main turn ends without an answer (an
// interrupt, an error), which no hook reports. Reason "failed" or "error" is
// an error. See ApplyTurnEnded.
const HookTurnEnded = "TurnEnded"

// HookOperatorTurn is not a Claude Code hook either: a bridge sends it when
// the operator's own message reached its agent (a Codex user message), so
// the agent's operator items close. It says nothing of the agent's activity.
const HookOperatorTurn = "OperatorTurn"

// OperatorTurnHook is the report of a user turn whose text was text. Only
// whether the operator wrote it travels, never the text.
func OperatorTurnHook(text string) ClaudeHook {
	return ClaudeHook{Event: HookOperatorTurn, OperatorTurn: service.IsOperatorText(text)}
}

// ClaudeHook is what Legato keeps of one Claude Code hook payload: only
// what the agent's state needs, so a large tool result never travels to the
// host.
type ClaudeHook struct {
	Event string `json:"event"`
	// AgentID is set when a subagent fired the hook, not the main loop.
	AgentID string `json:"agent_id,omitempty"`
	// Started is the background job a tool call started (PostToolUse).
	Started *store.BackgroundJob `json:"started,omitempty"`
	// Edited is the file an edit tool call changed (PostToolUse of Edit,
	// Write, MultiEdit or NotebookEdit): only its path, never its content.
	Edited string `json:"edited,omitempty"`
	// Woke are the background jobs whose end woke the session
	// (UserPromptSubmit with task notifications).
	Woke []string `json:"woke,omitempty"`
	// Running is what still runs as a turn ends: the session's background
	// tasks (Stop and SubagentStop, at every depth) and session crons (Stop).
	Running []store.BackgroundJob `json:"running,omitempty"`
	// Listed says the payload carried the session's background tasks, so
	// Running is the whole truth even when empty (SubagentStop; a Stop
	// always is).
	Listed bool `json:"listed,omitempty"`
	// Trigger is a compaction's: "manual" or "auto".
	Trigger string `json:"trigger,omitempty"`
	// Reason is a SessionEnd's, or a TurnEnded's.
	Reason string `json:"reason,omitempty"`
	// Error is a StopFailure's.
	Error string `json:"error,omitempty"`
	// OperatorTurn says a user turn was the operator's own words, not a
	// [legato …] message or a task notification: it closes the agent's
	// operator items (UserPromptSubmit, OperatorTurn).
	OperatorTurn bool `json:"operator_turn,omitempty"`
}

type claudeHookPayload struct {
	Event           string                  `json:"hook_event_name"`
	AgentID         string                  `json:"agent_id"`
	ToolName        string                  `json:"tool_name"`
	ToolInput       json.RawMessage         `json:"tool_input"`
	ToolResponse    json.RawMessage         `json:"tool_response"`
	Prompt          string                  `json:"prompt"`
	TranscriptPath  string                  `json:"transcript_path"`
	BackgroundTasks *[]claudeBackgroundTask `json:"background_tasks"`
	SessionCrons    []struct {
		ID       string `json:"id"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
	} `json:"session_crons"`
	Trigger string `json:"trigger"`
	Reason  string `json:"reason"`
	Error   string `json:"error"`
}

// claudeBackgroundTask is one entry of a payload's background_tasks: what
// the whole session still runs, at every depth of subagent.
type claudeBackgroundTask struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Command     string `json:"command"`
	AgentType   string `json:"agent_type"`
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
		hook.Edited = editedFile(p.ToolName, p.ToolInput)
	case ClaudeUserPromptSubmit:
		hook.Woke = wokenBy(p.Prompt)
		hook.OperatorTurn = hook.Woke == nil && !strings.HasPrefix(strings.TrimSpace(p.Prompt), "<task-notification>") && service.IsOperatorText(p.Prompt)
	case ClaudeStop, ClaudeSubagentStop:
		hook.Listed = p.BackgroundTasks != nil
		var tasks []claudeBackgroundTask
		if p.BackgroundTasks != nil {
			tasks = *p.BackgroundTasks
		}
		for _, task := range tasks {
			job := store.BackgroundJob{ID: task.ID, Kind: task.Type, Label: strings.TrimSpace(firstNonBlank(task.Description, task.Command))}
			switch task.Type {
			case "shell":
				job.Kind = store.JobBash
			case "subagent":
				job.Kind, job.Label = store.JobSubagent, subagentLabel(task.AgentType, task.Description)
			}
			hook.Running = append(hook.Running, job)
		}
		subagentParents(hook.Running, p.TranscriptPath)
		if p.Event == ClaudeSubagentStop {
			// The main loop's crons are its own: only its Stop lists them.
			break
		}
		for _, cron := range p.SessionCrons {
			hook.Running = append(hook.Running, store.BackgroundJob{Kind: store.JobCron, ID: cron.ID, Label: cron.Prompt, Schedule: cron.Schedule})
		}
	}
	return hook, nil
}

// maxSubagentMeta bounds the record of a subagent Legato reads.
const maxSubagentMeta = 64 << 10

var claudeAgentID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// subagentParents names the parent of each listed subagent, as Claude Code
// recorded it beside the session's transcript (subagents/agent-<id>.meta.json,
// parentAgentId): a list names none, and the start that would can reach
// Legato after it, or never (a session older than Legato's tracking). A
// record that is missing, too large or unreadable names none.
func subagentParents(jobs []store.BackgroundJob, transcriptPath string) {
	if !filepath.IsAbs(transcriptPath) || !strings.HasSuffix(transcriptPath, ".jsonl") {
		return
	}
	dir := filepath.Join(strings.TrimSuffix(transcriptPath, ".jsonl"), "subagents")
	for i, job := range jobs {
		if i >= maxJobs {
			return
		}
		if job.Kind != store.JobSubagent || !claudeAgentID.MatchString(job.ID) {
			continue
		}
		file, err := os.Open(filepath.Join(dir, "agent-"+job.ID+".meta.json"))
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(file, maxSubagentMeta+1))
		file.Close()
		if err != nil || len(raw) > maxSubagentMeta {
			continue
		}
		var meta struct {
			Parent string `json:"parentAgentId"`
		}
		if json.Unmarshal(raw, &meta) == nil && claudeAgentID.MatchString(meta.Parent) {
			jobs[i].Parent = meta.Parent
		}
	}
}

// maxEditedPath bounds the path an edit hook carries; a longer one is
// dropped.
const maxEditedPath = 4096

// editedFile is the file a Claude Code tool call changed, "" when it
// changed none.
func editedFile(tool string, rawInput json.RawMessage) string {
	var input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if json.Unmarshal(rawInput, &input) != nil {
		return ""
	}
	path := ""
	switch tool {
	case "Edit", "Write", "MultiEdit":
		path = input.FilePath
	case "NotebookEdit":
		path = input.NotebookPath
	}
	if len(path) > maxEditedPath || strings.ContainsFunc(path, unicode.IsControl) {
		return ""
	}
	return path
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
	if hook.OperatorTurn && (hook.Event == ClaudeUserPromptSubmit || hook.Event == HookOperatorTurn) && hook.AgentID == "" {
		if _, err := service.CloseOperatorItemsForOperatorTurn(ctx, s, report.TaskID, session.ExecutionID); err != nil {
			return fmt.Errorf("closing operator items: %w", err)
		}
	}
	if hook.Event == HookOperatorTurn {
		return nil
	}
	if report.ExecutionID != "" {
		if err := recordClaudeSubagents(ctx, s, report.ExecutionID, hook, now); err != nil {
			return fmt.Errorf("recording subagent activity: %w", err)
		}
	}
	if hook.Event == ClaudePostToolUse && hook.Edited != "" {
		// An edit in the working folder, the main loop's or a subagent's,
		// is evidence of the work's stage.
		if _, err := service.RecordEditEvidence(ctx, s, *session, hook.Edited, report.WorkingDir); err != nil {
			log.Printf("stage evidence for %s: %v", report.TaskID, err)
		}
	}
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
			jobs = endedJob(jobs, id)
		}
	case ClaudePostToolUse:
		if hook.Started != nil {
			// A subagent's own background work is listed under it.
			started := *hook.Started
			started.Parent = hook.AgentID
			jobs = withJob(jobs, started, now)
		}
		// A subagent's tool calls say nothing of the main loop's turn: a
		// background subagent makes them after that turn has ended. Only
		// its permission prompt, once answered, is over.
		switch {
		case hook.AgentID == "":
			set(store.ActivityWorking)
		case session.Activity == store.ActivityWaiting && hasJob(jobs, hook.AgentID):
			set(store.ActivityBackground)
		case session.Activity == store.ActivityWaiting:
			set(store.ActivityWorking)
		}
		// Its permission prompt is over. A tool call never wakes a
		// subagent whose turn ended: Claude Code wakes one with a
		// SubagentStart, and a call can be reported after its stop.
		if subagentState(jobs, hook.AgentID) == store.ActivityWaiting {
			jobs = withSubagentState(jobs, hook.AgentID, store.ActivityWorking)
		}
	case ClaudePermissionRequest:
		set(store.ActivityWaiting)
		jobs = withSubagentState(jobs, hook.AgentID, store.ActivityWaiting)
	case ClaudeSubagentStart:
		// A background subagent its children woke starts again.
		jobs = withSubagentState(jobs, hook.AgentID, store.ActivityWorking)
	case ClaudeSubagentStop:
		if hook.Listed {
			jobs = listedJobs(jobs, hook.Running, hook.AgentID, now)
		} else {
			jobs = endedJob(jobs, hook.AgentID)
		}
	case ClaudeStop:
		if hook.AgentID == "" {
			jobs = listedJobs(jobs, hook.Running, "", now)
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

	if len(jobs) > maxJobs {
		jobs = jobs[:maxJobs]
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
		if errors.Is(err, store.ErrAgentStateIgnored) {
			return nil
		}
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
	return settleActivity(ctx, s, session, report.TaskID, report.ExecutionID, activity, report.WorkingDir, now, pushNotifier, osNotifier)
}

// recordClaudeSubagents times the session's subagents, for task time: each
// from its SubagentStart to its SubagentStop, waiting while its permission
// prompt is open. Only a SubagentStart starts one, so Claude Code's own
// agents (prompt suggestions, compaction), which fire only SubagentStop, are
// not counted. The main loop's Stop lists the subagents still running in the
// background; it ends any other, should its SubagentStop have been lost, as a
// foreground subagent cannot outlive the turn. The session's end ends them
// all. A subagent's parent is the subagent whose Agent call started it, or
// the one Claude Code recorded beside the transcript (see subagentParents);
// "" is the top-level agent. A background subagent its children wake gets a
// SubagentStart again and a new span.
func recordClaudeSubagents(ctx context.Context, s *store.Store, executionID string, hook ClaudeHook, now time.Time) error {
	id := cleanText(hook.AgentID, maxJobIDOrWhen)
	// A subagent's parent is known once the parent's call that started it
	// is reported, or from what Claude Code recorded of it.
	parents := map[string]string{}
	if hook.Started != nil && hook.Started.Kind == store.JobSubagent && id != "" {
		parents[hook.Started.ID] = id
	}
	for _, job := range hook.Running {
		if job.Kind == store.JobSubagent && job.Parent != "" {
			parents[job.ID] = job.Parent
		}
	}
	for child, parent := range parents {
		if err := s.SetSubagentParent(ctx, executionID, child, parent); err != nil {
			return err
		}
	}
	var err error
	switch hook.Event {
	case ClaudeSubagentStart:
		if id != "" {
			err = s.RecordSubagentActivityAt(ctx, executionID, id, "", store.ActivityWorking, now)
		}
	case ClaudeSubagentStop:
		if id != "" {
			err = s.RecordSubagentActivityAt(ctx, executionID, id, "", "", now)
		}
	case ClaudePermissionRequest, ClaudePostToolUse:
		if id == "" {
			return nil
		}
		open, openErr := s.OpenSubagents(ctx, executionID)
		if openErr != nil {
			return openErr
		}
		if !slices.Contains(open, id) {
			return nil
		}
		state := store.ActivityWorking
		if hook.Event == ClaudePermissionRequest {
			state = store.ActivityWaiting
		}
		err = s.RecordSubagentActivityAt(ctx, executionID, id, "", state, now)
	case ClaudeStop:
		if id != "" {
			return nil
		}
		var running []string
		for _, job := range hook.Running {
			if job.Kind == store.JobSubagent {
				running = append(running, job.ID)
			}
		}
		err = s.EndSubagentsAt(ctx, executionID, running, now)
	case ClaudeSessionEnd:
		err = s.EndSubagentsAt(ctx, executionID, nil, now)
	}
	if errors.Is(err, store.ErrExecutionTerminal) {
		return nil
	}
	return err
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

// withJob adds a job the agent started. One listed already (a list may
// come before the start's own report) keeps what it has and gains the
// parent and label it lacked.
func withJob(jobs store.BackgroundJobs, job store.BackgroundJob, now time.Time) store.BackgroundJobs {
	for i, j := range jobs {
		if j.ID != job.ID {
			continue
		}
		out := append(store.BackgroundJobs(nil), jobs...)
		if out[i].Parent == "" {
			out[i].Parent = job.Parent
		}
		if out[i].Label == "" {
			out[i].Label = job.Label
		}
		return out
	}
	job.StartedAt = now
	if job.Kind == store.JobSubagent {
		job.State = store.ActivityWorking
	}
	return append(jobs, job)
}

// subagentState is a listed subagent's state, "" for any other id.
func subagentState(jobs store.BackgroundJobs, id string) string {
	for _, j := range jobs {
		if id != "" && j.ID == id && j.Kind == store.JobSubagent {
			return j.State
		}
	}
	return ""
}

// withSubagentState moves a listed subagent to a state: a hook it fired
// says what it does. Any other id (the main loop's, a foreground
// subagent's) changes nothing.
func withSubagentState(jobs store.BackgroundJobs, id, state string) store.BackgroundJobs {
	if id == "" {
		return jobs
	}
	for i, j := range jobs {
		if j.ID == id && j.Kind == store.JobSubagent && j.State != state {
			out := append(store.BackgroundJobs(nil), jobs...)
			out[i].State = state
			return out
		}
	}
	return jobs
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

// runningJobs is what still runs at the end of a Chimera turn: the turn's
// own list, which is the whole truth, with what Legato knew of each job
// before. Chimera reports no subagent's own work, so its list is flat.
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

// endedJob is the jobs once one has ended (its turn, for a subagent): it
// leaves, unless work it started still runs, when it waits on that in the
// background.
func endedJob(jobs store.BackgroundJobs, id string) store.BackgroundJobs {
	if !hasJob(jobs, id) {
		return jobs
	}
	var out store.BackgroundJobs
	for _, j := range jobs {
		if j.ID != id {
			out = append(out, j)
			continue
		}
		if hasDescendant(jobs, id) {
			j.State = store.ActivityBackground
			out = append(out, j)
		}
	}
	return out
}

// listedJobs is what still runs once a turn has ended, the main loop's
// (stopped "") or a subagent's: the session's list, which is the whole
// truth at every depth, with what Legato knew of each job before. Claude
// Code lists a Monitor as a shell; its start said it was a Monitor.
//
// The list leaves out a subagent whose turn has ended while work it started
// runs on, so such a one stays while it has any (ActivityBackground); and
// through the end of its last, since Claude Code wakes it then. The
// subagent whose turn has just ended is listed too: it stays only as such a
// parent. The main loop's crons are listed only at its own Stop.
func listedJobs(known store.BackgroundJobs, listed []store.BackgroundJob, stopped string, now time.Time) store.BackgroundJobs {
	byID := make(map[string]store.BackgroundJob, len(listed))
	for _, job := range listed {
		byID[job.ID] = job
	}
	keep := func(k store.BackgroundJob) bool {
		if _, ok := byID[k.ID]; ok {
			return true
		}
		switch {
		case k.Kind == store.JobCron:
			return stopped != ""
		case k.Kind == store.JobSubagent:
			return hasDescendant(known, k.ID)
		}
		return false
	}
	var out store.BackgroundJobs
	seen := map[string]bool{}
	for _, k := range known {
		seen[k.ID] = true
		if k.ID == stopped || !keep(k) {
			continue
		}
		if job, ok := byID[k.ID]; ok {
			k = mergedJob(k, job)
		} else if k.Kind == store.JobSubagent {
			k.State = store.ActivityBackground
		}
		out = append(out, k)
	}
	for _, job := range listed {
		if seen[job.ID] || job.ID == stopped {
			continue
		}
		seen[job.ID] = true
		job.StartedAt = now
		if job.Kind == store.JobSubagent {
			job.State = store.ActivityWorking
		}
		out = append(out, job)
	}
	// The subagent whose turn ended waits on what it started, if anything.
	for _, k := range append(append(store.BackgroundJobs(nil), known...), listed...) {
		if k.ID != stopped || stopped == "" {
			continue
		}
		if hasDescendant(out, stopped) {
			if _, ok := byID[k.ID]; ok && !hasJob(known, k.ID) {
				k.StartedAt = now
			}
			k.State = store.ActivityBackground
			out = append(out, k)
		}
		break
	}
	return out
}

// mergedJob is a job Legato knew, as a list names it now.
func mergedJob(known, listed store.BackgroundJob) store.BackgroundJob {
	job := known
	if !(known.Kind == store.JobMonitor && listed.Kind == store.JobBash) {
		job.Kind = listed.Kind
	}
	if listed.Label != "" && known.Label == "" {
		job.Label = listed.Label
	}
	if job.Parent == "" {
		job.Parent = listed.Parent
	}
	if listed.Schedule != "" {
		job.Schedule = listed.Schedule
	}
	// A listed subagent's turn runs, unless it waits on the operator.
	if job.Kind == store.JobSubagent && job.State != store.ActivityWaiting {
		job.State = store.ActivityWorking
	}
	return job
}

// hasDescendant says whether a job started, at any depth, another that is
// listed. A parent chain that loops ends the walk.
func hasDescendant(jobs store.BackgroundJobs, id string) bool {
	parent := make(map[string]string, len(jobs))
	for _, j := range jobs {
		parent[j.ID] = j.Parent
	}
	for _, j := range jobs {
		if j.ID == id {
			continue
		}
		at := j.Parent
		for depth := 0; at != "" && depth < len(jobs); depth++ {
			if at == id {
				return true
			}
			at = parent[at]
		}
	}
	return false
}

func sameJobs(a, b store.BackgroundJobs) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Kind != y.Kind || x.ID != y.ID || x.Label != y.Label || x.Schedule != y.Schedule || x.Parent != y.Parent || x.State != y.State || !x.StartedAt.Equal(y.StartedAt) {
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
	hook.AgentID = cleanText(hook.AgentID, maxJobIDOrWhen)
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
	job.Parent = cleanText(job.Parent, maxJobIDOrWhen)
	// Legato decides a subagent's state from its hooks.
	job.State = ""
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
