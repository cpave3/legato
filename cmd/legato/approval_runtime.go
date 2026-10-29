package main

import (
	"context"
	"net"
	"sync"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/discovery"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
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
