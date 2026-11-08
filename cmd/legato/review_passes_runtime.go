package main

import (
	"os"
	"sort"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

// reviewPassesAdvert is what this worker tells main instances of its review
// passes in /api/meta, nil while they are off. Skills are read at each ask,
// so one installed since start is offered.
func reviewPassesAdvert(cfg *config.Config) func() *service.ReviewPassesAdvert {
	return func() *service.ReviewPassesAdvert {
		rp := cfg.ReviewPasses
		if !rp.Enabled {
			return nil
		}
		advert := &service.ReviewPassesAdvert{
			Defaults: service.ReviewPassSettings{
				Enabled: true, Agent: rp.Reviewer.Agent, Model: rp.Reviewer.Model, Tier: rp.Reviewer.Tier,
				Skill: rp.Reviewer.Skill, MaxPasses: rp.MaxPasses,
			},
			Skills: []string{},
		}
		for _, name := range []string{"chimera", "claude-code", "codex"} {
			adapter := service.ReviewAdapter{Name: name}
			for tier := range cfg.Adapters[name].Tiers {
				adapter.Tiers = append(adapter.Tiers, tier)
			}
			sort.Strings(adapter.Tiers)
			advert.Adapters = append(advert.Adapters, adapter)
		}
		if home, err := os.UserHomeDir(); err == nil {
			advert.Skills = service.AgentSkills(home)
		}
		return advert
	}
}
