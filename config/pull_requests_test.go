package config

import (
	"strings"
	"testing"
)

func TestPullRequestRulesAreParsed(t *testing.T) {
	cfg, err := LoadBytes([]byte(`
pull_requests:
  rules:
    - match: cpave3/*
      mode: draft
    - match: github.com/cpave3/Legato
      mode: ready
`))
	if err != nil {
		t.Fatal(err)
	}
	rules := cfg.PullRequests.Rules
	if len(rules) != 2 || rules[0].Match != "cpave3/*" || rules[1].Match != "cpave3/Legato" || rules[1].Mode != PullRequestsReady {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestPullRequestRulesAreChecked(t *testing.T) {
	for name, body := range map[string]string{
		"a mode that is not one": "- {match: a/b, mode: maybe}",
		"no mode":                "- {match: a/b}",
		"not a repository":       "- {match: legato, mode: draft}",
		"too many parts":         "- {match: a/b/c/d, mode: draft}",
		"the same match twice":   "- {match: a/B, mode: draft}\n    - {match: A/b, mode: ready}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte("pull_requests:\n  rules:\n    " + body + "\n"))
			if err == nil || !strings.Contains(err.Error(), "pull_requests.rules") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestThePullRequestModeOfARepository(t *testing.T) {
	prs := PullRequestsConfig{Rules: []PullRequestRule{
		{Match: "*", Mode: PullRequestsAgent},
		{Match: "cpave3/*", Mode: PullRequestsDraft},
		{Match: "cpave3/legato", Mode: PullRequestsReady},
		{Match: "*/site", Mode: PullRequestsReady},
	}}
	for repo, want := range map[string]string{
		// An exact name beats any pattern, whatever the order.
		"cpave3/legato": PullRequestsReady,
		// Matching ignores case, and takes a host or a remote address.
		"CPave3/LEGATO":                          PullRequestsReady,
		"github.com/cpave3/legato":               PullRequestsReady,
		"git@github.com:cpave3/legato.git":       PullRequestsReady,
		"https://github.com/CPAVE3/burnlite.git": PullRequestsDraft,
		// owner/* has more of the name than */site.
		"cpave3/site": PullRequestsDraft,
		"other/site":  PullRequestsReady,
		"other/thing": PullRequestsAgent,
	} {
		if got := prs.ModeFor(repo); got != want {
			t.Errorf("ModeFor(%q) = %q, want %q", repo, got, want)
		}
	}
}

// With no rule that matches, the mode is "": today's behaviour (handoffs
// open drafts, local agents choose).
func TestUnsetPullRequestModeIsEmpty(t *testing.T) {
	cfg, err := LoadBytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.PullRequests.ModeFor("cpave3/legato"); got != "" {
		t.Fatalf("mode = %q", got)
	}
	prs := PullRequestsConfig{Rules: []PullRequestRule{{Match: "other/*", Mode: PullRequestsReady}}}
	if got := prs.ModeFor("cpave3/legato"); got != "" {
		t.Fatalf("mode = %q", got)
	}
	if got := prs.ModeFor("not a repository"); got != "" {
		t.Fatalf("mode = %q", got)
	}
}

// ready_after_review marks a draft repository's approved handoff stack ready
// (LEG-456): it belongs to a draft rule alone.
func TestReadyAfterReviewIsParsedOnADraftRule(t *testing.T) {
	cfg, err := LoadBytes([]byte(`
pull_requests:
  rules:
    - match: acme/*
      mode: draft
      ready_after_review: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if r := cfg.PullRequests.Rules[0]; !r.ReadyAfterReview {
		t.Fatalf("rule = %+v", r)
	}
}

func TestReadyAfterReviewOnARuleThatIsNotDraftIsAnError(t *testing.T) {
	for _, mode := range []string{PullRequestsReady, PullRequestsAgent} {
		_, err := LoadBytes([]byte("pull_requests:\n  rules:\n    - {match: a/b, mode: " + mode + ", ready_after_review: true}\n"))
		if err == nil || !strings.Contains(err.Error(), "pull_requests.rules[0].ready_after_review") {
			t.Fatalf("%s: err = %v", mode, err)
		}
	}
}

// The most specific rule decides: a pattern's ready_after_review does not
// reach a repository an exact draft rule without it names.
func TestReadyAfterReviewOfARepository(t *testing.T) {
	prs := PullRequestsConfig{Rules: []PullRequestRule{
		{Match: "acme/*", Mode: PullRequestsDraft, ReadyAfterReview: true},
		{Match: "acme/plain", Mode: PullRequestsDraft},
		{Match: "acme/open", Mode: PullRequestsReady},
	}}
	for repo, want := range map[string]bool{
		"acme/widget":                    true,
		"git@github.com:ACME/widget.git": true,
		"acme/plain":                     false,
		"acme/open":                      false,
		"other/thing":                    false,
	} {
		if got := prs.ReadyAfterReview(repo); got != want {
			t.Errorf("ReadyAfterReview(%q) = %v, want %v", repo, got, want)
		}
	}
}
