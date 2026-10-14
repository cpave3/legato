package main

import (
	"context"
	"net"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/libretto"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireAgentUI gives a web server the API over what agents show (artifacts and
// cards) and has it tell web clients when that changes. Both the standalone
// server and the one the TUI starts call it, so neither can drift. It also
// gives the server the libretto studios behind studio items; the function it
// returns stops the ones that were started, and is called as the server
// stops.
func wireAgentUI(srv *server.Server, cfg *config.Config, db *store.Store, bus *events.Bus) (stop func()) {
	ui := service.NewAgentUIService(db, bus, service.AgentUIOptions{
		ResourceDomains: cfg.Agents.MCP.ResourceDomains, ImageDir: agentUIImageRoot(cfg), VideoDir: agentUIVideoRoot(cfg),
	})
	srv.SetAgentUIService(ui)
	srv.StartAgentUIEvents()
	// Nothing is started here, and libretto need not be installed: a studio
	// is a process started when its panel is first opened.
	studios := service.NewAgentStudioService(ui, libretto.Runner{})
	srv.SetAgentStudioService(studios)
	// Images outlive what used them when a task is deleted or the person
	// removes an artifact. Nothing waits for this, and a failure costs disk
	// space only.
	go func() {
		_ = ui.RemoveOrphanImages(context.Background())
		_ = ui.RemoveOrphanVideos(context.Background())
	}()
	return studios.Close
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
