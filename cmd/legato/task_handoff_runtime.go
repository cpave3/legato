package main

import (
	"context"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// handoffReconcileEvery is how often a worker brings its handoffs' states in
// line with their cards, besides when a card moves in this process: a card
// moved by another process (legato task update) may tell this one nothing.
const handoffReconcileEvery = 30 * time.Second

// sandboxAvailable says whether this Legato's agents can run in its sandbox:
// the configuration and bwrap on PATH, not the whole sandbox plan.
func sandboxAvailable(cfg *config.Config) bool {
	return cfg.Agents.Sandbox.Enabled && (sandbox.Bwrap{}).Available() == nil
}

// wireTaskHandoffs lets the main instances paired with this Legato hand it
// tasks (POST /api/integrations/handoff/tasks): it makes their cards and
// starts their agents, sandboxed and unattended, in clones of their
// repositories. While this process serves the broker, so one Legato does it,
// it starts the agents of handoffs accepted and never started. See
// docs/ai/integrations.md § Handing off a task.
func wireTaskHandoffs(srv *server.Server, deps integrationDeps) (stop func()) {
	agents, ok := deps.agents.(service.HandoffAgents)
	if !ok || deps.db == nil {
		return func() {}
	}
	opts := service.HandoffWorkerOptions{
		Agents:  agents,
		Sandbox: func() bool { return sandboxAvailable(deps.cfg) },
		Bus:     deps.bus,
		// Step 1 of a return gives back the stack an agent built.
		Stacks: service.NewTaskStackService(deps.db, nil),
	}
	if deps.repos != nil {
		opts.Repositories = deps.repos
	}
	// A make-a-change handoff's session may push its branches and open
	// draft pull requests without asking.
	if deps.approvals != nil && deps.approvals.credentials != nil {
		opts.Grants = deps.approvals.credentials
	}
	handoffs := service.NewHandoffWorkerService(deps.db, opts)
	srv.SetTaskHandoffs(handoffs)
	addInterruptedResumeRoute(deps.agents, handoffs.ResumeInterrupted)
	stopReviews := wireReviewPasses(deps, handoffs)
	ctx, cancel := context.WithCancel(context.Background())
	stopLoop := handoffs.StartEventLoop(ctx, handoffReconcileEvery)
	if deps.approvals != nil && deps.approvals.brokerOwner != nil {
		owner := deps.approvals.brokerOwner
		go func() {
			if owner() {
				handoffs.ResumePending(ctx)
			}
			whileBrokerOwner(ctx, owner, time.Minute, handoffs.ResumePending)
		}()
	}
	return func() {
		stopReviews()
		stopLoop()
		cancel()
	}
}
