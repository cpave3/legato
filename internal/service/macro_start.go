package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
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
	// Branch is the pull request's head branch: what a fresh clone checks
	// out when the task's folder is gone. The task's code context's branch
	// when empty.
	Branch string
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
	NewWorkDefaults(context.Context) (NewWorkDefaults, error)
	GetTaskResumeSession(ctx context.Context, taskID, agentKind string) (*TaskResumeSession, error)
}

// ErrMacroAgentsMissing is a start with no agent service wired.
var ErrMacroAgentsMissing = errors.New("macros cannot start agents here")

// ErrMacroFolderGone is a start whose task's folder is gone, with no
// repository to make a fresh clone of.
var ErrMacroFolderGone = errors.New("the task's folder is gone and it names no repository to clone")

// SetAgents lets Start start and message agents.
func (s *MacroService) SetAgents(agents MacroAgents) { s.agents = agents }

// Start puts a macro to work on a task: with an agent running, the message
// is queued as its next one; with none, the task's main agent starts with
// the macro's agent tool and tier (else the default tool), resuming its
// saved conversation in its own directory, or starting a new one in the
// task's checkout (macroWorkingDir), with the message as its first prompt.
// When that folder is gone (a job clone removed), the agent starts a new
// conversation in a fresh clone of the repository on the pull request's
// branch, and a task note says so; with no repository it does not start.
// The message is a short header naming the task (and pull request), then
// the macro's text. It is never tagged: a macro is the operator's own words.
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
		// A macro is the operator's own words.
		NoteOperatorMessage(ctx, s.agents, req.TaskID, message)
		result.Queued = true
		return result, nil
	}
	opts := AgentSpawnOptions{AgentKind: macro.Agent, Tier: macro.Tier, Prompt: message, WorkingDir: s.macroWorkingDir(ctx, req)}
	kind := macro.Agent
	if kind == "" || SupportsTaskResume(kind) {
		saved, err := s.agents.GetTaskResumeSession(ctx, req.TaskID, kind)
		if err != nil {
			return MacroStartResult{}, err
		}
		if saved != nil {
			kind, opts.AgentKind = saved.AgentKind, saved.AgentKind
			opts.SessionPolicy, opts.ResumeSessionID, opts.ResumeWorkingDir = "resume", saved.ID, saved.CWD
			opts.WorkingDir = saved.CWD
		}
	}
	if gone := opts.WorkingDir; gone != "" && !s.folderExists(gone) {
		opts.AgentKind, kind = macro.Agent, macro.Agent
		if err := s.freshClone(ctx, req, &opts); err != nil {
			return MacroStartResult{}, err
		}
	}
	if opts.AgentKind == "" {
		defaults, err := s.agents.NewWorkDefaults(ctx)
		if err != nil {
			return MacroStartResult{}, err
		}
		opts.AgentKind, kind = defaults.Agent, defaults.Agent
		if opts.Tier == "" {
			opts.Tier = defaults.Tier
		}
	}
	if err := s.agents.SpawnAgent(ctx, req.TaskID, 0, 0, opts); err != nil {
		return MacroStartResult{}, err
	}
	result.Started, result.AgentKind = true, kind
	return result, nil
}

// freshClone turns a start in a folder that is gone into one in a new job
// clone of the task's repository, on the pull request's branch (else the
// one the task remembers), with a new conversation: an agent tool keeps a
// conversation with the folder it ran in, so it cannot resume elsewhere.
func (s *MacroService) freshClone(ctx context.Context, req MacroStart, opts *AgentSpawnOptions) error {
	gone := opts.WorkingDir
	repo, branch := "", req.Branch
	if req.PR != nil {
		repo = req.PR.Repo
	}
	if task, err := s.settings.db.GetTask(ctx, req.TaskID); err == nil {
		if cc, _ := store.ParseCodeContext(task.CodeContext); cc != nil {
			if repo == "" {
				repo = cc.Repo
			}
			if branch == "" && cc.Repo == repo {
				branch = cc.Branch
			}
		}
	}
	if repo == "" {
		return fmt.Errorf("%w: %s", ErrMacroFolderGone, gone)
	}
	opts.WorkingDir, opts.SessionPolicy, opts.ResumeSessionID, opts.ResumeWorkingDir = "", "new", "", ""
	opts.Repo, opts.Branch, opts.PushedBranch = repo, branch, branch != ""
	on := "a new branch"
	if branch != "" {
		on = branch
	}
	note := fmt.Sprintf("The agent's folder %s was gone, so the %s macro started it in a fresh clone of %s on %s, in a new conversation: the earlier one stayed with that folder.", gone, req.Name, repo, on)
	if _, err := AddTaskNote(ctx, s.settings.db, req.TaskID, note, "macro"); err != nil {
		log.Printf("macros: note on %s: %v", req.TaskID, err)
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// macroWorkingDir is where a macro's new agent starts: the task's
// worktree, else its live job clone of the pull request's repository (the
// task's own repository for a task macro), else the working directory the
// task remembers. "" leaves it to the agent service's default.
func (s *MacroService) macroWorkingDir(ctx context.Context, req MacroStart) string {
	task, err := s.settings.db.GetTask(ctx, req.TaskID)
	if err != nil {
		return ""
	}
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		return *task.WorktreePath
	}
	cc, _ := store.ParseCodeContext(task.CodeContext)
	repo := ""
	if req.PR != nil {
		repo = req.PR.Repo
	} else if cc != nil {
		repo = cc.Repo
	}
	if repo != "" {
		if clone, err := s.settings.db.LiveJobCloneForTask(ctx, req.TaskID, repo); err == nil && clone.Path != "" {
			return clone.Path
		}
	}
	if cc != nil {
		return cc.WorkingDir
	}
	return ""
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

// MacroTarget is what a macro is started on: a task, or one of its pull
// requests, which is the operator's own (Mine) or someone else's.
type MacroTarget struct {
	PR   bool
	Mine bool
}

// MacroSuggestions names the macro suggested for each target.
type MacroSuggestions struct {
	Task     string `json:"task"`
	MyPR     string `json:"my_pr"`
	OthersPR string `json:"others_pr"`
}

// SuggestedMacro is the macro offered first and started by a plain press:
// Plan for a task, Address feedback for the operator's own pull request,
// Review for someone else's.
func SuggestedMacro(target MacroTarget) string {
	switch {
	case !target.PR:
		return "Plan"
	case target.Mine:
		return "Address feedback"
	default:
		return "Review"
	}
}

// MacroSuggestionsFor lists SuggestedMacro for every target, for clients.
func MacroSuggestionsFor() MacroSuggestions {
	return MacroSuggestions{
		Task:     SuggestedMacro(MacroTarget{}),
		MyPR:     SuggestedMacro(MacroTarget{PR: true, Mine: true}),
		OthersPR: SuggestedMacro(MacroTarget{PR: true}),
	}
}
