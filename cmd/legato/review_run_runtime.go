package main

import (
	"path/filepath"

	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireReviewRuns registers the run caller with the review-pass engine and
// attaches it to the workflow service's github.pr.review_passes step: review
// loops on any pull request run on this instance, with its reviewer App.
func wireReviewRuns(srv *server.Server, deps integrationDeps, reviews *service.ReviewPassService) {
	if reviews == nil || deps.workflows == nil || deps.repos == nil {
		return
	}
	step, ok := deps.workflows.Executor(service.ReviewPassesStepType).(*service.ReviewPassesStep)
	if !ok {
		return
	}
	caller := service.NewReviewRunCaller(reviews, service.ReviewRunOptions{
		GitHub:     service.NewReviewPublisher(deps.approvals.channels, deps.cfg.ReviewPasses.GitHubCredential),
		Clones:     service.ReviewRunRepositories{Repos: deps.repos, Checkout: service.GitReviewCheckout{Store: deps.db}},
		Owners:     service.NewOwnerLookupService(deps.db, deps.agents, deps.cfg.Web.PublicURL),
		SummaryDir: filepath.Join(workflowArtifactRoot(deps.cfg), "review-summaries"),
	})
	caller.SetWorkflows(deps.workflows)
	step.Attach(caller)
	srv.SetReviewRuns(caller)
}
