package main

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/discovery"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wirePairing lets phones and main instances pair with srv by phrase. A
// paired phone gets the token srv checks, this machine's name, and the
// certificate to trust for HTTPS: the generated CA, or the configured
// certificate. A main instance gets a token that can only hand off, and
// this server's instance ID; srv then lists it in Settings › Main
// instances. srv also gets Settings › Workers, for this Legato as a main.
// See docs/ai/integrations.md § Main instances and workers.
func wirePairing(srv *server.Server, cfg *config.Config, db *store.Store, caCertFile, port string) {
	scheme := "http"
	if tlsEnabled(cfg) {
		scheme = "https"
	}
	srv.SetPairingAddresses(func() []string {
		var out []string
		for _, ip := range discovery.ReachableIPv4() {
			out = append(out, scheme+"://"+net.JoinHostPort(ip, port))
		}
		return out
	})
	certFile := caCertFile
	if certFile == "" && cfg.Web.TLS.Cert != "" && cfg.Web.TLS.Key != "" {
		certFile = cfg.Web.TLS.Cert
	}
	tokens := service.NewIntegrationTokenService(db)
	instanceID := func(ctx context.Context) (string, error) { return service.InstanceID(ctx, db) }
	secrets := service.PairingSecrets{
		WebToken: srv.AuthToken,
		Name:     pairingName(cfg),
		CACertificate: func() (string, error) {
			if certFile == "" {
				return "", nil
			}
			pem, err := os.ReadFile(certFile)
			return string(pem), err
		},
		Mains:      tokens,
		InstanceID: instanceID,
	}
	pairing := service.NewPairingService(secrets.Secret)
	pairing.AllowMains()
	srv.SetPairingService(pairing)
	srv.SetMainInstances(tokens)
	srv.SetInstanceID(instanceID)
	// Asked at every /api/meta: the configuration and bwrap on PATH, not the
	// whole sandbox plan.
	srv.SetSandboxAvailable(func() bool { return sandboxAvailable(cfg) })
	srv.SetWorkers(service.NewWorkerService(db, service.WorkerServiceOptions{
		Secrets:  configuredSecretStore(cfg, db),
		MainName: pairingName(cfg),
	}))
}

// pairingName is how phones list this machine: its host name without the
// domain ("erebus", not "erebus.example.net").
func pairingName(cfg *config.Config) string {
	if host, err := os.Hostname(); err == nil && host != "" {
		short, _, _ := strings.Cut(host, ".")
		return short
	}
	if cfg.Web.TLS.Hostname != "" {
		return cfg.Web.TLS.Hostname
	}
	return "Legato"
}

// announceWebServer makes the web server findable by the Legato app on the
// local network, unless web.discovery is false. stop ends the announcement.
func announceWebServer(cfg *config.Config, port string) (stop func()) {
	if cfg.Web.Discovery != nil && !*cfg.Web.Discovery {
		return func() {}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 {
		return func() {}
	}
	scheme := "http"
	if tlsEnabled(cfg) {
		scheme = "https"
	}
	stop, err = discovery.Announce(discovery.Service{Name: pairingName(cfg), Port: n, Scheme: scheme})
	if err != nil {
		log.Printf("web: not announced on the local network: %v", err)
		return func() {}
	}
	return stop
}
