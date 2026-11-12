package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResourceConfig is a named set of files and folders an agent can be given:
// an Obsidian vault, a folder of specs. It is attached when an agent starts or
// while it runs.
type ResourceConfig struct {
	Name  string               `yaml:"name"`
	Paths []ResourcePathConfig `yaml:"paths"`
}

// ResourcePathConfig is one file or folder of a resource. Access is "read"
// (the default) or "write".
type ResourcePathConfig struct {
	Path   string `yaml:"path"`
	Access string `yaml:"access"`
}

// normalizeResources expands a leading ~ in each path, fills in read access,
// and rejects a resource an agent could not be given.
func normalizeResources(cfg *Config) error {
	seen := map[string]bool{}
	home, _ := os.UserHomeDir()
	for i := range cfg.Resources {
		resource := &cfg.Resources[i]
		resource.Name = strings.TrimSpace(resource.Name)
		if resource.Name == "" {
			return fmt.Errorf("resources[%d]: name must not be blank", i)
		}
		key := strings.ToLower(resource.Name)
		if seen[key] {
			return fmt.Errorf("resources: duplicate name %q", resource.Name)
		}
		seen[key] = true
		if len(resource.Paths) == 0 {
			return fmt.Errorf("resources %q: needs at least one path", resource.Name)
		}
		for j := range resource.Paths {
			entry := &resource.Paths[j]
			path := strings.TrimSpace(entry.Path)
			if path == "~" {
				path = home
			} else if strings.HasPrefix(path, "~/") {
				path = filepath.Join(home, path[2:])
			}
			if !filepath.IsAbs(path) {
				return fmt.Errorf("resources %q: path %q must be absolute or start with ~/", resource.Name, entry.Path)
			}
			entry.Path = filepath.Clean(path)
			switch entry.Access {
			case "":
				entry.Access = "read"
			case "read", "write":
			default:
				return fmt.Errorf("resources %q: access of %q must be read or write, got %q", resource.Name, entry.Path, entry.Access)
			}
		}
	}
	return nil
}

// RepositoryResources gives the named resources to every agent that works in
// one repository, whichever checkout, worktree or clone it is in, as if they
// were ticked when it started. Nothing in the repository names Legato.
type RepositoryResources struct {
	// Repo names the repository as AgentGuidance.Repo does. Load keeps it
	// normalised as host/owner/name.
	Repo string `yaml:"repo" json:"repo"`
	// Resources names resources of the resources block. Load keeps each
	// once, spelled as that block spells it.
	Resources []string `yaml:"resources" json:"resources"`
}

// normalizeRepositoryResources checks each entry names a repository once and
// only resources the configuration defines. It runs after normalizeResources.
func normalizeRepositoryResources(cfg *Config) error {
	names := map[string]string{}
	for _, resource := range cfg.Resources {
		names[strings.ToLower(resource.Name)] = resource.Name
	}
	seen := map[string]bool{}
	for i := range cfg.Agents.RepositoryResources {
		entry := &cfg.Agents.RepositoryResources[i]
		repo, ok := NormalizeRepository(entry.Repo)
		if !ok {
			return fmt.Errorf("agents.repository_resources[%d].repo %q: want owner/name, host/owner/name or a Git remote address", i, entry.Repo)
		}
		if seen[repo] {
			return fmt.Errorf("agents.repository_resources[%d]: %s is already listed", i, repo)
		}
		seen[repo] = true
		entry.Repo = repo
		if len(entry.Resources) == 0 {
			return fmt.Errorf("agents.repository_resources[%d]: %s needs at least one resource", i, repo)
		}
		var kept []string
		given := map[string]bool{}
		for _, name := range entry.Resources {
			key := strings.ToLower(strings.TrimSpace(name))
			resolved, ok := names[key]
			if !ok {
				return fmt.Errorf("agents.repository_resources[%d]: there is no resource named %q in resources", i, strings.TrimSpace(name))
			}
			if given[key] {
				continue
			}
			given[key] = true
			kept = append(kept, resolved)
		}
		entry.Resources = kept
	}
	return nil
}
