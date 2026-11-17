package server

import (
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestMetaAdvertisesReviewPassesOnlyWhenTheyAreOn(t *testing.T) {
	srv := pairingServer()
	var meta map[string]any
	call(t, srv, http.MethodGet, "/api/meta", false, nil, &meta)
	if _, ok := meta["review_passes"]; ok {
		t.Fatalf("a worker without review passes advertised them: %v", meta)
	}

	srv.SetReviewPasses(func() *service.ReviewPassesAdvert {
		return &service.ReviewPassesAdvert{
			Defaults: service.ReviewPassSettings{Agent: "codex", Model: "gpt-5.5", Skill: "3-tier-review", MaxPasses: 3},
			Adapters: []service.ReviewAdapter{{Name: "codex", Tiers: []string{"careful"}}},
			Skills:   []string{"3-tier-review"},
		}
	})
	meta = nil
	call(t, srv, http.MethodGet, "/api/meta", false, nil, &meta)
	advert, ok := meta["review_passes"].(map[string]any)
	if !ok {
		t.Fatalf("meta = %v; want review_passes", meta)
	}
	defaults := advert["defaults"].(map[string]any)
	if defaults["agent"] != "codex" || defaults["model"] != "gpt-5.5" || defaults["max_passes"] != float64(3) {
		t.Fatalf("defaults = %v", defaults)
	}
	if adapters := advert["adapters"].([]any); len(adapters) != 1 || adapters[0].(map[string]any)["name"] != "codex" {
		t.Fatalf("adapters = %v", advert["adapters"])
	}
	// Skill names are for a paired main only (the capabilities advert):
	// /api/meta, which anyone on the network reads, names none.
	if skills := advert["skills"].([]any); len(skills) != 0 || defaults["skill"] != nil {
		t.Fatalf("skills = %v, default skill %v; want none in /api/meta", advert["skills"], defaults["skill"])
	}
}
