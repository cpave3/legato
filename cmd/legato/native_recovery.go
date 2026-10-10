package main

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// nativeRecoveryTimeout bounds the startup pass over running agents: each
// endpoint is probed with a short timeout of its own.
const nativeRecoveryTimeout = time.Minute

// recoverNativeEndpoints restores the native connections of agents that were
// already running when Legato started. Their endpoints (Chimera instances,
// Claude Code and Codex bridges) live with the agent sessions, so a restart
// of Legato leaves them up; this binds again the ones whose binding was lost
// and clears bindings to endpoints that are gone. It runs in the background:
// startup does not wait on other processes.
func recoverNativeEndpoints(agents service.AgentService) {
	recoverer, ok := agents.(service.NativeEndpointRecoverer)
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), nativeRecoveryTimeout)
		defer cancel()
		recoverer.RecoverNativeEndpoints(ctx)
	}()
}
