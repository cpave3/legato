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
			RequiredChecks: rp.RequiredChecks, AppLogins: cfg.GitHub.AppLogins,
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

// reviewEngine is this process's review-pass engine, its run caller (the
// github.pr.review_passes step) and the handoff worker whose caller it
// registered, nil fields when review passes are off.
type reviewEngine struct {
	reviews  *service.ReviewPassService
	runs     *service.ReviewRunCaller
	handoffs *service.HandoffWorkerService
	stop     func()
}

// startReviewEngine builds the review-pass engine on an instance with
// review_passes on, in every host process (serve, and the TUI whether or not
// it serves the web UI): it takes submit_review through the broker, runs
// the workflow step, and ticks only while this process serves the broker,
// holding the engine's lock (reviewEngineRunning) meanwhile (spec LEG-559
// D9). Any process may start a review step; the holder's tick opens it. It
// needs the credential channels, which post as the reviewer App. Its two
// callers, the run caller and the handoff caller, are registered here, not
// with the web routes, so a process that holds the broker without the web
// UI moves both on.
func startReviewEngine(deps integrationDeps) *reviewEngine {
	engine := &reviewEngine{stop: func() {}}
	if !deps.cfg.ReviewPasses.Enabled || deps.approvals == nil || deps.approvals.channels == nil {
		return engine
	}
	agents, ok := deps.agents.(service.ReviewAgents)
	if !ok {
		return engine
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
	engine.reviews = reviews
	engine.runs = attachReviewRuns(deps, reviews)
	if engine.handoffs = newHandoffWorker(deps); engine.handoffs != nil {
		wireHandoffReviewCaller(deps, reviews, engine.handoffs, engine.runs != nil)
	}
	deps.approvals.setReviewPasses(reviews)
	if deps.reviewPrompt != nil {
		deps.reviewPrompt.SetReviews(reviews)
		if deps.bus != nil {
			// Attention reconciled before the engine ran: look again.
			deps.bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	owner := deps.approvals.brokerOwner
	lock := &reviewEngineLock{dir: config.ResolveDataDir(deps.cfg)}
	tick := func(ctx context.Context) {
		if owner == nil || owner() {
			lock.hold()
			reviews.Tick(ctx)
		}
	}
	if deps.bus != nil {
		// A refresh of a card's pull requests brings the steward's
		// watches forward (spec LEG-559 C7); ctx's end stops it.
		reviews.StartWatchEvents(ctx, deps.bus)
		moved := deps.bus.Subscribe(events.EventCardMoved)
		// A run that starts a review loop is ticked at once, so its
		// first pass does not wait for the timer.
		runs := deps.bus.Subscribe(events.EventWorkflowChanged)
		go func() {
			defer deps.bus.Unsubscribe(moved)
			defer deps.bus.Unsubscribe(runs)
			// A new holder picks up at once what waits for the engine.
			tick(ctx)
			reviewPassWake(ctx, moved, runs, reviewPassEvery, reviewPassRunGap, tick)
		}()
	}
	engine.stop = func() {
		cancel()
		lock.release()
	}
	log.Printf("review passes: on, reviewing as %s with %s", deps.cfg.ReviewPasses.GitHubCredential, deps.cfg.ReviewPasses.Reviewer.Agent)
	return engine
}

// wireHandoffReviewCaller registers the handoff caller with the engine: a
// worker's reviewed handoffs' Done waits for a clean review. With the run
// caller attached (runs), each reviewed handoff runs the built-in
// review-loop (LEG-600); without it, review passes apply to no handoff
// (LEG-601: no handoff loop moves on outside review-loop).
func wireHandoffReviewCaller(deps integrationDeps, reviews *service.ReviewPassService, handoffs *service.HandoffWorkerService, runs bool) {
	if reviews == nil || deps.board == nil {
		return
	}
	var fetch func(ctx context.Context, repo string, number int) error
	if deps.repos != nil {
		// A layer of the stack the author's clone lacks is fetched into
		// the cache its objects come from.
		fetch = deps.repos.PullHead
	}
	opts := service.HandoffReviewCallerOptions{
		Board: deps.board, Bus: deps.bus, FetchPR: fetch,
		// A dead author is resumed, with everything it had, to hear of its
		// feedback.
		ResumeAuthor: handoffs.ResumeAuthor,
		// A draft rule with ready_after_review marks the approved stack
		// ready (LEG-456), read from the effective rules at each start.
		PullRequests: pullRequestModes(deps.cfg, deps.db),
	}
	if runs && deps.workflows != nil {
		opts.Runs = deps.workflows
	}
	handoffs.SetReviews(service.NewHandoffReviewCaller(reviews, opts))
}
