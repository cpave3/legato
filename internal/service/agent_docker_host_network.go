package service

import (
	"context"
	"errors"
	"fmt"
)

// ErrDockerHostNetwork means an agent was asked to have the host network
// for its containers where it cannot: it is refused, not ignored, so whoever
// asked does not believe the agent has it.
var ErrDockerHostNetwork = errors.New("the host network for an agent's containers")

// checkDockerHostNetwork says why an agent about to start may not have the
// host network for its containers, or nil. It is for an agent the operator
// starts, in the sandbox, with Docker in it.
func (a *agentService) checkDockerHostNetwork(reeve, namedSlot bool, opt AgentSpawnOptions) error {
	refuse := func(why string) error { return fmt.Errorf("%w is not allowed %s", ErrDockerHostNetwork, why) }
	switch {
	case reeve:
		return refuse("for the Reeve")
	case namedSlot:
		return refuse("for an agent Legato starts in a named slot")
	case opt.ParentTaskID != "" || opt.SubtaskID != "" || opt.Role != "":
		return refuse("for a swarm's agents")
	case opt.WorkflowRunID != "":
		return refuse("for a workflow's agents")
	case opt.Unattended || opt.ReadOnlyCheckout:
		return refuse("for an unattended or read-only agent")
	case a.sandbox == nil || opt.Unsandboxed:
		return refuse("outside Legato's sandbox: there is no filtered Docker socket there to allow it")
	case a.sandbox.Docker == nil:
		return refuse("while Docker in the sandbox is off (agents.sandbox.docker.enabled)")
	}
	return nil
}

// DockerHostNetworkSwitcher starts a running agent again with or without
// the host network for its containers.
type DockerHostNetworkSwitcher interface {
	// RestartAgentSettingDockerHostNetwork restarts the agent as
	// RestartAgent does, in a new execution whose containers may (on) or
	// may not use the host's network. The old execution's containers are
	// removed with it, so turning it off leaves none on the host network.
	RestartAgentSettingDockerHostNetwork(ctx context.Context, taskID string, resources []Resource, on bool) error
}

var _ DockerHostNetworkSwitcher = (*agentService)(nil)

func (a *agentService) RestartAgentSettingDockerHostNetwork(ctx context.Context, taskID string, resources []Resource, on bool) error {
	_, err := a.restartAgentWith(ctx, taskID, resources, restartChange{hostNetwork: &on})
	return err
}

// keptDockerHostNetwork is whether an agent started again (a reload or a
// resume) keeps the host network it had: only where it may still have it.
func (a *agentService) keptDockerHostNetwork(had, sandboxed bool) bool {
	return had && sandboxed && a.sandbox != nil && a.sandbox.Docker != nil
}

// CheckDockerHostNetwork says why a spawn of taskID's agent with opt would
// be refused the host network for its containers, or nil; the API asks it
// before an agent that starts in the background is started.
func (a *agentService) CheckDockerHostNetwork(ctx context.Context, taskID string, opt AgentSpawnOptions) error {
	if !opt.DockerHostNetwork {
		return nil
	}
	reeve := taskID != "" && IsReeveTask(ctx, a.store, taskID)
	return a.checkDockerHostNetwork(reeve, IsNamedSlot(opt.Slot), opt)
}
