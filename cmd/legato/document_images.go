package main

import (
	"context"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// planImageRoot and reviewImageRoot are where Legato keeps its copies of the
// image files that the Markdown of plans and of reviews shows: written by
// the plan and review commands, given out by the web server.
func planImageRoot(cfg *config.Config) string {
	return filepath.Join(config.ResolveDataDir(cfg), "plan-images")
}

func reviewImageRoot(cfg *config.Config) string {
	return filepath.Join(config.ResolveDataDir(cfg), "review-images")
}

// newPlanService and newReviewService build the two services with the
// folders they keep image files in, so that a command and a server can
// never disagree about where those are.
func newPlanService(cfg *config.Config, db *store.Store, tmux service.TmuxManager, bus *events.Bus) *service.PlanService {
	svc := service.NewPlanService(db, tmux, bus)
	svc.SetImageDir(planImageRoot(cfg))
	return svc
}

func newReviewService(cfg *config.Config, db *store.Store, tmux service.TmuxManager, bus *events.Bus) *service.ReviewService {
	svc := service.NewReviewService(db, tmux, bus)
	svc.SetImageDirs(reviewImageRoot(cfg), planImageRoot(cfg))
	return svc
}

// sweepDocumentImages removes the kept image files that no plan and no
// review names any more: a deleted task takes its plans and tours with it in
// the database and leaves their files, and an edited chapter leaves the
// files of its old narration. A web server runs it when it starts. Nothing
// waits for it, and a failure costs disk space only.
func sweepDocumentImages(plans *service.PlanService, reviews *service.ReviewService) {
	go func() {
		_ = plans.RemoveOrphanImages(context.Background())
		_ = reviews.RemoveOrphanImages(context.Background())
	}()
}

// markdownInputDir is the folder a relative image path is taken from, for
// Markdown read from the file a command was given: the file's own folder,
// and the working directory for standard input.
func markdownInputDir(path string) string {
	if path == "" || path == "-" {
		return ""
	}
	return filepath.Dir(path)
}
