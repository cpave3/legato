package main

import (
	"errors"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/discovery"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wirePairing lets phones pair with srv by phrase. A paired phone gets the
// token srv checks, this machine's name, and the certificate to
// trust for HTTPS: the generated CA, or the configured certificate.
func wirePairing(srv *server.Server, cfg *config.Config, caCertFile, port string) {
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
	srv.SetPairingService(service.NewPairingService(func() (service.PairingSecret, error) {
		token := srv.AuthToken()
		if token == "" {
			return service.PairingSecret{}, errors.New("this server has no token")
		}
		secret := service.PairingSecret{Token: token, Name: pairingName(cfg)}
		if certFile != "" {
			pem, err := os.ReadFile(certFile)
			if err != nil {
				return service.PairingSecret{}, err
			}
			secret.CACertificate = string(pem)
		}
		return secret, nil
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
