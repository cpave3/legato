package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// MacroPR names the pull request a macro is started on.
type MacroPR struct {
	Repo   string `json:"repo"`
	Number int    `json:"pr_number"`
	URL    string `json:"url,omitempty"`
}

// MacroStart asks for a macro on a task, and on one of its pull requests.
type MacroStart struct {
	Name   string
	TaskID string
	PR     *MacroPR
}

// MacroStartResult says what a start did: started the task's agent with
// the macro as its first message, or queued it to the agent running.
type MacroStartResult struct {
	TaskID    string `json:"task_id"`
	Started   bool   `json:"started"`
	Queued    bool   `json:"queued"`
	AgentKind string `json:"agent_kind,omitempty"`
}

// MacroAgents is what a macro start needs of the agent service.
type MacroAgents interface {
	CaptureOutput(ctx context.Context, taskID string) (string, error)
	QueueMessage(ctx context.Context, taskID, message string) error
	SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error
	DefaultAdapter() string
	GetTaskResumeSession(ctx context.Context, taskID, agentKind string) (*TaskResumeSession, error)
}

// ErrMacroAgentsMissing is a start with no agent service wired.
var ErrMacroAgentsMissing = errors.New("macros cannot start agents here")

// SetAgents lets Start start and message agents.
func (s *MacroService) SetAgents(agents MacroAgents) { s.agents = agents }

// Start puts a macro to work on a task: with an agent running, the message
// is queued as its next one; with none, the task's main agent starts with
// the macro's agent tool and tier (else the default tool), resuming its
// saved conversation, with the message as its first prompt. The message is
// a short header naming the task (and pull request), then the macro's
// text. It is never tagged: a macro is the operator's own words.
func (s *MacroService) Start(ctx context.Context, req MacroStart) (MacroStartResult, error) {
	if s.agents == nil {
		return MacroStartResult{}, ErrMacroAgentsMissing
	}
	macro, err := s.Get(ctx, req.Name)
	if err != nil {
		return MacroStartResult{}, err
	}
	message, err := s.Message(ctx, macro.Keys, req.TaskID, req.PR)
	if err != nil {
		return MacroStartResult{}, err
	}
	result := MacroStartResult{TaskID: req.TaskID}
	if _, err := s.agents.CaptureOutput(ctx, req.TaskID); err == nil {
		if err := s.agents.QueueMessage(ctx, req.TaskID, message); err != nil {
			return MacroStartResult{}, err
		}
		result.Queued = true
		return result, nil
	}
	opts := AgentSpawnOptions{AgentKind: macro.Agent, Tier: macro.Tier, Prompt: message}
	kind := macro.Agent
	if kind == "" {
		kind = s.agents.DefaultAdapter()
	}
	if SupportsTaskResume(kind) {
		saved, err := s.agents.GetTaskResumeSession(ctx, req.TaskID, kind)
		if err != nil {
			return MacroStartResult{}, err
		}
		if saved != nil {
			opts.SessionPolicy, opts.ResumeSessionID, opts.ResumeWorkingDir = "resume", saved.ID, saved.CWD
			opts.WorkingDir = saved.CWD
		}
	}
	if err := s.agents.SpawnAgent(ctx, req.TaskID, 0, 0, opts); err != nil {
		return MacroStartResult{}, err
	}
	result.Started, result.AgentKind = true, kind
	return result, nil
}

// Message is a macro's text as Start sends it: a header naming the task
// (its ticket key, else its ID, and title) and the pull request, then the
// text, without the trailing newline a picker uses to submit it.
func (s *MacroService) Message(ctx context.Context, text, taskID string, pr *MacroPR) (string, error) {
	task, err := s.settings.db.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("task %s: %w", taskID, err)
	}
	key := task.ID
	if task.RemoteID != nil && *task.RemoteID != "" {
		key = *task.RemoteID
	}
	header := fmt.Sprintf("Task %s: %s", key, task.Title)
	if pr != nil {
		line := fmt.Sprintf("Pull request %s#%d", pr.Repo, pr.Number)
		url := pr.URL
		if url == "" && pr.Repo != "" {
			url = fmt.Sprintf("https://github.com/%s/pull/%d", pr.Repo, pr.Number)
		}
		if url != "" {
			line += ": " + url
		}
		header += "\n" + line
	}
	return header + "\n\n" + strings.TrimRight(text, "\n"), nil
}
