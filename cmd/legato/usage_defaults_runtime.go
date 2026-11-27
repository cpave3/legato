package main

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// usageSelection reads the committed defaults for new work in a repository
// ("" for none) under the live usage settings.
func usageSelection(ctx context.Context, db *store.Store, repo string) (service.UsageSelection, error) {
	live, err := settingsFor(db).Effective(ctx)
	if err != nil {
		return service.UsageSelection{}, err
	}
	return service.UsageDefaultsFor(ctx, db, live.Usage, repo)
}

func usageNewWorkDefaults(db *store.Store) func(context.Context, string) (service.NewWorkDefaults, error) {
	return func(ctx context.Context, repo string) (service.NewWorkDefaults, error) {
		sel, err := usageSelection(ctx, db, repo)
		p := sel.Profile
		return service.NewWorkDefaults{Agent: p.Agent, Tier: p.Tier, Account: p.Account}, err
	}
}

func usageReviewerDefaults(ctx context.Context, db *store.Store, repo string, defaults service.ReviewPassSettings) (service.ReviewPassSettings, error) {
	sel, err := usageSelection(ctx, db, repo)
	if err != nil {
		return defaults, err
	}
	if p := sel.Profile; p.Reviewer != nil {
		defaults.Account = p.Reviewer.Account
		defaults.Agent = p.Reviewer.Agent
		defaults.Tier = p.Reviewer.Tier
		defaults.Model = ""
	}
	return defaults, nil
}

// withUsageNewWork gives an agent service's options what usage defaults for
// new work need: the defaults of the spawn's repository, and an inspector
// for the repository of a working directory the operator chose. Every
// runtime (the TUI, serve and commands) builds its agent service with it.
func withUsageNewWork(opts service.AgentServiceOptions, db *store.Store) service.AgentServiceOptions {
	opts.NewWorkDefaults = usageNewWorkDefaults(db)
	opts.NewWorkInspector = checkoutInspector()
	return opts
}
