package main

import (
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func openStore(cfg *config.Config) (*store.Store, error) {
	database, err := config.ResolveDatabase(cfg)
	if err != nil {
		return nil, err
	}
	return store.Open(store.Options{Driver: database.Driver, Path: database.Path})
}
