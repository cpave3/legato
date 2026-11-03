package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
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

// giveAgentsPrimaryDirs tells an agent service where each repository's
// main checkout is (its primary_dir), which a fresh clone's setup copies
// and links files from.
func giveAgentsPrimaryDirs(agents service.AgentService, presets *service.RepositoryPresetService) {
	if agents == nil || presets == nil {
		return
	}
	if setter, ok := agents.(interface{ SetPrimaryDirs(func(string) string) }); ok {
		setter.SetPrimaryDirs(presets.PrimaryDir)
	}
}

// giveRepositories gives the repository cache this process's GitHub
// credential. While this process serves the broker, so one Legato does it,
// it copies every repository offered once at the start, so an agent's
// first clone rarely waits for one, and sweeps idle job clones.
func (rt *approvalRuntime) giveRepositories(repos *service.RepositoryService, presets *service.RepositoryPresetService) {
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
			repos.Warm(ctx, offeredRepositories(ctx, presets))
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
		// One added in the web UI is copied at once, before an agent needs it.
		presets.SetOnAdded(func(repo string) { go repos.Warm(context.Background(), []string{repo}) })
	}
	return presets
}

// offeredRepositories are the GitHub repositories Legato offers.
func offeredRepositories(ctx context.Context, presets *service.RepositoryPresetService) []string {
	if presets == nil {
		return nil
	}
	listed, err := presets.List(ctx)
	if err != nil {
		log.Printf("repositories: listing them to copy: %v", err)
	}
	var repos []string
	for _, p := range listed {
		if p.Repo != "" {
			repos = append(repos, p.Repo)
		}
	}
	return repos
}

// wireTaskStacks records the stacks of branches agents build with Staccato
// in job clones: when an agent ends and before a clone is removed.
func wireTaskStacks(db *store.Store, bus *events.Bus, repos *service.RepositoryService) *service.TaskStackService {
	stacks := service.NewTaskStackService(db, nil)
	repos.SetBeforeRemove(stacks.Record)
	stacks.StartEventLoop(context.Background(), bus)
	return stacks
}
