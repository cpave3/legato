package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
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
	cfg.Learning.Enabled = true
	cfg.ReviewPasses.Enabled = true
	cfg.Credentials = []config.CredentialConfig{{Name: "gh"}}
	on := featureStates(setupFeatures(cfg, service.DoctorReport{}, featureCounts{Workers: 1, Workflows: 2}))
	for _, id := range []string{"sandbox", "credentials", "workers", "review_passes", "swarms", "learn", "reeve", "workflows"} {
		if on[id].State != service.FeatureOn {
			t.Errorf("%s = %+v, want on", id, on[id])
		}
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
