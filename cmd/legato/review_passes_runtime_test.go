package main

import (
	"context"
	"encoding/json"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"path/filepath"
	"testing"
)

func TestUsageProfileReviewerAppearsInTheLiveAdvert(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "policy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Defaults()
	cfg.ReviewPasses.Enabled = true
	cfg.ReviewPasses.Reviewer.Agent = "claude-code"
	cfg.ReviewPasses.Reviewer.Model = "old-model"
	advert := reviewPassesAdvert(cfg, db)
	if got := advert().Defaults.Agent; got != "claude-code" {
		t.Fatal(got)
	}
	state := service.UsageProfileState{Profile: config.UsageProfile{Name: "save", Agent: "chimera", Reviewer: &config.UsageProfileReviewer{Agent: "codex", Tier: "careful"}}}
	raw, _ := json.Marshal(state)
	if err := db.SaveUsageProfile(ctx, string(raw), nil); err != nil {
		t.Fatal(err)
	}
	got := advert().Defaults
	if got.Agent != "codex" || got.Tier != "careful" || got.Model != "" {
		t.Fatalf("advert=%+v", got)
	}
}
