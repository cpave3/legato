package main

import (
	"context"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// scopeDir is where Scope keeps pins' screenshots: <data_dir>/scope.
func scopeDir(cfg *config.Config) string {
	return filepath.Join(resolveDataDir(cfg), "scope")
}

// startScope builds the Scope pool both host runtimes share: the pins the
// operator sends from live pages and the origin subscriptions that push
// them to agents. It follows agents starting and ending until ctx ends,
// and ends an interrupted agent's subscriptions when it is dismissed.
// agents may be nil (no tmux); then nothing is pushed and batches wait.
func startScope(ctx context.Context, cfg *config.Config, db *store.Store, bus *events.Bus, agents service.AgentService) (*service.ScopeService, func()) {
	opts := service.ScopeServiceOptions{Store: db, Dir: scopeDir(cfg), Bus: bus}
	if messenger, ok := agents.(service.ScopeMessenger); ok && agents != nil {
		opts.Messenger = messenger
	}
	scope := service.NewScopeService(opts)
	stop := scope.StartEventLoop(ctx)
	if svc, ok := agents.(interruptedAgentService); ok {
		svc.AddInterruptedListener(func() { scope.PruneEnded(context.Background()) })
	}
	return scope, stop
}
