package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
	"github.com/cpave3/legato/internal/service"
)

// skillsDir is the skills folder: skills.dir, else ~/.legato/skills (or
// $LEGATO_HOME/skills). Empty when there is no home folder to put it in.
func skillsDir(cfg *config.Config) string {
	if cfg.Skills.Dir != "" {
		return cfg.Skills.Dir
	}
	home, err := swarm.LegatoHome()
	if err != nil {
		log.Printf("skills: %v", err)
		return ""
	}
	return config.ResolveSkillsDir(cfg, home)
}

// skillService is Legato's skills, which every agent service gives its
// agents and the web server lists in Settings › Skills.
func skillService(cfg *config.Config, db *store.Store) *service.SkillService {
	return service.NewSkillService(skillsDir(cfg), db)
}

// capabilityFolders is where each execution's skills are copied:
// <data_dir>/capabilities/<execution>.
func capabilityFolders(cfg *config.Config) service.CapabilityFolders {
	return service.CapabilityFolders{Root: filepath.Join(config.ResolveDataDir(cfg), "capabilities")}
}

// agentHasSkill says whether agents have a skill of a name: one of
// Legato's managed skills that is on and usable, or one the agent tools keep
// in the home folder. Asked at each spawn, so a skill added shows at once.
func agentHasSkill(cfg *config.Config, db *store.Store) func(name string) bool {
	skills := skillService(cfg, db)
	return func(name string) bool {
		if list, err := skills.List(context.Background()); err == nil {
			for _, s := range list {
				if s.Name == name && s.Enabled && s.Err == "" {
					return true
				}
			}
		}
		home, err := os.UserHomeDir()
		return err == nil && slices.Contains(service.AgentSkills(home), name)
	}
}
