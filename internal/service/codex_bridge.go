package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/codexbridge"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// codexAgentKind is the adapter name of Codex agents.
const codexAgentKind = "codex"

// discoverCodexBridge binds a Codex agent to the bridge its session is
// running.
//
// A bridge binds itself once its terminal has attached, so this is the way
// back after the binding was lost while the session lived on: a downgrade to
// tmux, a snapshot failure that invalidated it, or a Legato restart. The
// bridge speaks the native protocol Chimera endpoints speak and is bound in
// the same columns, which is why the rest of Legato cannot tell them apart.
func (a *agentService) discoverCodexBridge(ctx context.Context, taskID string, agentSessionID int) error {
	lockfile, err := codexbridge.ReadLockfile(ipc.SocketDir(), taskID)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrChimeraDiscoveryNotFound
	}
	if err != nil {
		return fmt.Errorf("reading the Codex bridge lockfile: %w", err)
	}
	if !isLoopbackHTTPURL(lockfile.URL) || !nativeEndpointServes(ctx, lockfile.URL, lockfile.SessionID, taskID) {
		return ErrChimeraDiscoveryNotFound
	}
	if err := a.store.UpdateChimeraDiscoveryForSession(ctx, agentSessionID, lockfile.SessionID, lockfile.URL); err != nil {
		return fmt.Errorf("persisting Codex bridge discovery: %w", err)
	}
	if a.bus != nil {
		a.bus.PublishAgentChimeraDiscovered(taskID)
	}
	return nil
}

// NativeEndpointRecoverer restores the native connections of running agents.
type NativeEndpointRecoverer interface {
	// RecoverNativeEndpoints returns how many agents it bound again.
	RecoverNativeEndpoints(ctx context.Context) int
}

// RecoverNativeEndpoints brings running agents' native connections back to
// what is actually alive. It is run when Legato starts.
//
// A native endpoint (a Chimera instance, a Claude Code or Codex bridge) lives
// with its agent session, not with Legato, and its binding is stored. Most
// connections therefore survive a restart untouched. What does not is a
// binding that was lost while the endpoint lived on (a snapshot that failed
// at the wrong moment, a launch-time discovery the old process never
// finished) and a binding to an endpoint that has since gone:
//
//   - a binding whose endpoint still answers is left exactly as it is;
//   - an agent with no usable binding is bound to the endpoint its session
//     advertises, when one is alive and serves that agent;
//   - a binding to an endpoint that is gone, with nothing to replace it, is
//     cleared, so the agent is shown through its terminal.
//
// An agent downgraded to its terminal on purpose is bound again here if its
// endpoint is still running: Legato keeps no record of the choice.
func (a *agentService) RecoverNativeEndpoints(ctx context.Context) int {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return 0
	}
	recovered := 0
	for _, session := range sessions {
		if session.Status != "running" {
			continue
		}
		bound := session.ChimeraEndpoint != nil && session.ChimeraSessionID != nil
		if bound && nativeEndpointAlive(ctx, *session.ChimeraEndpoint, *session.ChimeraSessionID) {
			continue
		}
		err := a.DiscoverChimera(ctx, session.TaskID)
		switch {
		case err == nil:
			recovered++
			log.Printf("native endpoint recovered for %s (%s)", session.TaskID, session.AgentKind)
		case bound:
			if _, err := a.store.ClearChimeraDiscoveryForSession(ctx, session.ID, *session.ChimeraSessionID, *session.ChimeraEndpoint); err == nil && a.bus != nil {
				a.bus.PublishAgentChimeraDiscovered(session.TaskID)
			}
		}
	}
	return recovered
}

// nativeEndpointAlive reports whether endpoint still serves the session it
// was bound for. The session's name is not checked: the binding was verified
// when it was made, and an ephemeral session has a name of its own.
func nativeEndpointAlive(ctx context.Context, endpoint, sessionID string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, chimeraDiscoveryRequestTimeout)
	defer cancel()
	client := chimera.NewClientWithHTTPClient(endpoint, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}})
	_, err := client.GetSession(requestCtx, sessionID)
	return err == nil
}
