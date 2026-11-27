package main

import (
	"context"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func usageNewWorkDefaults(db *store.Store) func(context.Context) (service.NewWorkDefaults, error) {
	return func(ctx context.Context) (service.NewWorkDefaults, error) {
		p, err := service.UsageDefaults(ctx, db)
		return service.NewWorkDefaults{Agent: p.Agent, Tier: p.Tier, Account: p.Account}, err
	}
}

func usageReviewerDefaults(ctx context.Context, db *store.Store, defaults service.ReviewPassSettings) (service.ReviewPassSettings, error) {
	p, err := service.UsageDefaults(ctx, db)
	if err != nil {
		return defaults, err
	}
	if p.Reviewer != nil {
		defaults.Account = p.Reviewer.Account
		defaults.Agent = p.Reviewer.Agent
		defaults.Tier = p.Reviewer.Tier
		defaults.Model = ""
	}
	return defaults, nil
}
