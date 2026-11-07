package main

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// reviewPassEvery is how often the review loop looks at its handoffs,
// besides when a card or a handoff changes.
const reviewPassEvery = 15 * time.Second

// reviewPassConfig is config review_passes as the review loop reads it.
func reviewPassConfig(cfg *config.Config) func() service.ReviewPassConfig {
	return func() service.ReviewPassConfig {
		rp := cfg.ReviewPasses
		return service.ReviewPassConfig{
			Enabled: rp.Enabled, PassTimeout: rp.PassTimeoutDuration(), MaxConcurrent: rp.MaxConcurrent, ReviewColumn: rp.ReviewColumn,
			PollInterval: rp.PollIntervalDuration(), QuietTime: rp.QuietTimeDuration(), CountedBots: rp.CountedBots,
			Defaults: service.ReviewPassSettings{Enabled: rp.Enabled, Agent: rp.Reviewer.Agent, Model: rp.Reviewer.Model,
				Tier: rp.Reviewer.Tier, Skill: rp.Reviewer.Skill, MaxPasses: rp.MaxPasses},
		}
	}
}

// wireReviewPasses starts the review loop of a worker with review_passes
// on: it gates its handoffs' Done on a clean review, takes submit_review
// through the broker, and ticks while this process serves the broker. It
// needs the credential channels, which post as the reviewer App.
func wireReviewPasses(deps integrationDeps, handoffs *service.HandoffWorkerService) (stop func()) {
	if !deps.cfg.ReviewPasses.Enabled || deps.approvals == nil || deps.approvals.channels == nil {
		return func() {}
	}
	agents, ok := deps.agents.(service.ReviewAgents)
	if !ok || deps.board == nil {
		return func() {}
	}
	opts := service.ReviewPassOptions{
		Config: reviewPassConfig(deps.cfg), Agents: agents, Board: deps.board, Bus: deps.bus,
		GitHub:   service.NewReviewPublisher(deps.approvals.channels, deps.cfg.ReviewPasses.GitHubCredential),
		Checkout: service.GitReviewCheckout{Store: deps.db},
		Run:      func(f func()) { go f() },
		// A dead author is resumed, with everything it had, to hear of its
		// feedback.
		ResumeAuthor: handoffs.ResumeAuthor,
	}
	if prober, ok := deps.agents.(service.CheckoutOverlayProber); ok {
		opts.Overlay = prober.CheckoutOverlayAvailable
	}
	reviews := service.NewReviewPassService(deps.db, opts)
	handoffs.SetReviews(reviews)
	deps.approvals.setReviewPasses(reviews)
	ctx, cancel := context.WithCancel(context.Background())
	owner := deps.approvals.brokerOwner
	tick := func(ctx context.Context) {
		if owner == nil || owner() {
			reviews.Tick(ctx)
		}
	}
	if deps.bus != nil {
		moved := deps.bus.Subscribe(events.EventCardMoved)
		go func() {
			defer deps.bus.Unsubscribe(moved)
			timer := time.NewTicker(reviewPassEvery)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-moved:
				case <-timer.C:
				}
				tick(ctx)
			}
		}()
	}
	log.Printf("review passes: on, reviewing as %s with %s", deps.cfg.ReviewPasses.GitHubCredential, deps.cfg.ReviewPasses.Reviewer.Agent)
	return cancel
}
