package config

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// The pull request modes a repository can have.
const (
	// PullRequestsDraft: pull requests are opened as drafts, and Legato's
	// broker refuses any other.
	PullRequestsDraft = "draft"
	// PullRequestsReady: pull requests are opened ready for review.
	PullRequestsReady = "ready"
	// PullRequestsAgent: the agent chooses, and a handoff may open either.
	PullRequestsAgent = "agent"
)

// PullRequestsConfig is how agents open pull requests, by repository.
type PullRequestsConfig struct {
	// Rules give a mode to the repositories they match. The most specific
	// match wins: an exact name, then the pattern with the most of the name
	// written out. No match is today's behaviour: a handoff opens drafts and
	// a local agent chooses.
	Rules []PullRequestRule `yaml:"rules"`
}

// PullRequestRule is the mode of the repositories one match names.
type PullRequestRule struct {
	// Match is owner/name, or a pattern with * (owner/*, */name, *), on
	// GitHub. A leading github.com/ is dropped. Case is ignored.
	Match string `yaml:"match" json:"match"`
	// Mode is draft, ready or agent.
	Mode string `yaml:"mode" json:"mode"`
	// ReadyAfterReview, on a draft rule only, marks a handed-off task's
	// stack ready once its review passes approve it (the handoff's
	// review-loop run's ready input, LEG-456). Legato never merges.
	ReadyAfterReview bool `yaml:"ready_after_review,omitempty" json:"ready_after_review,omitempty"`
}

// pullRequestMatchPart is one part of a match: a name, or a pattern of one.
var pullRequestMatchPart = regexp.MustCompile(`^[A-Za-z0-9._*-]+$`)

// normalizePullRequests checks each rule and drops a github.com/ host from
// its match.
func normalizePullRequests(cfg *Config) error {
	seen := map[string]bool{}
	for i := range cfg.PullRequests.Rules {
		r := &cfg.PullRequests.Rules[i]
		r.Match = strings.TrimSpace(r.Match)
		r.Mode = strings.TrimSpace(r.Mode)
		if strings.HasPrefix(strings.ToLower(r.Match), "github.com/") {
			r.Match = r.Match[len("github.com/"):]
		}
		parts := strings.Split(r.Match, "/")
		ok := r.Match == "*" || len(parts) == 2
		for _, p := range parts {
			ok = ok && pullRequestMatchPart.MatchString(p)
		}
		if !ok {
			return fmt.Errorf("pull_requests.rules[%d].match: %q names no GitHub repository: give owner/name, owner/*, */name or *", i, r.Match)
		}
		switch r.Mode {
		case PullRequestsDraft, PullRequestsReady, PullRequestsAgent:
		default:
			return fmt.Errorf("pull_requests.rules[%d].mode must be draft, ready or agent, got %q", i, r.Mode)
		}
		if r.ReadyAfterReview && r.Mode != PullRequestsDraft {
			return fmt.Errorf("pull_requests.rules[%d].ready_after_review is for a draft rule only, and %s is %s", i, r.Match, r.Mode)
		}
		key := strings.ToLower(r.Match)
		if seen[key] {
			return fmt.Errorf("pull_requests.rules: %s is matched twice", r.Match)
		}
		seen[key] = true
	}
	return nil
}

// ModeFor is the mode of a repository (owner/name, host/owner/name or a
// Git remote address on GitHub): the mode of the most specific rule that
// matches it, "" when none does.
func (c PullRequestsConfig) ModeFor(repo string) string {
	if r, ok := c.ruleFor(repo); ok {
		return r.Mode
	}
	return ""
}

// ReadyAfterReview says whether a repository's handed-off stacks are marked
// ready once review passes approve them: its most specific rule is a draft
// rule with ready_after_review.
func (c PullRequestsConfig) ReadyAfterReview(repo string) bool {
	r, ok := c.ruleFor(repo)
	return ok && r.Mode == PullRequestsDraft && r.ReadyAfterReview
}

// ruleFor is the most specific rule that matches a repository.
func (c PullRequestsConfig) ruleFor(repo string) (PullRequestRule, bool) {
	name, ok := NormalizeRepository(repo)
	if !ok || !strings.HasPrefix(name, "github.com/") {
		return PullRequestRule{}, false
	}
	name = strings.TrimPrefix(name, "github.com/")
	var best PullRequestRule
	bestScore := -1
	for _, r := range c.Rules {
		match := strings.ToLower(r.Match)
		if match == "*" {
			match = "*/*"
		}
		if ok, _ := path.Match(match, name); !ok {
			continue
		}
		// An exact name beats any pattern; then the pattern with more of
		// the name written out. The first of equals wins.
		score := len(strings.ReplaceAll(match, "*", ""))
		if !strings.Contains(match, "*") {
			score = 1 << 20
		}
		if score > bestScore {
			best, bestScore = r, score
		}
	}
	return best, bestScore >= 0
}
