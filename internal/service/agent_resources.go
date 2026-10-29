package service

import (
	"context"
	"errors"
	"fmt"
)

// ResourceAttacher gives a running agent more resources.
type ResourceAttacher interface {
	AttachResources(ctx context.Context, taskID string, resources []Resource) error
	// AttachedResources names the resources the running agent of a task was
	// given, when it started or since.
	AttachedResources(ctx context.Context, taskID string) ([]string, error)
}

// AttachResources gives the running agent of a task more resources, through
// its native endpoint: each path is granted (Chimera widens what its tools may
// touch; the Claude Code and Codex bridges accept the grant and leave access to
// their tool) and the agent is told about them in a note appended to its
// conversation. A busy agent reads the note at its next step; an idle Chimera
// takes it as a message and answers it. An agent with no native endpoint has
// no way to take them while it runs.
// ErrSandboxFixed: a sandboxed agent's mounts were fixed when it started.
var ErrSandboxFixed = errors.New("this agent runs in Legato's sandbox, which cannot see paths added after it started: reload it with the resource added (its conversation resumes), or start an agent without the sandbox")

func (a *agentService) AttachResources(ctx context.Context, taskID string, resources []Resource) error {
	if session, err := a.store.GetAgentSessionByTaskID(ctx, taskID); err == nil && session.Sandboxed {
		return ErrSandboxFixed
	}
	granter, ok := a.chimeraAdapter.(pathGranter)
	if !a.HasChimeraEndpoint(ctx, taskID) || !ok {
		return fmt.Errorf("resources can be added to a running agent only through its native panel: %w", ErrNoNativeEndpoint)
	}
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	usable := usableResources(resources)
	if len(usable) == 0 {
		return invalidResource("none of the paths exists")
	}
	read, write := resourceGrants(usable)
	// Tagged as Legato's own, so the conversation shows it apart from what the
	// person typed and the agent knows who added the paths.
	note := "[legato resources] " + resourcesPrompt(usable)
	if err := granter.GrantPaths(ctx, taskID, read, write, note); err != nil {
		return err
	}
	names := make([]string, 0, len(usable))
	for _, resource := range usable {
		names = append(names, resource.Name)
	}
	if session.ExecutionID == "" {
		return nil
	}
	return a.store.AddExecutionResources(ctx, session.ExecutionID, names)
}

// pathGranter is the native interaction that can grant a running session
// files and folders.
type pathGranter interface {
	GrantPaths(ctx context.Context, taskID string, read, write []string, note string) error
}

// GrantPaths grants a native session files and folders, then appends note to
// its conversation without starting a turn. Every path is tried; the first
// failure is returned.
func (a *ChimeraInteractionAdapter) GrantPaths(ctx context.Context, taskID string, read, write []string, note string) error {
	endpoint, sessionID, err := a.getEndpointAndSession(ctx, taskID)
	if err != nil {
		return err
	}
	client := a.clientFactory(endpoint)
	var errs []error
	grant := func(kind string, paths []string) {
		for _, path := range paths {
			if err := client.AddPath(ctx, sessionID, kind, path); err != nil {
				errs = append(errs, fmt.Errorf("granting %s: %w", path, err))
			}
		}
	}
	grant("read", read)
	grant("write", write)
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return client.PostMessageAppend(ctx, sessionID, note)
}

func (a *agentService) AttachedResources(ctx context.Context, taskID string) ([]string, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if session.ExecutionID == "" {
		return nil, nil
	}
	return a.store.ListExecutionResources(ctx, session.ExecutionID)
}
