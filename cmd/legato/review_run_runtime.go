package main

import (
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// attachReviewRuns registers the run caller with the review-pass engine and
// attaches it to the workflow service's github.pr.review_passes step: review
// loops on any pull request run on this instance, with its reviewer App.
// Nil without workflows or a repository cache.
func attachReviewRuns(deps integrationDeps, reviews *service.ReviewPassService) *service.ReviewRunCaller {
	if reviews == nil || deps.workflows == nil || deps.repos == nil {
		return nil
	}
	step, ok := deps.workflows.Executor(service.ReviewPassesStepType).(*service.ReviewPassesStep)
	if !ok {
		return nil
	}
	var discover service.ReviewRunDiscover
	if d, ok := deps.prs.(service.ReviewRunDiscover); ok {
		discover = d
	}
	caller := service.NewReviewRunCaller(reviews, service.ReviewRunOptions{
		Discover:   discover,
		GitHub:     learningPublisher(deps.db, service.NewReviewPublisher(deps.approvals.channels, deps.cfg.ReviewPasses.GitHubCredential)),
		Clones:     service.ReviewRunRepositories{Repos: deps.repos, Checkout: service.GitReviewCheckout{Store: deps.db}},
		Owners:     service.NewOwnerLookupService(deps.db, deps.agents, deps.cfg.Web.PublicURL),
		SummaryDir: filepath.Join(workflowArtifactRoot(deps.cfg), "review-summaries"),
	})
	caller.SetWorkflows(deps.workflows)
	step.Attach(caller)
	if steps := service.ReviewLoopStepsOf(deps.workflows.Executor(service.ReviewOpenStepType)); steps != nil {
		steps.Attach(caller)
	}
	return caller
}

// wireReviewRuns gives a web server the run caller, for the run page's
// review loop.
func wireReviewRuns(srv *server.Server, engine *reviewEngine) {
	if engine == nil || engine.runs == nil {
		return
	}
	srv.SetReviewRuns(engine.runs)
}

// reviewAuthors derives the agent_author the card menu and the TUI start
// review-loop with (spec LEG-559 D8), from what PR tracking keeps of who
// opened each pull request and Legato's own App logins.
func reviewAuthors(cfg *config.Config, db *store.Store) *service.ReviewAuthors {
	return service.NewReviewAuthors(db, cfg.GitHub.AppLogins)
}
