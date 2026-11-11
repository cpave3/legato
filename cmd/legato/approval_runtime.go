package main

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/discovery"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// approvalRuntime is the approval service of one Legato process, with the
// watcher that turns native agents' requests into approvals.
type approvalRuntime struct {
	approvals *service.ApprovalService
	// caseReplies sends agents' replies to Argus; nil without it.
	caseReplies *service.CaseReplyService
	// brokerOwner says whether this process serves the broker now.
	brokerOwner func() bool
	devices     *service.DeviceService
	watcher     *service.ApprovalWatcher
	credentials *service.CredentialBroker
	// caseAttention keeps Argus's cases in Attention; set by the first
	// Attention service registered. Every Attention service shares the
	// database and the bus, so any of them serves the routes.
	caseAttention *service.CaseAttentionService
	stop          func()
	// channels are the credential channels this process built, nil
	// without a credential.
	channels *service.CredentialChannels
	// reviews runs review passes on this worker's handoffs, once wired
	// (wireTaskHandoffs); submit_review reaches it through the broker.
	reviews *service.ReviewPassService

	cfg  *config.Config
	mu   sync.Mutex
	port string
}

// startApprovals builds the approval service both runtimes share, with the
// credential broker that applies decisions on credential requests. agents
// may be nil (no tmux); then agent requests are not watched. The
// notification link uses SetServerURL's address, or web.public_url.
func startApprovals(ctx context.Context, cfg *config.Config, db *store.Store, bus *events.Bus, agents service.AgentService) *approvalRuntime {
	rt := &approvalRuntime{cfg: cfg, stop: func() {}}
	rt.approvals = newApprovalService(cfg, db, bus, rt.url)
	// Devices that sign decisions; only this host process checks them.
	rt.devices = service.NewDeviceService(db, rt.approvals, service.DeviceServiceOptions{AndroidDigests: cfg.ApprovalDevices.AndroidAppDigestBytes()})
	stops := []func(){}
	var stopCredentials func()
	rt.credentials, stopCredentials = startCredentials(ctx, cfg, db, bus, rt.approvals, agents)
	stops = append(stops, stopCredentials)
	dropSecretEnv()
	if rt.credentials != nil {
		var reconcile func(context.Context) error
		if agents != nil {
			reconcile = agents.ReconcileSessions
		}
		host := startChannelHost(ctx, cfg, db, rt.credentials, bus, ipc.SocketDir(), reconcile)
		stops = append(stops, host.Stop)
		giveSessionsChannels(agents, host.Channels())
		rt.channels = host.Channels()
	}
	// The broker agent-side processes ask instead of the store.
	rt.caseReplies = newCaseReplyService(cfg, db, bus, true)
	changeSets := startChangeSets(ctx, cfg, db, rt.approvals, agents, nil)
	processSpeech = newReeveVoice(cfg, db, rt.credentials, bus)
	deps := hostBrokerDeps(cfg, db, agents, rt.credentials, rt.caseReplies, rt.reviewPasses)
	broker := startBrokerHost(ctx, db, ipc.SocketDir(), brokerHostOptions{Deps: deps})
	stops = append(stops, broker.Stop)
	rt.brokerOwner = broker.Owner
	// Approved change sets are run by the process that serves the broker.
	go whileBrokerOwner(ctx, broker.Owner, 2*time.Second, func(ctx context.Context) {
		changeSets.RunQueued(ctx)
		changeSets.ReportRestarts(ctx, processStart, version.String())
	})
	if rt.caseReplies != nil {
		// Replies are sent by the process that serves the broker.
		repliesCtx, stopReplies := context.WithCancel(ctx)
		go whileBrokerOwner(repliesCtx, broker.Owner, 15*time.Second, rt.caseReplies.SendDue)
		stops = append(stops, stopReplies)
	}
	if agents != nil {
		giveSessionsBroker(agents, db, broker.SocketPath())
	}
	rt.stop = func() {
		for _, stop := range stops {
			stop()
		}
	}
	if agents == nil {
		return rt
	}
	rt.watcher = service.NewApprovalWatcher(rt.approvals, agents, service.ApprovalWatcherOptions{
		Sessions: db.ListAgentSessions,
		TaskTitle: func(ctx context.Context, taskID string) string {
			if task, err := db.GetTask(ctx, taskID); err == nil {
				return task.Title
			}
			return ""
		},
	})
	stops = append(stops, rt.watcher.Start(ctx, bus))
	return rt
}

// SetServerURL records the port this process's web server listens on, for
// the link in approval notifications.
func (rt *approvalRuntime) SetServerURL(_ string, port string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.port = port
}

func (rt *approvalRuntime) url() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return approvalServerURL(rt.cfg, rt.port, discovery.ReachableIPv4)
}

// config is the configuration this process runs with; an empty one
// without a runtime.
func (rt *approvalRuntime) config() *config.Config {
	if rt == nil || rt.cfg == nil {
		return &config.Config{}
	}
	return rt.cfg
}

// serverURL gives this server's address for a phone, as the approval
// notifications' links use it; nil without a runtime.
func (rt *approvalRuntime) serverURL() func() string {
	if rt == nil || rt.cfg == nil {
		return nil
	}
	return rt.url
}

// approvalServerURL is the address a phone uses for this server, for the
// link in an approval notification: web.public_url, else the TLS host name,
// else the first local network address. port is the one the web server
// listens on, when known; else web.port.
func approvalServerURL(cfg *config.Config, port string, ips func() []string) string {
	if cfg.Web.PublicURL != "" {
		return cfg.Web.PublicURL
	}
	if port == "" {
		port = cfg.Web.Port
	}
	if port == "" {
		port = "3080"
	}
	scheme := "http"
	if tlsEnabled(cfg) {
		scheme = "https"
	}
	if tlsEnabled(cfg) && cfg.Web.TLS.Hostname != "" {
		return scheme + "://" + net.JoinHostPort(cfg.Web.TLS.Hostname, port)
	}
	if list := ips(); len(list) > 0 {
		return scheme + "://" + net.JoinHostPort(list[0], port)
	}
	return ""
}

// attach gives a web server the approvals API and the agents' events.
func (rt *approvalRuntime) attach(srv *server.Server) {
	srv.SetApprovalService(rt.approvals)
	srv.SetDeviceService(rt.devices)
	srv.StartApprovalEvents()
	if rt.credentials != nil {
		srv.SetCredentialBroker(rt.credentials)
	}
	if rt.watcher != nil {
		srv.UseAgentEvents(rt.watcher)
	}
}

// registerApprovalAttention shows pending approvals in Attention.
func registerApprovalAttention(attention *service.AttentionService, rt *approvalRuntime) {
	if rt == nil || rt.approvals == nil {
		return
	}
	adapter := service.NewApprovalAttentionAdapter(rt.approvals)
	mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventApprovalChanged))
	mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	// A device reset at the host; `legato device reset` sends approval_changed.
	if rt.devices != nil {
		mustRegister(attention.RegisterProducerWithEvents(service.NewDeviceAttentionAdapter(rt.devices), events.EventApprovalChanged))
	}
	// What went wrong between Legato and Argus.
	if rt.cfg != nil && rt.cfg.Integrations.Argus != nil {
		mustRegister(attention.RegisterProducerWithEvents(service.NewIntegrationAttentionAdapter(rt.approvals.Store()), events.EventDelegationChanged))
	}
	// Argus's cases. Registered without Argus too, so that its items are
	// resolved when it is removed from the configuration.
	cases := newCaseAttentionService(rt.cfg, rt.approvals.Store(), attention)
	mustRegister(attention.RegisterProducerWithEvents(cases.Producer(), events.EventPRStatusUpdated, events.EventCardsRefreshed))
	if rt.caseAttention == nil {
		rt.caseAttention = cases
	}
}

// newCaseAttentionService takes Argus's cases into an Attention service;
// without integrations.argus it refuses them and shows none.
func newCaseAttentionService(cfg *config.Config, db *store.Store, attention *service.AttentionService) *service.CaseAttentionService {
	opts := service.CaseAttentionOptions{Attention: attention, Owners: service.NewOwnerLookupService(db, nil, "")}
	if cfg != nil && cfg.Integrations.Argus != nil {
		opts.Origin = cfg.Integrations.Argus.URL
	}
	return service.NewCaseAttentionService(db, opts)
}

// setReviewPasses keeps the review loop for submit_review.
func (rt *approvalRuntime) setReviewPasses(reviews *service.ReviewPassService) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reviews = reviews
}

// reviewPasses is the review loop, nil until it is wired.
func (rt *approvalRuntime) reviewPasses() cli.MCPReviews {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.reviews == nil {
		return nil
	}
	return rt.reviews
}

// hostBrokerDeps is what the host's broker serves agent-side processes:
// Legato's MCP tools, forwarded commands of the sandbox, and, with a
// credential broker, the values `legato credential get` and `exec` ask for.
func hostBrokerDeps(cfg *config.Config, db *store.Store, agents service.AgentService, credentials *service.CredentialBroker, caseReplies *service.CaseReplyService, reviews func() cli.MCPReviews) cli.BrokerDeps {
	deps := cli.BrokerDeps{Push: agentPushNotifier(cfg), OS: agentOSNotifier(cfg), ClaudeModels: cfg.Adapters["claude-code"].Models}
	deps.Ops = cli.MCPBrokerOps(func(cli.BrokerCaller) cli.MCPOptions {
		opts := cli.MCPOptions{Store: db, Version: version.String(), ResourceDomains: cfg.Agents.MCP.ResourceDomains,
			ImageDir: agentUIImageRoot(cfg), VideoDir: agentUIVideoRoot(cfg), Stacks: hostStackSupport()}
		if peers, ok := agents.(cli.MCPPeers); ok && agents != nil {
			opts.Agents = peers
		}
		if credentials != nil {
			opts.Credentials = credentials
		}
		if caseReplies != nil {
			opts.CaseReplies = caseReplies
		}
		if reviews != nil {
			opts.Reviews = reviews()
		}
		opts.Reeve = reeveTools(cfg, db)
		return opts
	})
	// Agents in the sandbox run their own `legato` commands here.
	deps.Ops["cli.run"] = cliRunOp(legatoBinary(), *sandboxScratch(cfg), func(ctx context.Context, taskID string) bool {
		return service.IsReeveTask(ctx, db, taskID)
	})
	// Values come from this broker, whose stores (1Password) stay signed in.
	if credentials != nil {
		for name, op := range cli.CredentialBrokerOps(credentials) {
			deps.Ops[name] = op
		}
	}
	return deps
}
