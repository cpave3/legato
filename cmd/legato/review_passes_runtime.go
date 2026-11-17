package main

import (
	"context"
	"os"
	"slices"
	"sort"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reviewPassesAdvert is what this worker tells its paired mains of its
// review passes (the capabilities advert; /api/meta gives it without the
// skill names), nil while they are off. Skills are read at each ask,
// so one installed since start is offered.
func reviewPassesAdvert(cfg *config.Config, db *store.Store) func() *service.ReviewPassesAdvert {
	var skills *service.SkillService
	if db != nil {
		skills = skillService(cfg, db)
	}
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
		// The worker's own Legato-managed skills that are on: a reviewer
		// gets them too (Claude Code as legato-skills:<name>).
		if skills != nil {
			if usable, err := skills.Usable(context.Background()); err == nil {
				for _, name := range usable {
					if !slices.Contains(advert.Skills, name) {
						advert.Skills = append(advert.Skills, name)
						advert.ManagedSkills = append(advert.ManagedSkills, name)
					}
				}
				sort.Strings(advert.Skills)
			}
		}
		return advert
	}
}
