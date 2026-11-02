package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// jobCloneSweepEvery is how often the process that serves the broker
// removes job clones left idle for repository_cache.keep_idle.
const jobCloneSweepEvery = 5 * time.Minute

// newRepositoryService builds Legato's repository cache. Its git runs this
// binary as the askpass helper that hands it a token. It has no credential
// until the credential channels are built (SetTokens).
func newRepositoryService(cfg *config.Config, db *store.Store) *service.RepositoryService {
	return service.NewRepositoryService(service.RepositoryServiceOptions{
		DataDir:  config.ResolveDataDir(cfg),
		MaxAge:   cfg.RepositoryCache.MaxAgeDuration(),
		KeepIdle: jobCloneKeepIdle(cfg),
		Git:      gitrepo.Runner{AskPass: legatoBinary()},
		Store:    db,
	})
}

// jobCloneKeepIdle is repository_cache.keep_idle for the repository
// service: 0 in the configuration keeps idle clones forever.
func jobCloneKeepIdle(cfg *config.Config) time.Duration {
	d, forever := cfg.RepositoryCache.KeepIdleDuration()
	if forever {
		return service.KeepIdleForever
	}
	return d
}

// giveAgentsRepositories lets an agent service start agents in clones of a
// repository.
func giveAgentsRepositories(agents service.AgentService, repos *service.RepositoryService) {
	if agents == nil || repos == nil {
		return
	}
	if setter, ok := agents.(interface{ SetRepositories(service.JobCloner) }); ok {
		setter.SetRepositories(repos)
	}
}

// giveRepositories gives the repository cache this process's GitHub
// credential, and sweeps idle job clones while this process serves the
// broker, so one Legato does it.
func (rt *approvalRuntime) giveRepositories(repos *service.RepositoryService) {
	if rt == nil || repos == nil {
		return
	}
	if rt.channels != nil {
		repos.SetTokens(rt.channels)
	}
	if rt.brokerOwner == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sweep := func(ctx context.Context) {
		if err := repos.SweepJobClones(ctx); err != nil {
			log.Printf("repositories: sweeping job clones: %v", err)
		}
	}
	go func() {
		if rt.brokerOwner() {
			sweep(ctx)
		}
		whileBrokerOwner(ctx, rt.brokerOwner, jobCloneSweepEvery, sweep)
	}()
	stop := rt.stop
	rt.stop = func() {
		cancel()
		stop()
	}
}

// newRepositoryPresetService is every repository Legato offers: the
// configuration's, then those added in the web UI. It says which of them
// the repository cache cannot clone, and why.
func newRepositoryPresetService(cfg *config.Config, db *store.Store, repos *service.RepositoryService) *service.RepositoryPresetService {
	configured := make([]service.RepositoryPreset, 0, len(cfg.Repositories))
	for _, r := range cfg.Repositories {
		configured = append(configured, service.RepositoryPreset{Name: r.Name, Repo: r.Repo, PrimaryDir: r.PrimaryDir, BaseBranch: r.BaseBranch})
	}
	home, _ := os.UserHomeDir()
	presets := service.NewRepositoryPresetService(db, configured, home)
	if repos != nil {
		presets.SetCloneChecker(repos)
	}
	return presets
}
