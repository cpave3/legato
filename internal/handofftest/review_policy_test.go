package handofftest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// The engine resolves policy only at a new pass. A crash retry and the
// review posted from it retain that pass's selected reviewer.
func TestReviewerPolicyChangesAtNewPassNotCrashRetry(t *testing.T) {
	agent := "codex"
	var repos []string
	opts := ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
		o.Config, o.Advert = ReviewPassOptions{}.config, ReviewPassOptions{}.advert
		o.ReviewerDefaults = func(_ context.Context, repo string, defaults service.ReviewPassSettings) (service.ReviewPassSettings, error) {
			repos = append(repos, repo)
			defaults.Agent, defaults.Model, defaults.Tier = agent, "", ""
			return defaults, nil
		}
		return o
	}}
	r := newReviewRun(t, opts)
	view := r.start(t, pr12)
	task := view.Run.TaskID
	r.tick()
	assertReviewer := func(want string) {
		t.Helper()
		session, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), task, "reviewer")
		if err != nil || session == nil || session.AgentKind != want {
			t.Fatalf("reviewer = %+v, %v; want %s", session, err, want)
		}
	}
	assertReviewer("codex")
	agent = "claude-code"
	if err := r.worker.Tmux.Kill("legato-" + task + "_reviewer"); err != nil {
		t.Fatal(err)
	}
	r.tick()
	assertReviewer("codex")
	r.submit(t, task, service.ReviewSubmission{Summary: "One problem.", Findings: []service.SubmittedFinding{blockingFinding}})
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || !strings.Contains(posted[0].Footer, "codex") {
		t.Fatalf("attribution = %+v", posted)
	}
	if err := r.worker.Reviews.NextPass(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	r.tick()
	assertReviewer("claude-code")
	// Resolved for the loop's repository, once per new pass: never on the
	// crash retry.
	if len(repos) != 2 || repos[0] != "github.com/acme/widget" || repos[1] != repos[0] {
		t.Fatalf("reviewer defaults asked for %q", repos)
	}
}

func TestReviewerPolicyPreservesExplicitChoicesButNotSkillOnlyDefaults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override service.ReviewPassOverride
		want     string
	}{
		{"explicit tool", service.ReviewPassOverride{Agent: "codex", Tier: "careful"}, "codex"},
		{"skill only", service.ReviewPassOverride{Skill: "3-tier-review"}, "claude-code"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newReviewed(t, ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
				o.Config, o.Advert = ReviewPassOptions{}.config, ReviewPassOptions{}.advert
				o.ReviewerDefaults = func(_ context.Context, _ string, defaults service.ReviewPassSettings) (service.ReviewPassSettings, error) {
					defaults.Agent, defaults.Model, defaults.Tier = "claude-code", "", ""
					return defaults, nil
				}
				return o
			}})
			settings, err := h.worker.Reviews.Override(tc.override)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.worker.Reviews.SetSettings(ctx, h.card, settings); err != nil {
				t.Fatal(err)
			}
			h.finish(t)
			if session := h.reviewer(t); session.AgentKind != tc.want {
				t.Fatalf("reviewer = %s; want %s", session.AgentKind, tc.want)
			}
			if _, err := h.submit(t, service.ReviewSubmission{Summary: "Clean."}); err != nil {
				t.Fatal(err)
			}
			posted := h.worker.ReviewGitHub.Posted()
			if len(posted) != 1 || !strings.Contains(posted[0].Footer, tc.want) {
				t.Fatalf("attribution = %+v", posted)
			}
			if tc.override.Tier != "" && !strings.Contains(posted[0].Footer, "tier careful") {
				t.Fatalf("explicit tier lost: %+v", posted)
			}
		})
	}
}

func TestReviewerPolicyKeepsLegacySettingsFrozen(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
		o.Config, o.Advert = ReviewPassOptions{}.config, ReviewPassOptions{}.advert
		o.ReviewerDefaults = func(_ context.Context, _ string, _ service.ReviewPassSettings) (service.ReviewPassSettings, error) {
			t.Fatal("legacy settings must not consult live policy")
			return service.ReviewPassSettings{}, nil
		}
		return o
	}})
	view := r.start(t, pr12)
	if err := r.worker.Reviews.SetSettings(context.Background(), view.Run.TaskID, service.ReviewPassSettings{
		Enabled: true, Agent: "codex", Model: "legacy-model", Skill: "3-tier-review", MaxPasses: 3,
	}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Clean."})
	posted := r.worker.ReviewGitHub.Posted()
	if len(posted) != 1 || !strings.Contains(posted[0].Footer, "codex (legacy-model)") {
		t.Fatalf("legacy attribution = %+v", posted)
	}
}

func TestReviewerPolicyWorkflowChoicesWinAndSkillOnlyStaysLive(t *testing.T) {
	for _, tc := range []struct{ reviewer, want, model string }{
		{"codex:careful", "codex", "tier careful"},
		{"", "claude-code", "policy-model"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			r := newReviewRun(t, ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
				o.Config, o.Advert = ReviewPassOptions{}.config, ReviewPassOptions{}.advert
				o.ReviewerDefaults = func(_ context.Context, _ string, defaults service.ReviewPassSettings) (service.ReviewPassSettings, error) {
					defaults.Agent, defaults.Model, defaults.Tier = "claude-code", "policy-model", ""
					return defaults, nil
				}
				return o
			}})
			view := r.startLoop(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "reviewer": tc.reviewer, "skill": "3-tier-review"})
			r.tick()
			session, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), view.Run.TaskID, "reviewer")
			if err != nil || session == nil || session.AgentKind != tc.want {
				t.Fatalf("reviewer = %+v, %v; want %s", session, err, tc.want)
			}
			r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Clean."})
			posted := r.worker.ReviewGitHub.Posted()
			if len(posted) != 1 || !strings.Contains(posted[0].Footer, tc.want+" ("+tc.model+")") {
				t.Fatalf("attribution = %+v", posted)
			}
		})
	}
}

func TestReviewerPolicyFailureIsVisibleAndStartsNoReviewer(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{Runtime: func(o service.ReviewPassOptions) service.ReviewPassOptions {
		o.Config, o.Advert = ReviewPassOptions{}.config, ReviewPassOptions{}.advert
		o.ReviewerDefaults = func(_ context.Context, _ string, _ service.ReviewPassSettings) (service.ReviewPassSettings, error) {
			return service.ReviewPassSettings{}, fmt.Errorf("profile configuration cannot be read")
		}
		return o
	}})
	view := r.start(t, pr12)
	r.tick()
	loop := r.loopPhase(t, view.Run.TaskID)
	if loop.Phase != service.ReviewPhaseStuck || !strings.Contains(loop.Reason, "profile configuration cannot be read") {
		t.Fatalf("loop = %+v", loop)
	}
	if r.reviewer(t, view.Run.TaskID) {
		t.Fatal("policy failure silently started a default reviewer")
	}
}
