package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"gopkg.in/yaml.v3"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/auth"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/setup"
)

// webSetup is setup's step for the web UI on this machine. On a first run
// (firstRun) the TUI starts the web server once setup is done, so pairing
// waits for it (setup.ErrPairOnStart).
type webSetup struct {
	cfg      *config.Config
	firstRun bool
	out      io.Writer
}

func (w webSetup) State() (bool, bool) {
	return w.cfg.Web.Enabled, w.cfg.Pinned("web.enabled")
}

func (w webSetup) Enable(context.Context) (string, error) {
	path := config.ResolveConfigPath()
	if err := enableWebInConfig(path); err != nil {
		return "", err
	}
	w.cfg.Web.Enabled = true
	if w.firstRun {
		return "web.enabled: true is in " + path + ". The web UI starts with Legato.", nil
	}
	return "web.enabled: true is in " + path + ". A running Legato starts the web UI at its next start (legato restart).", nil
}

// enableWebInConfig writes web.enabled: true into the config.yaml at path,
// keeping its comments and every other setting; a missing file is made as
// the first run makes it.
func enableWebInConfig(path string) error {
	if _, err := config.WriteFirstConfig(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if _, ok := doc["web"]; ok {
		// A web block of its own: edited in place, keeping its comments.
		_, err := service.WriteConfigEdit(path, []service.ConfigEdit{{Key: "web.enabled", Value: true}})
		return err
	}
	// No web block: one is added at the end, and the file is otherwise
	// left byte for byte (a comments-only file has nothing to edit).
	text := string(data)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return os.WriteFile(path, []byte(text+"web:\n  enabled: true\n"), 0o600)
}

func (w webSetup) Address() string {
	_, address := webPairingAddress(w.cfg)
	return address
}

// webPairingAddress is the scheme of this machine's web server and the
// address a phone uses for it, as legato pair works them out.
func webPairingAddress(cfg *config.Config) (scheme, address string) {
	port := cfg.Web.Port
	if port == "" {
		port = "3080"
	}
	host := cfg.Web.TLS.Hostname
	if host == "" {
		host, _ = os.Hostname()
	}
	if host == "" {
		host = "localhost"
	}
	scheme = "http"
	if tlsEnabled(cfg) {
		scheme = "https"
	}
	return scheme, scheme + "://" + net.JoinHostPort(host, port)
}

func (w webSetup) Pair(ctx context.Context) error {
	if w.firstRun {
		return setup.ErrPairOnStart
	}
	return pairPhoneHere(ctx, w.cfg, w.out)
}

// pairPhoneHere opens a pairing request on the web server running on this
// machine, shows its QR code and phrase, and waits until a phone pairs, the
// phrase expires, or Ctrl-C.
func pairPhoneHere(ctx context.Context, cfg *config.Config, out io.Writer) error {
	token, err := auth.ReadToken(resolveDataDir(cfg))
	if err != nil {
		return err
	}
	scheme, address := webPairingAddress(cfg)
	port := cfg.Web.Port
	if port == "" {
		port = "3080"
	}
	phrase := localPhrasePairing(scheme, port, token, filepath.Join(resolveDataDir(cfg), "certs", "ca.pem"))
	opened, err := phrase.open(ctx)
	if err != nil {
		return errors.New("no Legato web server answers on this machine; once Legato runs with the web UI, run legato pair")
	}
	qrterminal.GenerateWithConfig(phraseQR(opened, "", address), qrterminal.Config{Level: qrterminal.L, Writer: out, QuietZone: 1})
	fmt.Fprintf(out, "\nScan this in the Legato app, or add a server there and type\n\n    %s\n\n", formatPhrase(opened.Phrase))
	fmt.Fprintf(out, "Valid for %d minutes. Waiting for a phone… (Ctrl-C to go on without one)\n", int(service.PairingLifetime/time.Minute))
	waitCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ended, err := phrase.wait(waitCtx)
	if err != nil {
		phrase.cancel()
		fmt.Fprintln(out, "\nPairing cancelled. Later: legato pair, or Settings › Devices.")
		return nil
	}
	message, _ := pairingOutcome(ended.PairingStatus)
	fmt.Fprintln(out, message)
	return nil
}
