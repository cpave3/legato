package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ChimeraHook is the state-only report carried from the ordered mod stream
// to the host. AgentID on agent.spawn/finish identifies the subject, not the emitter.
type ChimeraHook struct {
	Event      string `json:"event"`
	AgentID    string `json:"agent_id,omitempty"`
	IsSubagent bool   `json:"is_subagent,omitempty"`
	Reason     string `json:"reason,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	// PendingRequests is the number still open AFTER this event. A resolution
	// without a count cannot prove that the last request was answered.
	PendingRequests *int                 `json:"pending_requests,omitempty"`
	Started         *store.BackgroundJob `json:"started,omitempty"`
	// Running, when present, replaces the complete job list; nil preserves it.
	Running []store.BackgroundJob `json:"running"`
	Woke    []string              `json:"woke,omitempty"`
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
		AgentID         string                `json:"agentId"`
		JobID           string                `json:"jobId"`
		Command         string                `json:"command"`
		Description     string                `json:"description"`
		Background      bool                  `json:"background"`
		IsSubagent      bool                  `json:"isSubagent"`
		Reason          string                `json:"reason"`
		RequestID       string                `json:"requestId"`
		PendingRequests *int                  `json:"pendingRequests"`
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
	h := ChimeraHook{Event: p.Type, AgentID: p.AgentID, IsSubagent: p.IsSubagent, Reason: p.Reason, RequestID: p.RequestID, PendingRequests: p.PendingRequests, Started: p.Started, Running: p.Running, Woke: p.Woke}
	switch p.Type {
	case "background.start":
		if p.JobID != "" {
			h.Started = &store.BackgroundJob{Kind: store.JobBash, ID: p.JobID, Label: p.Command}
		}
	case "background.exit":
		if p.JobID != "" {
			h.Woke = append(h.Woke, p.JobID)
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
	cleaned := cleanHook(ClaudeHook{Started: h.Started, Running: h.Running})
	jobs := append(store.BackgroundJobs(nil), session.BackgroundJobs...)
	now := time.Now().UTC()
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
	case "permission.request", "question.request":
		activity, decided = store.ActivityWaiting, true
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
	if err := s.SetAgentHookState(ctx, session.ID, activity, jobs, store.HookSourceMod); err != nil {
		if errors.Is(err, store.ErrAgentStateIgnored) {
			return nil
		}
		return fmt.Errorf("updating agent state: %w", err)
	}
	if !decided {
		if !sameJobs(jobs, session.BackgroundJobs) {
			ipc.Broadcast(ipc.Message{Type: "agent_state", TaskID: report.TaskID, Status: activity})
		}
		return nil
	}
	return settleActivity(ctx, s, session, report.TaskID, report.ExecutionID, activity, report.WorkingDir, pushNotifier, osNotifier)
}
