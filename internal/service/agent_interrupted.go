package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrAgentNotResumable means an interrupted agent cannot be started again
// from where it was: a swarm or a workflow owns it, it has no conversation
// to resume, or its folder is gone. Nothing is started.
var ErrAgentNotResumable = errors.New("interrupted agent cannot be resumed")

// InterruptedNote is the first message a resumed interrupted agent gets: it
// may have stopped in the middle of a step.
const InterruptedNote = "Legato and this session were interrupted (the machine or tmux went down) while you were working, and your conversation has been resumed. Your last step may not have finished: check the state of what you were doing before you go on."

// InterruptedAgent is an agent that was running when Legato last stopped and
// whose tmux session was gone when it started again.
type InterruptedAgent struct {
	TaskID        string
	Title         string
	AgentKind     string
	WorkingDir    string
	Engram        string
	Sandboxed     bool
	Ephemeral     bool
	Resources     []string
	InterruptedAt time.Time
	// Resumable says its conversation can be resumed where it was.
	Resumable bool
	// CanStartNew says a new agent can start on its task in its place, when
	// it cannot be resumed (it never reported a conversation).
	CanStartNew bool
	// Reason says why it cannot be resumed; empty when it can.
	Reason string
}

// InterruptedResult is how resuming one interrupted agent went.
type InterruptedResult struct {
	TaskID string
	Err    error
}

// InterruptedAgents finds the agents a crash took and starts them again.
type InterruptedAgents interface {
	// SweepInterruptedAgents marks the running agents whose tmux session is
	// gone as interrupted. It runs once, when Legato starts, before
	// anything else reconciles agents: later, a gone session is an agent
	// that ended. It says how many it marked.
	SweepInterruptedAgents(ctx context.Context) (int, error)
	ListInterruptedAgents(ctx context.Context) ([]InterruptedAgent, error)
	// ResumeInterruptedAgent starts the interrupted agent of a task again
	// as it was (tool, folder, resources, sandbox, engram), resuming its
	// conversation and telling it it was interrupted. With fresh, it
	// starts a new conversation instead, for an agent that never reported
	// one.
	ResumeInterruptedAgent(ctx context.Context, taskID string, fresh bool) error
	// ResumeAllInterruptedAgents resumes every interrupted agent that can
	// be resumed, and says how each went.
	ResumeAllInterruptedAgents(ctx context.Context) []InterruptedResult
	// DismissInterruptedAgent marks it ended, and ends its engram session.
	DismissInterruptedAgent(ctx context.Context, taskID string) error
}

var _ InterruptedAgents = (*agentService)(nil)

// InterruptedResumeRoute resumes the interrupted agent of a task that
// another service starts its own way (the Reeve, a handed-off task). It
// says whether it handled the task.
type InterruptedResumeRoute func(ctx context.Context, taskID string) (handled bool, err error)

// ResourceResolver finds resources by name.
type ResourceResolver interface {
	Resolve(ctx context.Context, names []string) ([]Resource, error)
}

// AddInterruptedResumeRoute adds a route that resumes some interrupted
// agents in place of the agent service.
func (a *agentService) AddInterruptedResumeRoute(route InterruptedResumeRoute) {
	a.interruptedMu.Lock()
	defer a.interruptedMu.Unlock()
	a.interruptedRoutes = append(a.interruptedRoutes, route)
}

// AddInterruptedListener adds a listener told each time the interrupted
// agents change: marked, resumed or dismissed.
func (a *agentService) AddInterruptedListener(listener func()) {
	a.interruptedMu.Lock()
	defer a.interruptedMu.Unlock()
	a.interruptedListeners = append(a.interruptedListeners, listener)
}

func (a *agentService) interruptedChanged() {
	a.interruptedMu.Lock()
	listeners := append([]func(){}, a.interruptedListeners...)
	a.interruptedMu.Unlock()
	for _, listener := range listeners {
		listener()
	}
}

// SetResourceResolver gives the agent service the resources an interrupted
// agent had, by name.
func (a *agentService) SetResourceResolver(resolver ResourceResolver) {
	a.interruptedMu.Lock()
	defer a.interruptedMu.Unlock()
	a.resourceResolver = resolver
}

func (a *agentService) SweepInterruptedAgents(ctx context.Context) (int, error) {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return 0, err
	}
	liveSessions, err := a.tmux.ListSessions()
	if err != nil {
		return 0, err
	}
	live := make(map[string]bool, len(liveSessions))
	for _, name := range liveSessions {
		live[name] = true
	}
	count := 0
	for _, s := range sessions {
		if live[s.TmuxSession] {
			continue
		}
		// A named slot's agent (a reviewer) belongs to the work that
		// started it, which hears that it died.
		status := store.AgentSessionInterrupted
		if IsNamedSlot(s.Slot) {
			status = "dead"
		}
		if err := a.endGoneSession(ctx, s, status); err != nil {
			return count, err
		}
		if status == store.AgentSessionInterrupted {
			count++
		}
	}
	if count > 0 {
		defer a.interruptedChanged()
		log.Printf("agents: %d agent(s) were interrupted while Legato was down", count)
	}
	return count, nil
}

func (a *agentService) ListInterruptedAgents(ctx context.Context) ([]InterruptedAgent, error) {
	sessions, err := a.store.ListInterruptedAgentSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]InterruptedAgent, 0, len(sessions))
	for _, s := range sessions {
		plan := a.planInterruptedResume(ctx, s)
		agent := InterruptedAgent{
			TaskID: s.TaskID, AgentKind: s.AgentKind, Engram: s.Engram, Sandboxed: s.Sandboxed,
			WorkingDir: plan.workingDir, Resources: plan.resources,
			Resumable: plan.err == nil, CanStartNew: plan.canStartNew,
		}
		if plan.err != nil {
			agent.Reason = reasonOf(plan.err)
		}
		if task, err := a.store.GetTask(ctx, s.TaskID); err == nil {
			agent.Title, agent.Ephemeral = task.Title, task.Ephemeral
		}
		if s.EndedAt != nil {
			agent.InterruptedAt, _ = time.Parse("2006-01-02 15:04:05", *s.EndedAt)
		}
		out = append(out, agent)
	}
	return out, nil
}

func (a *agentService) ResumeInterruptedAgent(ctx context.Context, taskID string, fresh bool) error {
	session, err := a.store.GetInterruptedAgentSession(ctx, taskID)
	if err != nil {
		return err
	}
	a.interruptedMu.Lock()
	routes := append([]InterruptedResumeRoute(nil), a.interruptedRoutes...)
	a.interruptedMu.Unlock()
	if !fresh {
		for _, route := range routes {
			if handled, err := route(ctx, taskID); handled {
				if err != nil {
					return err
				}
				_ = a.store.EndInterruptedAgentSession(ctx, taskID, store.SlotMain)
				a.interruptedChanged()
				return nil
			}
		}
	}
	plan := a.planInterruptedResume(ctx, *session)
	opt := plan.opt
	switch {
	case plan.err == nil && !fresh:
	case fresh && plan.canStartNew:
		opt.SessionPolicy, opt.ResumeSessionID, opt.ResumeWorkingDir = "", "", ""
		opt.EngramSession = ""
	case fresh:
		return fmt.Errorf("%w: %s", ErrAgentNotResumable, reasonOf(plan.err))
	default:
		return plan.err
	}
	if !fresh {
		opt.Prompt = InterruptedNote
	}
	opt.Resources = a.resolveResources(ctx, plan.resources)
	// The new agent replaces the interrupted one in the task's slot.
	if err := a.SpawnAgent(ctx, taskID, 0, 0, opt); err != nil {
		return err
	}
	a.interruptedChanged()
	return nil
}

func (a *agentService) ResumeAllInterruptedAgents(ctx context.Context) []InterruptedResult {
	list, err := a.ListInterruptedAgents(ctx)
	if err != nil {
		return []InterruptedResult{{Err: err}}
	}
	results := make([]InterruptedResult, 0, len(list))
	for _, agent := range list {
		if !agent.Resumable {
			continue
		}
		results = append(results, InterruptedResult{TaskID: agent.TaskID, Err: a.ResumeInterruptedAgent(ctx, agent.TaskID, false)})
	}
	return results
}

func (a *agentService) DismissInterruptedAgent(ctx context.Context, taskID string) error {
	session, err := a.store.GetInterruptedAgentSession(ctx, taskID)
	if err != nil {
		return err
	}
	if err := a.store.EndInterruptedAgentSession(ctx, taskID, store.SlotMain); err != nil {
		return err
	}
	if session.ExecutionID != "" {
		a.endEngramSession(ctx, session.ExecutionID)
	}
	a.interruptedChanged()
	return nil
}

// interruptedResume is how an interrupted agent would start again: the
// options, or why it cannot.
type interruptedResume struct {
	opt         AgentSpawnOptions
	workingDir  string
	resources   []string
	err         error
	canStartNew bool
}

func (a *agentService) planInterruptedResume(ctx context.Context, s store.AgentSession) interruptedResume {
	var plan interruptedResume
	opt := AgentSpawnOptions{AgentKind: s.AgentKind, Unsandboxed: !s.Sandboxed}
	if s.ExecutionID != "" {
		if execution, err := a.store.GetExecution(ctx, s.ExecutionID); err == nil {
			opt.WorkingDir = execution.WorkingDir
		}
		if name, engramSession, err := a.store.ExecutionEngram(ctx, s.ExecutionID); err == nil && name != "" {
			opt.Engram, opt.EngramSession = name, engramSession
		}
		plan.resources, _ = a.store.ListExecutionResources(ctx, s.ExecutionID)
	}
	plan.workingDir = opt.WorkingDir
	plan.opt = opt
	if owner := restartOwner(&s); owner != "" {
		plan.err = fmt.Errorf("%w: %s; it starts its agents itself", ErrAgentNotResumable, owner)
		return plan
	}
	task, err := a.store.GetTask(ctx, s.TaskID)
	if err != nil {
		plan.err = fmt.Errorf("%w: its task is gone", ErrAgentNotResumable)
		return plan
	}
	plan.opt.Ephemeral = task.Ephemeral
	if opt.WorkingDir != "" {
		if info, err := os.Stat(opt.WorkingDir); err != nil || !info.IsDir() {
			plan.err = fmt.Errorf("%w: its folder %s is gone", ErrAgentNotResumable, opt.WorkingDir)
			return plan
		}
	}
	if !SupportsTaskResume(s.AgentKind) {
		plan.err = fmt.Errorf("%w: %s cannot resume a conversation", ErrAgentNotResumable, kindOrDefault(s.AgentKind))
		plan.canStartNew = true
		return plan
	}
	resume, err := a.restartConversation(ctx, task, &s)
	if err != nil {
		plan.err = fmt.Errorf("%w: it never reported its conversation, so there is nothing to resume", ErrAgentNotResumable)
		plan.canStartNew = true
		return plan
	}
	plan.opt.SessionPolicy = "resume"
	plan.opt.ResumeSessionID = resume.ID
	plan.opt.ResumeWorkingDir = resume.CWD
	check := plan.opt
	if err := a.prepareTaskResume(ctx, s.TaskID, &check); err != nil {
		plan.err = fmt.Errorf("%w: %v", ErrAgentNotResumable, err)
	}
	return plan
}

func (a *agentService) resolveResources(ctx context.Context, names []string) []Resource {
	a.interruptedMu.Lock()
	resolver := a.resourceResolver
	a.interruptedMu.Unlock()
	if resolver == nil {
		return nil
	}
	var resources []Resource
	// One removed since it was given is left out rather than stopping the
	// resume.
	for _, name := range names {
		if found, err := resolver.Resolve(ctx, []string{name}); err == nil {
			resources = append(resources, found...)
		}
	}
	return resources
}

// reasonOf is an error's text without the sentinel's prefix.
func reasonOf(err error) string {
	text := err.Error()
	prefix := ErrAgentNotResumable.Error() + ": "
	if len(text) > len(prefix) && text[:len(prefix)] == prefix {
		return text[len(prefix):]
	}
	return text
}

func kindOrDefault(kind string) string {
	if kind == "" {
		return "this agent tool"
	}
	return kind
}
