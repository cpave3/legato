package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// AgentGuidance is text Legato adds to the launch prompt of every agent that
// works in one repository, whichever checkout or worktree it is in.
type AgentGuidance struct {
	// Repo names the repository as its origin remote does: owner/name (on
	// GitHub), host/owner/name, or a Git remote address. Load keeps it
	// normalised as host/owner/name.
	Repo string `yaml:"repo" json:"repo"`
	Text string `yaml:"text" json:"text"`
}

var repositoryPart = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)

// NormalizeRepository turns a repository name or Git remote address into
// host/owner/name, lower-cased, with GitHub as the host of a bare
// owner/name. It reports false for anything that does not name one.
func NormalizeRepository(raw string) (string, bool) {
	repo := strings.TrimSpace(raw)
	if repo == "" {
		return "", false
	}

	// Convert the common SCP-style Git remote into URL form before parsing.
	if strings.HasPrefix(repo, "git@") && !strings.Contains(repo, "://") {
		parts := strings.SplitN(strings.TrimPrefix(repo, "git@"), ":", 2)
		if len(parts) != 2 {
			return "", false
		}
		repo = parts[0] + "/" + parts[1]
	} else if strings.Contains(repo, "://") {
		u, err := url.Parse(repo)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		repo = u.Hostname() + "/" + strings.TrimPrefix(u.Path, "/")
	}

	repo = strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
	parts := strings.Split(repo, "/")
	if len(parts) == 2 {
		parts = append([]string{"github.com"}, parts...)
	}
	if len(parts) != 3 {
		return "", false
	}
	for i, part := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(part))
		if !repositoryPart.MatchString(parts[i]) {
			return "", false
		}
	}
	return strings.Join(parts, "/"), true
}

// normalizeAgentGuidance checks each guidance entry names a repository, has
// text, and is the only one for its repository.
func normalizeAgentGuidance(cfg *Config) error {
	seen := map[string]bool{}
	for i := range cfg.Agents.Guidance {
		entry := &cfg.Agents.Guidance[i]
		repo, ok := NormalizeRepository(entry.Repo)
		if !ok {
			return fmt.Errorf("agents.guidance[%d].repo %q: want owner/name, host/owner/name or a Git remote address", i, entry.Repo)
		}
		entry.Repo = repo
		entry.Text = strings.TrimSpace(entry.Text)
		if entry.Text == "" {
			return fmt.Errorf("agents.guidance[%d].text: guidance for %s is empty", i, repo)
		}
		if seen[repo] {
			return fmt.Errorf("agents.guidance[%d]: %s already has guidance", i, repo)
		}
		seen[repo] = true
	}
	return nil
}
