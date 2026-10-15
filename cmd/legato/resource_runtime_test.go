package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestTheResourceServiceListsTheConfiguredResources(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{Resources: []config.ResourceConfig{{Name: "Vault", Paths: []config.ResourcePathConfig{{Path: "/srv/My Vault", Access: "write"}}}}}

	all, err := wiredResourceService(cfg, db, nil).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !all[0].Configured || all[0].Paths[0].Path != "/srv/My Vault" || all[0].Paths[0].Access != "write" {
		t.Fatalf("resources = %+v", all)
	}
}
