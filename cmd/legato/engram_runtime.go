package main

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// processEngrams holds this process's engram service for each store.
var processEngrams sync.Map

// engramsFor is this process's engram service over db: engram.enabled and
// engram.command read from the effective settings on every call.
func engramsFor(db *store.Store) *service.EngramService {
	if s, ok := processEngrams.Load(db); ok {
		return s.(*service.EngramService)
	}
	s := service.NewEngramService(service.EngramOptions{
		Config: func(ctx context.Context) (config.EngramConfig, error) {
			cfg, err := settingsFor(db).Effective(ctx)
			if err != nil {
				return config.EngramConfig{}, err
			}
			return cfg.Engram, nil
		},
		Runner: service.EngramRunner(os.Getenv),
		Store:  db,
	})
	actual, loaded := processEngrams.LoadOrStore(db, s)
	if !loaded {
		// The startup probe: with engram.enabled on, a failure is logged.
		go func() {
			st := s.Status(context.Background())
			if st.Enabled {
				log.Printf("engram: %s %s, store %s, %d engrams", st.Binary, st.Version, st.Store, len(st.Engrams))
			}
		}()
	}
	return actual.(*service.EngramService)
}

// giveAgentsEngrams lets an agent service start agents as engrams.
func giveAgentsEngrams(agents service.AgentService, db *store.Store) {
	if agents == nil {
		return
	}
	if setter, ok := agents.(interface{ SetEngrams(service.AgentEngrams) }); ok {
		setter.SetEngrams(engramsFor(db))
	}
}
