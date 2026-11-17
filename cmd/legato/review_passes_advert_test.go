package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// The review-pass advert offers the worker's Legato-managed skills that are
// on, beside the agent tools' own, and lists them as managed (LEG-235).
func TestTheReviewPassAdvertOffersTheManagedSkillsThatAreOn(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	dir := filepath.Join(root, "skills")
	for _, name := range []string{"my-review", "turned-off"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		skill := "---\nname: " + name + "\ndescription: Reviews.\n---\n"
		if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(skill), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.New(filepath.Join(root, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.SetSkillEnabled(ctx, "turned-off", false); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Skills.Dir = dir
	cfg.ReviewPasses.Enabled = true
	cfg.ReviewPasses.Reviewer.Agent = "claude-code"

	advert := reviewPassesAdvert(cfg, db)()
	if advert == nil || !slices.Contains(advert.Skills, "my-review") || !slices.Equal(advert.ManagedSkills, []string{"my-review"}) {
		t.Fatalf("advert %+v; want my-review offered and managed, turned-off left out", advert)
	}
}
