package main

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/egress"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// mcpBrokerRuntime is the host's side of the operator's MCP connections:
// the registry, OAuth flows, tool grants and the broker that calls upstream
// tools for agents. Only the host process that builds the credential broker
// builds it; agent-side processes reach it through the host broker.
type mcpBrokerRuntime struct {
	conns  *service.MCPConnectionService
	flows  *service.OAuthFlowService
	broker *service.MCPBroker
}

// mcpEgress adjusts every connection's egress policy; nil in production.
// Tests set it to reach fixture servers on the loopback address.
var mcpEgress func(egress.Policy) egress.Policy

// mcpSweepEvery is how often abandoned OAuth attempts are reconciled.
const mcpSweepEvery = 5 * time.Minute

// startMCPBroker builds the MCP broker over the credential broker; nil
// without one (its grants are the credential broker's).
func startMCPBroker(cfg *config.Config, db *store.Store, credentials *service.CredentialBroker) (*mcpBrokerRuntime, func()) {
	if credentials == nil {
		log.Printf("mcp: the credential broker is unavailable, so MCP connections are not offered to agents")
		return nil, func() {}
	}
	secretStore := configuredSecretStore(cfg, db)
	secrets := service.SealedIntegrationSecrets{Store: secretStore}
	conns := service.NewMCPConnectionService(db, service.MCPConnectionOptions{Secrets: secrets, Egress: mcpEgress})
	flows := service.NewOAuthFlowService(db, secrets, service.OAuthFlowOptions{Connections: conns, SecretSource: secretStore.Source})
	conns.SetOAuth(flows)
	grants := service.NewMCPGrants(credentials, conns, db)
	conns.SetGrants(grants)
	rt := &mcpBrokerRuntime{conns: conns, flows: flows, broker: service.NewMCPBroker(conns, grants, flows)}
	return rt, func() {
		rt.broker.Close()
		if err := flows.Close(); err != nil {
			log.Printf("mcp: closing OAuth receivers: %v", err)
		}
	}
}

// sweep reconciles abandoned OAuth attempts at once and then periodically,
// in the process that serves the broker.
func (rt *mcpBrokerRuntime) sweep(ctx context.Context, owner func() bool) {
	run := func(ctx context.Context) {
		if err := rt.flows.Sweep(ctx); err != nil {
			log.Printf("mcp: reconciling OAuth attempts: %v", err)
		}
	}
	if owner() {
		run(ctx)
	}
	whileBrokerOwner(ctx, owner, mcpSweepEvery, run)
}

// upstream is the broker as the host's MCP tools see it; nil without one.
func (rt *mcpBrokerRuntime) upstream() cli.MCPUpstream {
	if rt == nil {
		return nil
	}
	return rt.broker
}

// attach serves the operator's MCP connections and their authorization on
// a web server; nothing without a runtime.
func (rt *mcpBrokerRuntime) attach(srv *server.Server) {
	if rt == nil {
		return
	}
	srv.SetMCPConnectionService(rt.conns)
	srv.SetMCPOAuth(rt.flows)
}
