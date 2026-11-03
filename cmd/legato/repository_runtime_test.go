package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestJobClonesAreKeptIdleAsConfigured(t *testing.T) {
	for _, tc := range []struct {
		keepIdle string
		want     time.Duration
	}{{"", 7 * 24 * time.Hour}, {"2d", 48 * time.Hour}, {"0", service.KeepIdleForever}} {
		cfg := &config.Config{RepositoryCache: config.RepositoryCacheConfig{KeepIdle: tc.keepIdle}}
		if got := jobCloneKeepIdle(cfg); got != tc.want {
			t.Errorf("keep_idle %q = %v, want %v", tc.keepIdle, got, tc.want)
		}
	}
}

func TestTheRepositoriesCopiedAtTheStartAreTheGitHubOnesOffered(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := &config.Config{Repositories: []config.RepositoryConfig{
		{Name: "Legato", Repo: "cpave3/legato"},
		{Name: "Notes", PrimaryDir: t.TempDir()},
		{Name: "Chimera", Repo: "cpave3/chimera"},
	}}
	got := offeredRepositories(context.Background(), newRepositoryPresetService(cfg, db, nil))
	if strings.Join(got, ",") != "cpave3/legato,cpave3/chimera" {
		t.Fatalf("copied %v", got)
	}
}

type primaryDirsAgents struct {
	service.AgentService
	primaryDir func(string) string
}

func (a *primaryDirsAgents) SetPrimaryDirs(f func(string) string) { a.primaryDir = f }

// A fresh clone's setup takes files from the repository's primary_dir.
func TestAgentsAreToldWhereEachRepositorysMainCheckoutIs(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	checkout := t.TempDir()
	cfg := &config.Config{Repositories: []config.RepositoryConfig{{Name: "Widget", Repo: "acme/widget", PrimaryDir: checkout}}}
	agents := &primaryDirsAgents{}

	giveAgentsPrimaryDirs(agents, newRepositoryPresetService(cfg, db, nil))

	if agents.primaryDir == nil || agents.primaryDir("acme/widget") != checkout {
		t.Fatal("the agents were not told the main checkout of acme/widget")
	}
}
