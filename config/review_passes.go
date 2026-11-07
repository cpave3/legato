package config

import (
	"fmt"
	"strings"
	"time"
)

// ReviewPassesConfig is a worker's review loop for make-a-change handoffs:
// once the author finishes, a reviewer on another model reviews its pull
// requests read-only, Legato posts the review, and the findings go back to
// the author until a pass is clean.
type ReviewPassesConfig struct {
	Enabled  bool                `yaml:"enabled"`
	Reviewer ReviewerAgentConfig `yaml:"reviewer"`
	// GitHubCredential names the reviewer App's credential, on a host-only
	// profile: Legato posts and polls with it; no agent is given it.
	GitHubCredential string `yaml:"github_credential"`
	MaxPasses        int    `yaml:"max_passes"`
	PassTimeout      string `yaml:"pass_timeout"`
	MaxConcurrent    int    `yaml:"max_concurrent"`
	ReviewColumn     string `yaml:"review_column"`
	PollInterval     string `yaml:"poll_interval"`
	QuietTime        string `yaml:"quiet_time"`
	// CountedBots are bot logins whose comments count as feedback, beside
	// the repository's owners, members and collaborators.
	CountedBots []string `yaml:"counted_bots"`
	// RequiredChecks are the checks a pull request must pass; empty means
	// its branch protection's required checks.
	RequiredChecks []string `yaml:"required_checks"`
}

// ReviewerAgentConfig is the reviewer's agent tool, its model or tier, and
// the review skill it is told to use.
type ReviewerAgentConfig struct {
	Agent string `yaml:"agent"`
	Model string `yaml:"model"`
	Tier  string `yaml:"tier"`
	Skill string `yaml:"skill"`
}

func defaultReviewPasses() ReviewPassesConfig {
	return ReviewPassesConfig{
		MaxPasses: 3, PassTimeout: "45m", MaxConcurrent: 2, ReviewColumn: "Review",
		PollInterval: "60s", QuietTime: "2m",
	}
}

// PassTimeoutDuration is the wall-clock time a reviewer has for a pass.
func (r ReviewPassesConfig) PassTimeoutDuration() time.Duration { return mustDuration(r.PassTimeout) }

// PollIntervalDuration is how often pull requests are polled for comments.
func (r ReviewPassesConfig) PollIntervalDuration() time.Duration { return mustDuration(r.PollInterval) }

// QuietTimeDuration is how long feedback waits for more before it is sent.
func (r ReviewPassesConfig) QuietTimeDuration() time.Duration { return mustDuration(r.QuietTime) }

func mustDuration(value string) time.Duration {
	d, _ := time.ParseDuration(value)
	return d
}

// ValidateReviewPasses checks the review_passes block against the adapters,
// credentials and board columns of the loaded configuration.
func ValidateReviewPasses(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	rp := &cfg.ReviewPasses
	rp.Reviewer.Agent = strings.TrimSpace(rp.Reviewer.Agent)
	rp.Reviewer.Model = strings.TrimSpace(rp.Reviewer.Model)
	rp.Reviewer.Tier = strings.TrimSpace(rp.Reviewer.Tier)
	rp.Reviewer.Skill = strings.TrimSpace(rp.Reviewer.Skill)
	rp.GitHubCredential = strings.TrimSpace(rp.GitHubCredential)
	rp.ReviewColumn = strings.TrimSpace(rp.ReviewColumn)
	for _, count := range []struct {
		name  string
		value int
	}{{"max_passes", rp.MaxPasses}, {"max_concurrent", rp.MaxConcurrent}} {
		if count.value <= 0 {
			return fmt.Errorf("review_passes.%s must be positive", count.name)
		}
	}
	for _, d := range []struct{ name, value string }{
		{"pass_timeout", rp.PassTimeout}, {"poll_interval", rp.PollInterval}, {"quiet_time", rp.QuietTime},
	} {
		if parsed, err := time.ParseDuration(d.value); err != nil || parsed <= 0 {
			return fmt.Errorf("review_passes.%s must be a positive duration, got %q", d.name, d.value)
		}
	}
	if rp.ReviewColumn != "" && !boardHasColumn(cfg, rp.ReviewColumn) {
		return fmt.Errorf("review_passes.review_column %q is not a board column", rp.ReviewColumn)
	}
	if rp.Reviewer.Model != "" && rp.Reviewer.Tier != "" {
		return fmt.Errorf("review_passes.reviewer takes model or tier, not both")
	}
	if rp.Reviewer.Agent != "" {
		adapter, ok := cfg.Adapters[rp.Reviewer.Agent]
		if !ok {
			if _, known := supportedSwarmAgents[rp.Reviewer.Agent]; !known {
				return fmt.Errorf("review_passes.reviewer.agent %q is not configured", rp.Reviewer.Agent)
			}
		}
		if rp.Reviewer.Tier != "" {
			if _, ok := adapter.Tiers[rp.Reviewer.Tier]; !ok {
				return fmt.Errorf("review_passes.reviewer.tier %q is not configured for adapter %q", rp.Reviewer.Tier, rp.Reviewer.Agent)
			}
		}
	}
	if rp.GitHubCredential != "" && !hasCredential(cfg, rp.GitHubCredential) {
		return fmt.Errorf("review_passes.github_credential %q is not a configured credential", rp.GitHubCredential)
	}
	if !rp.Enabled {
		return nil
	}
	if rp.Reviewer.Agent == "" {
		return fmt.Errorf("review_passes.reviewer.agent is required when review passes are enabled")
	}
	if rp.GitHubCredential == "" {
		return fmt.Errorf("review_passes.github_credential is required when review passes are enabled")
	}
	return nil
}

func boardHasColumn(cfg *Config, name string) bool {
	for _, column := range cfg.Board.Columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func hasCredential(cfg *Config, name string) bool {
	for _, credential := range cfg.Credentials {
		if strings.EqualFold(credential.Name, name) {
			return true
		}
	}
	return false
}
