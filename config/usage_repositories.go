package config

import (
	"fmt"
	"regexp"
	"strings"
)

// UsageRepositoryDefault gives new work in the repositories it matches a
// baseline profile in place of usage.fallback_profile. Rules (those whose
// when_default allows the baseline) and the operator's pin still switch
// away from it; an explicit launch choice still wins.
type UsageRepositoryDefault struct {
	// Repository is owner/name or owner/* on GitHub. A leading github.com/
	// is dropped; it is kept lower-cased.
	Repository string `yaml:"repository" json:"repository"`
	Profile    string `yaml:"profile" json:"profile"`
}

// Kinds of repository default match.
const (
	UsageRepositoryExact = "exact"
	UsageRepositoryOwner = "owner"
)

var usageRepositoryPart = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)

func validateUsageRepositories(cfg *Config) error {
	seen := map[string]bool{}
	for i := range cfg.Usage.Repositories {
		d := &cfg.Usage.Repositories[i]
		path := fmt.Sprintf("usage.repositories[%d]", i)
		repo := strings.ToLower(strings.TrimSpace(d.Repository))
		repo = strings.TrimPrefix(repo, "github.com/")
		parts := strings.Split(repo, "/")
		ok := len(parts) == 2 && usageRepositoryPart.MatchString(parts[0]) &&
			(parts[1] == "*" || usageRepositoryPart.MatchString(parts[1]))
		if !ok {
			return fmt.Errorf("%s.repository %q names no GitHub repository: give owner/name or owner/*", path, d.Repository)
		}
		d.Repository = repo
		if seen[repo] {
			return fmt.Errorf("%s.repository %s is listed twice", path, repo)
		}
		seen[repo] = true
		p, found := usageProfileByName(cfg.Usage.Profiles, d.Profile)
		if !found {
			return fmt.Errorf("%s.profile %q is not a usage profile", path, strings.TrimSpace(d.Profile))
		}
		d.Profile = p.Name
	}
	return nil
}

// RepositoryDefault is the repository default for a repository (owner/name,
// host/owner/name or a Git remote address): an exact entry before its
// owner's, with its kind (UsageRepositoryExact or UsageRepositoryOwner).
// It reports false when none matches, and the fallback profile applies.
func (u UsageConfig) RepositoryDefault(repo string) (UsageRepositoryDefault, string, bool) {
	name, ok := NormalizeRepository(repo)
	if !ok || !strings.HasPrefix(name, "github.com/") {
		return UsageRepositoryDefault{}, "", false
	}
	name = strings.TrimPrefix(name, "github.com/")
	owner := strings.SplitN(name, "/", 2)[0] + "/*"
	var byOwner *UsageRepositoryDefault
	for i := range u.Repositories {
		d := u.Repositories[i]
		switch strings.ToLower(d.Repository) {
		case name:
			return d, UsageRepositoryExact, true
		case owner:
			if byOwner == nil {
				byOwner = &d
			}
		}
	}
	if byOwner != nil {
		return *byOwner, UsageRepositoryOwner, true
	}
	return UsageRepositoryDefault{}, "", false
}
