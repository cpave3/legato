package main

import (
	"net"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireAgentUI gives a web server the API over what agents show (artifacts and
// cards) and has it tell web clients when that changes. Both the standalone
// server and the one the TUI starts call it, so neither can drift.
func wireAgentUI(srv *server.Server, cfg *config.Config, db *store.Store, bus *events.Bus) {
	srv.SetAgentUIService(service.NewAgentUIService(db, bus, service.AgentUIOptions{ResourceDomains: cfg.Agents.MCP.ResourceDomains}))
	srv.StartAgentUIEvents()
}

// localWebURL is where this machine serves the web UI, for a link the TUI
// shows and opens in a browser on the same machine. It is empty when the web
// UI is not enabled: there is then nothing the TUI can promise to link to.
//
// With web.enabled the address holds whether this process serves it or
// another Legato already had the port.
func localWebURL(cfg *config.Config) string {
	if !cfg.Web.Enabled {
		return ""
	}
	scheme, host := "http", "localhost"
	if tlsEnabled(cfg) {
		scheme = "https"
		// The certificate names this host; the browser checks it.
		if cfg.Web.TLS.Hostname != "" {
			host = cfg.Web.TLS.Hostname
		}
	}
	port := cfg.Web.Port
	if port == "" {
		port = "3080"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}
