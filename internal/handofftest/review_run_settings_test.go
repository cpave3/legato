package handofftest

import (
	"context"
	"encoding/json"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func (r reviewRun) settings(t *testing.T, taskID string) service.ReviewPassSettings {
	t.Helper()
	loop, err := r.worker.Store.GetHandoffReview(context.Background(), taskID)
	if err != nil || loop == nil {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
	var settings service.ReviewPassSettings
	if err := json.Unmarshal([]byte(loop.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestAReviewLoopTakesThisLegatosSettingsUnlessItsInputsSayOtherwise(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	plain := r.start(t, pr12)
	if s := r.settings(t, plain.Run.TaskID); s.MaxPasses != 3 || s.Agent != "codex" || s.Skill != "3-tier-review" {
		t.Fatalf("defaults: %+v", s)
	}
	if _, err := r.runs.Workflows.Cancel(context.Background(), plain.Run.ID, "next"); err != nil {
		t.Fatal(err)
	}
	empty := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "max_passes": "", "reviewer": ""})
	if s := r.settings(t, empty.Run.TaskID); s.MaxPasses != 3 || s.Agent != "codex" {
		t.Fatalf("empty inputs: %+v", s)
	}
	if _, err := r.runs.Workflows.Cancel(context.Background(), empty.Run.ID, "next"); err != nil {
		t.Fatal(err)
	}
	tuned := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "max_passes": "5", "reviewer": "claude-code"})
	if s := r.settings(t, tuned.Run.TaskID); s.MaxPasses != 5 || s.Agent != "claude-code" || s.Model != "" {
		t.Fatalf("tuned: %+v", s)
	}
	r.tick()
	reviewer, err := r.worker.Store.GetAgentSessionInSlot(context.Background(), tuned.Run.TaskID, "reviewer")
	if err != nil || reviewer.AgentKind != "claude-code" {
		t.Fatalf("reviewer = %+v, %v", reviewer, err)
	}
}

func TestAStepParameterOverridesTheSettingAndAnInputOverridesBoth(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	definition, err := workflowengine.Parse("tuned.yaml", []byte(`
schema_version: 1
name: tuned
title: Tuned
inputs:
  max_passes: {type: integer, default: 0}
steps:
  - id: review
    title: Review
    type: github.pr.review_passes
    with: {repo: acme/widget, pr_number: 12, max_passes: 2}
  - id: again
    title: Again
    type: github.pr.review_passes
    with: {repo: acme/widget, pr_number: 13, max_passes: "${{ workflow.inputs.max_passes }}"}
`))
	if err != nil {
		t.Fatal(err)
	}
	view, err := r.runs.Workflows.Start(context.Background(), definition, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.runs.Open(context.Background())
	if s := r.settings(t, view.Run.TaskID); s.MaxPasses != 2 {
		t.Fatalf("step parameter: %+v", s)
	}
}

func TestAReviewerThisLegatoDoesNotOfferIsRefusedAtStart(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	view := r.start(t, map[string]string{"repo": "acme/widget", "pr_number": "12", "reviewer": "gemini"})
	refused(t, view, `no reviewer tool "gemini"`)
	refused(t, view, "claude-code, codex")
}
