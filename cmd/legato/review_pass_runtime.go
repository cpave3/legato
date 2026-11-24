package main

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reviewPassEvery is how often the review loop looks at its handoffs,
// besides when a card or a handoff changes.
const reviewPassEvery = 15 * time.Second

// reviewPassRunGap is how long after a workflow run changes the review
// loop looks: a step completing and the next starting are several changes,
// and each tick asks GitHub about every loop.
const reviewPassRunGap = 2 * time.Second

// reviewPassWake ticks the review loop until ctx ends: at once when a card
// moves, every `every`, and `gap` after a run changes, however many changes
// come in that time. A review loop's own changes (it moved on) never tick
// it again.
func reviewPassWake(ctx context.Context, moved, runs <-chan events.Event, every, gap time.Duration, tick func(context.Context)) {
	timer := time.NewTicker(every)
	defer timer.Stop()
	var pending <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-moved:
		case event := <-runs:
			if payload, ok := event.Payload.(events.WorkflowChangedPayload); ok && payload.Kind == service.ReviewLoopChangedKind {
				continue
			}
			if pending == nil {
				pending = time.After(gap)
			}
			continue
		case <-pending:
			pending = nil
		case <-timer.C:
		}
		tick(ctx)
	}
}

// reviewPassConfig is config review_passes as the review loop reads it.
func reviewPassConfig(cfg *config.Config) func() service.ReviewPassConfig {
	return func() service.ReviewPassConfig {
		rp := cfg.ReviewPasses
		return service.ReviewPassConfig{
			Enabled: rp.Enabled, PassTimeout: rp.PassTimeoutDuration(), MaxConcurrent: rp.MaxConcurrent, ReviewColumn: rp.ReviewColumn,
			PollInterval: rp.PollIntervalDuration(), QuietTime: rp.QuietTimeDuration(), CountedBots: rp.CountedBots,
			RequiredChecks: rp.RequiredChecks,
			Defaults: service.ReviewPassSettings{Enabled: rp.Enabled, Agent: rp.Reviewer.Agent, Model: rp.Reviewer.Model,
				Tier: rp.Reviewer.Tier, Skill: rp.Reviewer.Skill, MaxPasses: rp.MaxPasses},
		}
	}
}

// workerReviewPassOptions gives the review loop what it reads from the
// configuration: review_passes, and the advert a main's Hand off override is
// checked against, which is the one /api/meta gives (reviewPassesAdvert).
// Without the advert every override is refused.
func workerReviewPassOptions(cfg *config.Config, db *store.Store, opts service.ReviewPassOptions) service.ReviewPassOptions {
	opts.Config, opts.Advert = reviewPassConfig(cfg), reviewPassesAdvert(cfg, db)
	return opts
}

// startReviewPasses starts the review-pass engine on an instance with
// review_passes on: it takes submit_review through the broker and ticks
// while this process serves the broker. It needs the credential channels,
// which post as the reviewer App. It does nothing until a caller has a loop:
// the handoff caller registers when handoffs are wired
// (wireHandoffReviewCaller). Nil when review passes are off.
func startReviewPasses(deps integrationDeps) (*service.ReviewPassService, func()) {
	if !deps.cfg.ReviewPasses.Enabled || deps.approvals == nil || deps.approvals.channels == nil {
		return nil, func() {}
	}
	agents, ok := deps.agents.(service.ReviewAgents)
	if !ok {
		return nil, func() {}
	}
	opts := workerReviewPassOptions(deps.cfg, deps.db, service.ReviewPassOptions{
		Agents: agents, Bus: deps.bus,
		GitHub:   learningPublisher(deps.db, service.NewReviewPublisher(deps.approvals.channels, deps.cfg.ReviewPasses.GitHubCredential)),
		Checkout: service.GitReviewCheckout{Store: deps.db},
		Run:      func(f func()) { go f() },
	})
	if prober, ok := deps.agents.(service.CheckoutOverlayProber); ok {
		opts.Overlay = prober.CheckoutOverlayAvailable
	}
	reviews := service.NewReviewPassService(deps.db, opts)
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
		// A run that starts a review loop is ticked at once, so its
		// first pass does not wait for the timer.
		runs := deps.bus.Subscribe(events.EventWorkflowChanged)
		go func() {
			defer deps.bus.Unsubscribe(moved)
			defer deps.bus.Unsubscribe(runs)
			reviewPassWake(ctx, moved, runs, reviewPassEvery, reviewPassRunGap, tick)
		}()
	}
	log.Printf("review passes: on, reviewing as %s with %s", deps.cfg.ReviewPasses.GitHubCredential, deps.cfg.ReviewPasses.Reviewer.Agent)
	return reviews, cancel
}

// wireHandoffReviewCaller registers the handoff caller with the engine: a
// worker's reviewed handoffs' Done waits for a clean review.
func wireHandoffReviewCaller(deps integrationDeps, reviews *service.ReviewPassService, handoffs *service.HandoffWorkerService) {
	if reviews == nil || deps.board == nil {
		return
	}
	var fetch func(ctx context.Context, repo string, number int) error
	if deps.repos != nil {
		// A layer of the stack the author's clone lacks is fetched into
		// the cache its objects come from.
		fetch = deps.repos.PullHead
	}
	handoffs.SetReviews(service.NewHandoffReviewCaller(reviews, service.HandoffReviewCallerOptions{
		Board: deps.board, Bus: deps.bus, FetchPR: fetch,
		// A dead author is resumed, with everything it had, to hear of its
		// feedback.
		ResumeAuthor: handoffs.ResumeAuthor,
	}))
}
