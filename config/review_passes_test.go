package config

import (
	"strings"
	"testing"
	"time"
)

const reviewPassesBase = `
adapters:
  codex:
    tiers:
      careful:
        launch_args: ["-m", "gpt-5.5"]
credentials:
  - name: reviewer-app
    secret: env://REVIEWER_KEY
    profile: github-reviewer
    settings: {client_id: Iv23liReViEwEr, repositories: ["cpave3/*"]}
  - name: author-app
    secret: env://AUTHOR_KEY
    profile: github
    settings: {client_id: Iv23liAuThOr, repositories: [cpave3/legato]}
`

func TestReviewPassesAreOffWithTheDecidedDefaults(t *testing.T) {
	writeTestConfig(t, "theme: default\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	rp := cfg.ReviewPasses
	if rp.Enabled {
		t.Fatal("review_passes.enabled defaults to false")
	}
	if rp.MaxPasses != 3 || rp.MaxConcurrent != 2 || rp.ReviewColumn != "Review" {
		t.Fatalf("defaults = %+v", rp)
	}
	if rp.PassTimeoutDuration() != 45*time.Minute || rp.PollIntervalDuration() != time.Minute || rp.QuietTimeDuration() != 2*time.Minute {
		t.Fatalf("durations = %v, %v, %v", rp.PassTimeoutDuration(), rp.PollIntervalDuration(), rp.QuietTimeDuration())
	}
}

func TestReviewPassesParseAWorkersSettings(t *testing.T) {
	writeTestConfig(t, reviewPassesBase+`
review_passes:
  enabled: true
  reviewer: {agent: codex, tier: careful, skill: 3-tier-review}
  github_credential: reviewer-app
  max_passes: 4
  pass_timeout: 30m
  max_concurrent: 1
  review_column: Review
  poll_interval: 90s
  quiet_time: 5m
  counted_bots: ["cubic-dev-ai[bot]"]
  required_checks: [build, test]
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	rp := cfg.ReviewPasses
	if !rp.Enabled || rp.Reviewer.Agent != "codex" || rp.Reviewer.Tier != "careful" || rp.Reviewer.Skill != "3-tier-review" ||
		rp.GitHubCredential != "reviewer-app" || rp.MaxPasses != 4 || rp.MaxConcurrent != 1 ||
		rp.PassTimeoutDuration() != 30*time.Minute || rp.PollIntervalDuration() != 90*time.Second || rp.QuietTimeDuration() != 5*time.Minute ||
		strings.Join(rp.CountedBots, ",") != "cubic-dev-ai[bot]" || strings.Join(rp.RequiredChecks, ",") != "build,test" {
		t.Fatalf("review_passes = %+v", rp)
	}
}

func TestInvalidReviewPassSettingsFailToLoad(t *testing.T) {
	for _, tc := range []struct{ name, block, want string }{
		{"no reviewer agent", "enabled: true\n  github_credential: reviewer-app", "review_passes.reviewer.agent is required"},
		{"no credential", "enabled: true\n  reviewer: {agent: codex}", "review_passes.github_credential is required"},
		{"unknown adapter", "enabled: true\n  reviewer: {agent: gemini}\n  github_credential: reviewer-app", `review_passes.reviewer.agent "gemini" is not configured`},
		{"unknown tier", "enabled: true\n  reviewer: {agent: codex, tier: fast}\n  github_credential: reviewer-app", `review_passes.reviewer.tier "fast"`},
		{"model and tier", "enabled: true\n  reviewer: {agent: codex, tier: careful, model: x}\n  github_credential: reviewer-app", "model or tier, not both"},
		{"unknown credential", "enabled: true\n  reviewer: {agent: codex}\n  github_credential: nobody", `review_passes.github_credential "nobody" is not a configured credential`},
		{"the author's credential", "enabled: true\n  reviewer: {agent: codex}\n  github_credential: author-app", `review_passes.github_credential "author-app" is on the github profile, not github-reviewer`},
		{"zero passes", "max_passes: 0", "review_passes.max_passes must be positive"},
		{"negative concurrency", "max_concurrent: -1", "review_passes.max_concurrent must be positive"},
		{"bad duration", "pass_timeout: soon", "review_passes.pass_timeout must be a positive duration"},
		{"zero quiet time", "quiet_time: 0s", "review_passes.quiet_time must be a positive duration"},
		{"unknown column", "enabled: true\n  reviewer: {agent: codex}\n  github_credential: reviewer-app\n  review_column: Reviewing", `review_passes.review_column "Reviewing" is not a board column`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, reviewPassesBase+"review_passes:\n  "+tc.block+"\n")
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestABoardWithoutAReviewColumnLoadsWhileReviewPassesAreOff(t *testing.T) {
	writeTestConfig(t, "board:\n  columns:\n    - name: Backlog\n    - name: Done\n")
	if _, err := Load(); err != nil {
		t.Fatalf("a board without Review: %v", err)
	}
}
