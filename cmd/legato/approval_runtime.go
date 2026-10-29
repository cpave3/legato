package main

import (
	"context"
	"net"
	"sync"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/discovery"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// approvalRuntime is the approval service of one Legato process, with the
// watcher that turns native agents' requests into approvals.
type approvalRuntime struct {
	approvals   *service.ApprovalService
	watcher     *service.ApprovalWatcher
	credentials *service.CredentialBroker
	stop        func()

	mu        sync.Mutex
	serverURL string
}

// startApprovals builds the approval service both runtimes share, with the
// credential broker that applies decisions on credential requests. agents
// may be nil (no tmux); then agent requests are not watched. The
// notification link uses SetServerURL's address, or web.public_url.
func startApprovals(ctx context.Context, cfg *config.Config, db *store.Store, bus *events.Bus, agents service.AgentService) *approvalRuntime {
	rt := &approvalRuntime{serverURL: cfg.Web.PublicURL, stop: func() {}}
	rt.approvals = newApprovalService(cfg, db, bus, rt.url)
	stops := []func(){}
	var stopCredentials func()
	rt.credentials, stopCredentials = startCredentials(ctx, cfg, db, bus, rt.approvals, agents)
	stops = append(stops, stopCredentials)
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

// SetServerURL sets the address a phone uses for this process's web server,
// unless web.public_url names one.
func (rt *approvalRuntime) SetServerURL(scheme, port string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.serverURL != "" {
		return
	}
	if ips := discovery.ReachableIPv4(); len(ips) > 0 {
		rt.serverURL = scheme + "://" + net.JoinHostPort(ips[0], port)
	}
}

func (rt *approvalRuntime) url() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.serverURL
}

// attach gives a web server the approvals API and the agents' events.
func (rt *approvalRuntime) attach(srv *server.Server) {
	srv.SetApprovalService(rt.approvals)
	srv.StartApprovalEvents()
	if rt.credentials != nil {
		srv.SetCredentialBroker(rt.credentials)
	}
	if rt.watcher != nil {
		srv.UseAgentEvents(rt.watcher)
	}
}

// registerApprovalAttention shows pending approvals in Attention.
func registerApprovalAttention(attention *service.AttentionService, approvals *service.ApprovalService) {
	if approvals == nil {
		return
	}
	adapter := service.NewApprovalAttentionAdapter(approvals)
	mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventApprovalChanged))
	mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
}
