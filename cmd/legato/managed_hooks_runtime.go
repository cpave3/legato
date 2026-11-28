package main

import (
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// managedHooksDir is the managed-hooks folder: managed_hooks.dir, else
// ~/.legato/managed-hooks (or $LEGATO_HOME/managed-hooks). Empty when there
// is no home folder to put it in.
func managedHooksDir(cfg *config.Config) string {
	return legatoHomeFolder(cfg, "managed hooks", config.ResolveManagedHooksDir)
}

// managedHookService is Legato's managed hooks, which the web server lists
// in Settings › Managed hooks and every agent service asks at each spawn
// (the HookSource), recording each execution's hooks. Claude Code agents
// get theirs in a legato-hooks plugin (LEG-696); other tools skip them.
func managedHookService(cfg *config.Config, db *store.Store) *service.ManagedHookService {
	return service.NewManagedHookService(managedHooksDir(cfg), db)
}
