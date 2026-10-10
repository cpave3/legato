package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// claudeCodeAgentKind is the adapter name of Claude Code agents.
const claudeCodeAgentKind = "claude-code"

// discoverClaudeBridge binds a Claude Code agent to the bridge its session is
// running.
//
// A bridge binds itself when it starts, so this is the way back after the
// binding was cleared while the session lived on: a downgrade to tmux, or a
// snapshot failure that invalidated it. The bridge speaks the native protocol
// Chimera endpoints speak and is bound in the same columns, which is why the
// rest of Legato cannot tell the two apart.
func (a *agentService) discoverClaudeBridge(ctx context.Context, taskID string, agentSessionID int) error {
	lockfile, err := claudebridge.ReadLockfile(ipc.SocketDir(), taskID)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrChimeraDiscoveryNotFound
	}
	if err != nil {
		return fmt.Errorf("reading the Claude Code bridge lockfile: %w", err)
	}
	if !isLoopbackHTTPURL(lockfile.URL) || !nativeEndpointServes(ctx, lockfile.URL, lockfile.SessionID, taskID) {
		return ErrChimeraDiscoveryNotFound
	}
	if err := a.store.UpdateChimeraDiscoveryForSession(ctx, agentSessionID, lockfile.SessionID, lockfile.URL); err != nil {
		return fmt.Errorf("persisting Claude Code bridge discovery: %w", err)
	}
	if a.bus != nil {
		a.bus.PublishAgentChimeraDiscovered(taskID)
	}
	return nil
}
