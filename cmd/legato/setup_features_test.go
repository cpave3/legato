package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func featureStates(features []service.SetupFeature) map[string]service.SetupFeature {
	out := map[string]service.SetupFeature{}
	for _, f := range features {
		out[f.ID] = f
	}
	return out
}

func TestMoreSetupSaysWhatIsOnOffOrNeedsSomething(t *testing.T) {
	cfg := &config.Config{}
	off := featureStates(setupFeatures(cfg, service.DoctorReport{}, featureCounts{}))
	for _, id := range []string{"sandbox", "credentials", "workers", "review_passes", "swarms", "learn", "workflows"} {
		if off[id].State != service.FeatureOff {
			t.Errorf("%s = %+v, want off", id, off[id])
		}
	}
	if off["reeve"].State != service.FeatureNeeds {
		t.Errorf("the Reeve without the sandbox = %+v, want needs", off["reeve"])
	}

	cfg.Agents.Sandbox.Enabled = true
	cfg.Swarm.Enabled = true
	cfg.ReviewPasses.Enabled = true
	cfg.Credentials = []config.CredentialConfig{{Name: "gh"}}
	on := featureStates(setupFeatures(cfg, service.DoctorReport{}, featureCounts{Workers: 1, Workflows: 2, LearnOn: 1}))
	for _, id := range []string{"sandbox", "credentials", "workers", "review_passes", "swarms", "learn", "reeve", "workflows"} {
		if on[id].State != service.FeatureOn {
			t.Errorf("%s = %+v, want on", id, on[id])
		}
	}

	if on["learn"].Link != "/workflows" {
		t.Errorf("learn links to %s; the learn workflow's trigger switches it", on["learn"].Link)
	}
	// learning.enabled is deprecated: it switches nothing.
	deprecated := &config.Config{Learning: config.LearningConfig{Enabled: true}}
	if got := featureStates(setupFeatures(deprecated, service.DoctorReport{}, featureCounts{}))["learn"]; got.State != service.FeatureOff {
		t.Errorf("learn with only learning.enabled = %+v, want off", got)
	}

	noBwrap := service.DoctorReport{Checks: []service.DoctorCheck{{ID: "bwrap", Required: true, Result: service.DoctorMissing, Fix: "Install bubblewrap"}}}
	needs := featureStates(setupFeatures(cfg, noBwrap, featureCounts{}))
	if needs["sandbox"].State != service.FeatureNeeds || needs["sandbox"].Detail != "Install bubblewrap" {
		t.Errorf("sandbox without bwrap = %+v", needs["sandbox"])
	}
	if needs["reeve"].State != service.FeatureNeeds {
		t.Errorf("the Reeve without bwrap = %+v", needs["reeve"])
	}
	for id, f := range needs {
		if f.Link == "" || f.Docs == "" {
			t.Errorf("%s has no link or docs: %+v", id, f)
		}
	}
}

func TestEveryMoreSetupCardLinksToASitePageThatExists(t *testing.T) {
	for _, f := range setupFeatures(&config.Config{}, service.DoctorReport{}, featureCounts{}) {
		page := strings.TrimSuffix(strings.TrimPrefix(f.Docs, siteDocs), "/")
		if page == f.Docs {
			t.Errorf("%s links outside the site: %s", f.ID, f.Docs)
			continue
		}
		if _, err := os.Stat(filepath.Join("..", "..", "site", "src", "content", "docs", page+".md")); err != nil {
			t.Errorf("%s links to %s, which has no page: %v", f.ID, f.Docs, err)
		}
	}
}

func TestLearnIsOnInSetupWhenALearnWorkflowsTriggerIsOnHere(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	example, _ := workflow.ExampleNamed("learn")
	learn, err := workflow.Parse("learn.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	triage, _ := workflow.ExampleNamed("triage")
	other, err := workflow.Parse("triage.workflow.yaml", triage.YAML)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := learnWorkflowsOn(ctx, db, []*workflow.Definition{learn, other}); got != 0 {
		t.Fatalf("paused = %d", got)
	}
	for _, name := range []string{"learn", "triage"} {
		if err := db.SetWorkflowTriggerSwitch(ctx, name, workflow.TriggerTaskEnteredColumn, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := learnWorkflowsOn(ctx, db, []*workflow.Definition{learn, other}); got != 1 {
		t.Fatalf("switched on = %d, want 1", got)
	}
}
