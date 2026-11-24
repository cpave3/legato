package main

import (
	"context"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// siteDocs is the user site, where each More setup card links.
const siteDocs = "https://cpave3.github.io/legato/"

// featureCounts are the things More setup counts outside the configuration.
type featureCounts struct {
	Workers   int
	Workflows int
	// LearnOn is how many workflows with a Learn step have a trigger on in
	// this Legato: automatic Learn.
	LearnOn int
}

// setupFeatures are the More setup cards: each optional feature, on, off,
// or on but missing what the doctor says, with its place in the web UI and
// its page on the site.
func setupFeatures(cfg *config.Config, report service.DoctorReport, counts featureCounts) []service.SetupFeature {
	state := func(on bool) string {
		if on {
			return service.FeatureOn
		}
		return service.FeatureOff
	}
	var bwrapFix string
	for _, c := range report.Checks {
		if c.ID == "bwrap" && c.Failed() {
			bwrapFix = c.Fix
		}
	}
	sandbox := service.SetupFeature{ID: "sandbox", Title: "Sandbox", State: state(cfg.Agents.Sandbox.Enabled),
		Link: "/settings#setting-agents.sandbox.enabled", Docs: siteDocs + "security/sandbox/"}
	if cfg.Agents.Sandbox.Enabled && bwrapFix != "" {
		sandbox.State, sandbox.Detail = service.FeatureNeeds, bwrapFix
	}
	reeve := service.SetupFeature{ID: "reeve", Title: "The Reeve", State: service.FeatureOn,
		Link: "/settings#reeve", Docs: siteDocs + "using/reeve/"}
	switch {
	case !cfg.Agents.Sandbox.Enabled:
		reeve.State, reeve.Detail = service.FeatureNeeds, "The Reeve runs only in the sandbox: turn it on."
	case bwrapFix != "":
		reeve.State, reeve.Detail = service.FeatureNeeds, bwrapFix
	}
	return []service.SetupFeature{
		sandbox,
		{ID: "credentials", Title: "Credentials", State: state(len(cfg.Credentials) > 0),
			Link: "/settings#credentials", Docs: siteDocs + "security/credential-broker/"},
		{ID: "workers", Title: "Workers", State: state(counts.Workers > 0),
			Link: "/settings#workers", Docs: siteDocs + "running/instances/"},
		{ID: "review_passes", Title: "Review passes", State: state(cfg.ReviewPasses.Enabled),
			Link: "/settings#setting-review_passes.enabled", Docs: siteDocs + "using/review-passes/"},
		{ID: "swarms", Title: "Swarms", State: state(cfg.Swarm.Enabled),
			Link: "/settings#setting-swarm.enabled", Docs: siteDocs + "using/swarms/"},
		{ID: "learn", Title: "Learn", State: state(counts.LearnOn > 0),
			Link: "/workflows", Docs: siteDocs + "using/learn/"},
		reeve,
		{ID: "workflows", Title: "Workflow templates", State: state(counts.Workflows > 0),
			Link: "/workflows", Docs: siteDocs + "using/workflows/"},
	}
}

// learnWorkflowsOn counts the workflows with a Learn step whose column
// trigger is on in this Legato: its switch, else the file's enabled.
func learnWorkflowsOn(ctx context.Context, db *store.Store, workflows []*workflow.Definition) int {
	on := 0
	for _, definition := range workflows {
		trigger := definition.On.TaskEnteredColumn
		if trigger == nil || !definition.UsesLearning() {
			continue
		}
		enabled := trigger.Enabled == nil || *trigger.Enabled
		if switched, ok, err := db.WorkflowTriggerSwitchFor(ctx, definition.Name, workflow.TriggerTaskEnteredColumn); err == nil && ok {
			enabled = switched
		}
		if enabled {
			on++
		}
	}
	return on
}
