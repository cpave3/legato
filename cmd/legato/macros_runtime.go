package main

import (
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// macrosFor is the macro service over db: the macros read at each use,
// and started on tasks through agents when there is an agent service.
func macrosFor(db *store.Store, agents service.AgentService) *service.MacroService {
	macros := service.NewMacroService(settingsFor(db))
	if starter, ok := agents.(service.MacroAgents); ok {
		macros.SetAgents(starter)
	}
	return macros
}
