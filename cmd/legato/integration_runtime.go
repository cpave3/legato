package main

import (
	"context"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/worktree"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// integrationDeps is what the integration routes need from a host runtime.
type integrationDeps struct {
	cfg       *config.Config
	db        *store.Store
	bus       *events.Bus
	agents    service.AgentService
	board     service.BoardService
	approvals *approvalRuntime
}

// wireIntegrations gives a web server what integrations (Argus) use on
// /api/integrations/: scoped tokens, owner lookup and, with
// integrations.argus configured, delegations. It is called for both web
// servers. See docs/ai/integrations.md.
func wireIntegrations(srv *server.Server, deps integrationDeps) (stop func()) {
	srv.SetIntegrationAuth(service.NewIntegrationTokenService(deps.db))
	srv.SetOwnerLookup(service.NewOwnerLookupService(deps.db, deps.agents, deps.cfg.Web.PublicURL))
	ctx, cancel := context.WithCancel(context.Background())
	if delegations := newDelegationService(deps); delegations != nil {
		srv.SetDelegations(delegations)
		// A delegation a stopped Legato accepted and never delivered is
		// delivered by the one that serves the broker.
		if deps.approvals != nil && deps.approvals.brokerOwner != nil {
			go whileBrokerOwner(ctx, deps.approvals.brokerOwner, time.Minute, func(ctx context.Context) {
				delegations.ResumeStale(ctx, 5*time.Minute)
			})
		}
	}
	return cancel
}

// newDelegationService is the Argus integration's delegation service, or nil
// when integrations.argus is not configured.
func newDelegationService(deps integrationDeps) *service.DelegationService {
	argus := deps.cfg.Integrations.Argus
	if argus == nil || deps.board == nil {
		return nil
	}
	agents, ok := deps.agents.(service.DelegationAgents)
	if !ok {
		return nil
	}
	opts := service.DelegationOptions{
		Delivery:     argus.Delivery,
		Agent:        argus.Agent,
		Repositories: delegationRepositories(deps.cfg, deps.db),
		Bus:          deps.bus,
	}
	if deps.cfg.Worktrees.Yggdrasil.Enabled {
		opts.Worktrees = service.NewWorktreeWorkflow(deps.db, worktree.NewRunner("yg"), checkoutInspector())
	}
	if deps.approvals != nil {
		opts.Approvals = deps.approvals.approvals
	}
	svc := service.NewDelegationService(deps.db, agents, deps.board, opts)
	svc.RegisterResolver()
	return svc
}

// delegationRepositories is the repository catalog, with each repository's
// first workspace resolved to its ID.
func delegationRepositories(cfg *config.Config, db *store.Store) []service.DelegationRepository {
	workspaces, _ := db.ListWorkspaces(context.Background())
	var repos []service.DelegationRepository
	for _, r := range cfg.Repositories {
		if r.Repo == "" || r.PrimaryDir == "" {
			continue
		}
		repo := service.DelegationRepository{Repo: r.Repo, PrimaryDir: r.PrimaryDir, BaseBranch: r.BaseBranch}
		if len(r.Workspaces) > 0 {
			for _, w := range workspaces {
				if strings.EqualFold(w.Name, r.Workspaces[0]) {
					id := w.ID
					repo.WorkspaceID = &id
					break
				}
			}
		}
		repos = append(repos, repo)
	}
	return repos
}

// newCaseReplyService is the service that sends agents' replies to Argus,
// or nil without integrations.argus. sending is false in a process that only
// records replies for the host to send (legato mcp serve without a broker).
func newCaseReplyService(cfg *config.Config, db *store.Store, bus *events.Bus, sending bool) *service.CaseReplyService {
	argus := cfg.Integrations.Argus
	if argus == nil {
		return nil
	}
	return service.NewCaseReplyService(db, service.CaseReplyOptions{
		Integration: "argus",
		URL:         argus.URL,
		Secrets:     service.SealedIntegrationSecrets{Store: configuredSecretStore(cfg, db)},
		Bus:         bus,
		NoSending:   !sending,
	})
}

// whileBrokerOwner runs work every interval while this process serves the
// broker, so exactly one Legato sends replies and finishes delegations.
func whileBrokerOwner(ctx context.Context, owner func() bool, every time.Duration, work func(context.Context)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if owner() {
				work(ctx)
			}
		}
	}
}
