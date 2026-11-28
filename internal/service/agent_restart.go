package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrAgentNotRestartable means the running agent cannot be started again by
// itself: a swarm or a workflow owns it, or there is no conversation to
// resume. The agent is left running.
var ErrAgentNotRestartable = errors.New("agent cannot be restarted")

// AgentRestarter starts a running agent again.
type AgentRestarter interface {
	// RestartAgent stops the running agent of a task and starts it again as
	// it was: the same agent tool, directory and resources, resuming its
	// conversation, so it reads its configuration (MCP servers, plugins,
	// hooks) afresh. Everything that can stop the restart is checked before
	// the agent is stopped.
	RestartAgent(ctx context.Context, taskID string, resources []Resource) error
}

// SandboxSwitcher starts a running agent again on the other side of the
// sandbox.
type SandboxSwitcher interface {
	// RestartAgentSwitchingSandbox restarts the agent as RestartAgent does,
	// but outside the sandbox when it ran in it and in it when it ran
	// outside, for when the sandbox blocks something the agent needs. It
	// says whether the agent now runs in the sandbox.
	RestartAgentSwitchingSandbox(ctx context.Context, taskID string, resources []Resource) (sandboxed bool, err error)
}

// The web UI's restart route and the TUI's U both find the switch this way.
var _ SandboxSwitcher = (*agentService)(nil)

func (a *agentService) RestartAgent(ctx context.Context, taskID string, resources []Resource) error {
	_, err := a.restartAgentWith(ctx, taskID, resources, restartChange{})
	return err
}

func (a *agentService) RestartAgentSwitchingSandbox(ctx context.Context, taskID string, resources []Resource) (bool, error) {
	return a.restartAgentWith(ctx, taskID, resources, restartChange{switchSandbox: true})
}

// restartChange is what a restart changes about the agent: nothing, its
// side of the sandbox, or the host network for its containers.
type restartChange struct {
	switchSandbox bool
	hostNetwork   *bool
}

// HandedOffAgents is a worker's side of the agents of cards handed off from
// a main (HandoffWorkerService): such an agent always runs unattended in the
// sandbox, with its branches granted, so a restart goes through the handoff.
type HandedOffAgents interface {
	// HandedOffFrom names the main a card was handed off from while its
	// handoff is open; ok is false for any other card, and an error is
	// one the restart refuses on.
	HandedOffFrom(ctx context.Context, taskID string) (main string, ok bool, err error)
	// RestartHandedOff starts the card's running agent again as the
	// handoff starts it, resuming its conversation: it checks the
	// conversation, then calls r.Stop, then starts it with r's note and
	// resources.
	RestartHandedOff(ctx context.Context, taskID string, r HandedOffRestart) error
}

// HandedOffRestart is a restart of a handed-off card's agent: the stop of
// the running agent, and the first message and resources of the new one.
type HandedOffRestart struct {
	Note      string
	Resources []Resource
	Stop      func(ctx context.Context) error
}

// SetHandedOffAgents makes restarts of handed-off cards' agents go through
// the handoff (a worker's runtime sets it).
func (a *agentService) SetHandedOffAgents(h HandedOffAgents) {
	a.interruptedMu.Lock()
	defer a.interruptedMu.Unlock()
	a.handedOff = h
}

func (a *agentService) handedOffAgents() HandedOffAgents {
	a.interruptedMu.Lock()
	defer a.interruptedMu.Unlock()
	return a.handedOff
}

func (a *agentService) restartAgentWith(ctx context.Context, taskID string, resources []Resource, change restartChange) (bool, error) {
	switchSandbox := change.switchSandbox
	if _, slot := SplitAgentID(taskID); IsNamedSlot(slot) {
		// Legato starts a named slot's agent with what its work needs (a
		// reviewer's read-only, unattended sandbox); a restart would not.
		return false, fmt.Errorf("%w: Legato runs the %s of this task", ErrAgentNotRestartable, slot)
	}
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return false, err
	}
	if handedOff := a.handedOffAgents(); handedOff != nil {
		main, ok, err := handedOff.HandedOffFrom(ctx, taskID)
		if err != nil {
			return false, fmt.Errorf("%w: reading whether this card was handed off: %v", ErrAgentNotRestartable, err)
		}
		if ok {
			return true, a.restartHandedOff(ctx, handedOff, main, session, resources, change)
		}
	}
	// A swarm starts its agents itself. A workflow's agent is reloaded
	// as the workflow's, on the step it is on (LEG-689).
	if owner := swarmOwner(session); owner != "" {
		return false, fmt.Errorf("%w: %s", ErrAgentNotRestartable, owner)
	}
	workflowAgent := session.WorkflowRunID != "" || session.ExecutionKind == store.ExecutionWorkflowAgent
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	// It comes back as it was, in the sandbox or out of it, unless it is
	// asked to switch.
	sandboxed := session.Sandboxed
	if switchSandbox && IsReeveTask(ctx, a.store, taskID) {
		return false, ErrReeveSandboxed
	}
	if switchSandbox {
		sandboxed = !sandboxed
		if sandboxed && a.sandbox == nil {
			return false, errors.New("Legato's sandbox is off, so there is no sandbox to put the agent in")
		}
	}
	opt := AgentSpawnOptions{AgentKind: session.AgentKind, Resources: resources, Ephemeral: task.Ephemeral, Unsandboxed: !sandboxed,
		DockerHostNetwork: a.keptDockerHostNetwork(session.DockerHostNetwork, sandboxed)}
	if change.hostNetwork != nil {
		opt.DockerHostNetwork = *change.hostNetwork
		if opt.DockerHostNetwork {
			// Checked before the agent is stopped, as SpawnAgent checks it.
			if err := a.checkDockerHostNetwork(IsReeveTask(ctx, a.store, taskID), false, opt); err != nil {
				return false, err
			}
		}
	}
	if session.ExecutionID != "" {
		identity, err := a.store.ExecutionAccount(ctx, session.ExecutionID)
		if err != nil {
			return false, err
		}
		opt.AccountIdentity = identity
		if execution, err := a.store.GetExecution(ctx, session.ExecutionID); err == nil {
			opt.WorkingDir = execution.WorkingDir
		}
		// It comes back as the engram it ran as: in the same engram
		// session when its conversation is resumed, booted again with a
		// new one when not.
		if name, engramSession, err := a.store.ExecutionEngram(ctx, session.ExecutionID); err == nil && name != "" {
			opt.Engram, opt.EngramSession = name, engramSession
			opt.EngramHome, _ = a.store.ExecutionEngramHome(ctx, session.ExecutionID)
		}
	}
	if SupportsTaskResume(session.AgentKind) {
		resume, err := a.restartConversation(ctx, task, session)
		if err != nil {
			return false, err
		}
		opt.SessionPolicy = "resume"
		opt.ResumeSessionID = resume.ID
		opt.ResumeWorkingDir = resume.CWD
		check := opt
		if err := a.prepareTaskResume(ctx, taskID, &check); err != nil {
			return false, err
		}
	}
	var open *interruptedAttempt
	if workflowAgent {
		open = a.openAgentStep(ctx, session.ID)
		a.workflowResumeOptions(*session, &opt, open)
	}
	stopCtx := ctx
	if opt.SessionPolicy == "resume" {
		// The engram session goes on in the agent that resumes it.
		stopCtx = withEngramSessionKept(ctx)
	}
	if open != nil {
		// Suspended before the stop, so nothing that sees the agent go
		// (a reconcile, the wait check) fails the step meanwhile.
		attempt := open.attempt
		if err := a.store.SuspendWorkflowStep(ctx, attempt.RunID, attempt.Path, attempt.ID, workflowReloadingReason); err != nil {
			return false, fmt.Errorf("holding step %s of %s for the reload: %w", attempt.Path, attempt.RunID, err)
		}
	}
	if err := a.stopAgent(stopCtx, taskID, false); err != nil {
		if open != nil {
			_ = a.store.ResumeWorkflowStep(ctx, open.attempt.RunID, open.attempt.Path, open.attempt.ID, *session)
		}
		return false, err
	}
	opt.Prompt = restartNote(switchSandbox, sandboxed, opt.SessionPolicy == "resume")
	if change.hostNetwork != nil {
		opt.Prompt = hostNetworkNote(*change.hostNetwork, opt.SessionPolicy == "resume")
	}
	if open != nil {
		opt.Prompt += " " + workflowStepOpenNote(open.attempt.Path, open.attempt.RunID)
		if err := a.continueWorkflowStep(ctx, taskID, opt, open); err != nil {
			a.failInterruptedStep(ctx, open, "its agent could not be started again after a reload: "+reasonOf(err))
			return sandboxed, err
		}
		return sandboxed, nil
	}
	return sandboxed, a.SpawnAgent(ctx, taskID, 0, 0, opt)
}

// workflowReloadingReason is what a step carries for the moment its agent
// is reloaded.
const workflowReloadingReason = "the operator is reloading its agent"

// workflowStepOpenNote tells a reloaded workflow agent its step is open.
func workflowStepOpenNote(stepID, runID string) string {
	return fmt.Sprintf("You are still on workflow step %s of run %s, which is still open: finish its declared outputs, then run the completion command from your assignment.", stepID, runID)
}

// openAgentStep is the agent.run step attempt that waits on a session, nil
// for none.
func (a *agentService) openAgentStep(ctx context.Context, sessionID int) *interruptedAttempt {
	attempts, err := a.store.ListWaitingWorkflowAttemptsByAgentSession(ctx, sessionID)
	if err != nil {
		return nil
	}
	for _, attempt := range attempts {
		if attempt.ExecutorType == "agent.run" {
			return &interruptedAttempt{attempt: attempt}
		}
	}
	return nil
}

// restartHandedOff restarts the agent of a card handed off from main: never
// out of the sandbox or onto the host network, and through the handoff, so
// it comes back unattended in its clone with its branches granted again.
func (a *agentService) restartHandedOff(ctx context.Context, handedOff HandedOffAgents, main string, session *store.AgentSession, resources []Resource, change restartChange) error {
	if change.switchSandbox || change.hostNetwork != nil {
		return fmt.Errorf("%w: this agent was handed off from %s; it always runs unattended in the sandbox", ErrAgentNotRestartable, main)
	}
	taskID := session.TaskID
	stopped := false
	err := handedOff.RestartHandedOff(ctx, taskID, HandedOffRestart{
		Note: restartNote(false, true, true), Resources: resources,
		Stop: func(ctx context.Context) error {
			stopped = true
			return a.stopAgent(ctx, taskID, false)
		},
	})
	if err != nil && !stopped && errors.Is(err, ErrAuthorNotResumable) {
		// Refused before anything stopped: the agent runs on.
		return fmt.Errorf("%w: %w", ErrAgentNotRestartable, err)
	}
	return err
}

// restartNote is the first message a restarted agent gets: what happened to
// its session, since nothing in its conversation says so. Its tag marks it as
// Legato's own message, which the conversation draws apart from what the
// person typed.
func restartNote(switchedSandbox, sandboxed, resumed bool) string {
	conversation := ""
	if resumed {
		conversation = " and your conversation resumed"
	}
	unfinished := " A step that was running when it stopped may not have finished: check before you go on."
	switch {
	case !switchedSandbox:
		return "[legato reloaded] The operator reloaded this session: it was stopped and started again" + conversation +
			", so your tools and configuration (MCP servers, plugins, hooks) were read afresh." + unfinished
	case sandboxed:
		return "[legato sandbox] The operator added the sandbox: this session was stopped and started again inside Legato's sandbox" + conversation +
			". Files, credentials and commands you could reach before may now be hidden or refused." + unfinished
	default:
		return "[legato sandbox] The operator removed the sandbox: this session was stopped and started again outside Legato's sandbox" + conversation +
			". What the sandbox hid or refused before can now be reached, so a step it blocked can be tried again." + unfinished
	}
}

// hostNetworkNote is the first message an agent restarted to change the
// host network for its containers gets.
func hostNetworkNote(on, resumed bool) string {
	conversation := ""
	if resumed {
		conversation = " and your conversation resumed"
	}
	unfinished := " A step that was running when it stopped may not have finished: check before you go on."
	if on {
		return "[legato sandbox] The operator allowed the host network for your containers: this session was stopped and started again" + conversation +
			". Containers and builds may now use network_mode: host (--network host); everything else Docker refused before is still refused. Your containers from before were removed: start them again." + unfinished
	}
	return "[legato sandbox] The operator took the host network away from your containers: this session was stopped and started again" + conversation +
		". network_mode: host (--network host) is refused again. Your containers from before were removed: start them again." + unfinished
}

// restartOwner names what owns an agent that is started by it, not by the
// person, or is empty for an agent of its own.
func restartOwner(session *store.AgentSession) string {
	if session.WorkflowRunID != "" || session.ExecutionKind == store.ExecutionWorkflowAgent {
		return "a workflow runs this agent"
	}
	return swarmOwner(session)
}

// swarmOwner says "a swarm runs this agent" for a swarm's agent, which only
// the swarm starts again, "" for any other.
func swarmOwner(session *store.AgentSession) string {
	if session.Role != "" || (session.ParentTaskID != nil && *session.ParentTaskID != "") ||
		(session.SubtaskID != nil && *session.SubtaskID != "") {
		return "a swarm runs this agent"
	}
	return ""
}

// restartConversation is the conversation the running agent reported, which
// its restart resumes.
func (a *agentService) restartConversation(ctx context.Context, task *store.Task, session *store.AgentSession) (*TaskResumeSession, error) {
	kind := session.AgentKind
	// A Chimera agent's native binding is its Chimera session.
	if kind == "chimera" && session.ChimeraSessionID != nil && *session.ChimeraSessionID != "" {
		return &TaskResumeSession{AgentKind: kind, ID: *session.ChimeraSessionID}, nil
	}
	saved, err := a.GetTaskResumeSession(ctx, task.ID, kind)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, fmt.Errorf("%w: the %s session has not reported its conversation yet, so a restart would lose it", ErrAgentNotRestartable, kind)
	}
	return saved, nil
}
