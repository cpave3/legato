package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/version"
	"github.com/cpave3/legato/internal/service"
)

// restartHealthTimeout is how long a start after a restart has to be
// healthy before the restart is rolled back.
const restartHealthTimeout = 30 * time.Second

// configAtStart is config.yaml as this process loaded it. A restart that
// finds the file changed keeps this as the backup it can roll back to.
var configAtStart []byte

// loadConfig loads the configuration at the start of the TUI or `legato
// serve`. When it does not load and a pending restart changed the file, the
// old file is put back and this process starts again on it.
func loadConfig() (*config.Config, error) {
	path := config.ResolveConfigPath()
	// A first run writes a config.yaml that pins nothing, pointing to
	// Settings.
	if wrote, err := config.WriteFirstConfig(path); err != nil {
		fmt.Fprintf(os.Stderr, "config: writing %s: %v\n", path, err)
	} else if wrote {
		fmt.Fprintf(os.Stderr, "Wrote %s. Most settings are changed in Settings in the web UI.\n", path)
	}
	cfg, err := config.Load()
	if err != nil {
		rolled, rbErr := service.RollBackUnloadable(context.Background(), path, err, nil)
		if rbErr != nil {
			fmt.Fprintf(os.Stderr, "restart rollback: %v\n", rbErr)
		}
		if rolled {
			fmt.Fprintf(os.Stderr, "config: %v\nThe restart is rolled back to the configuration before it; starting again.\n", err)
			if execErr := reexec(); execErr != nil {
				fmt.Fprintln(os.Stderr, execErr)
			}
		}
		return nil, err
	}
	configAtStart, _ = os.ReadFile(path)
	return cfg, nil
}

// restartRollback is the rollback of this instance's restarts.
func restartRollback(cfg *config.Config, db *store.Store) *service.RestartRollback {
	resolved, _ := config.ResolveDatabase(cfg)
	return &service.RestartRollback{Store: db, Settings: settingsFor(db), DBDriver: resolved.Driver, DBPath: resolved.Path}
}

// prepareRestart records a restart before this process restarts, when
// config.yaml changed since it started: the file it started on becomes the
// backup, so a start that fails on the new file comes back on the old one.
// The first process of the instance to record it wins; the others find it
// pending.
func prepareRestart(rb *service.RestartRollback, reason string) {
	path := config.ResolveConfigPath()
	now, err := os.ReadFile(path)
	if err != nil || configAtStart == nil || string(now) == string(configAtStart) {
		return
	}
	backup := fmt.Sprintf("%s.%s.bak", path, time.Now().UTC().Format("20060102T150405"))
	if err := os.WriteFile(backup, configAtStart, 0o600); err != nil {
		log.Printf("restart: backing up config.yaml: %v", err)
		return
	}
	if _, err := rb.Begin(context.Background(), service.RestartPlan{Reason: reason, Version: version.String(), ConfigPath: path, ConfigBackup: backup}); err != nil && !errors.Is(err, service.ErrRestartPending) {
		log.Printf("restart: recording it: %v", err)
	}
}

// settleRestart decides a pending restart once this process is up: the web
// server answering /health (when this process serves one) and the broker
// answering. Rolled back, this process restarts on the old configuration.
func settleRestart(ctx context.Context, rb *service.RestartRollback, healthURL string) {
	outcome, err := rb.Settle(ctx, config.ResolveConfigPath(), func(ctx context.Context) error {
		return checkHealth(ctx, healthURL)
	}, restartHealthTimeout, 500*time.Millisecond)
	switch {
	case err != nil:
		log.Printf("restart: settling: %v", err)
	case outcome == store.RestartRolledBack:
		log.Printf("restart: not healthy in time; rolled back, starting again on the old configuration")
		requestRestart()
	case outcome == store.RestartApplied:
		log.Printf("restart: healthy; the restart is applied")
	}
}

// rollBackFailedStart is run when this process ends: when it was the TUI or
// `legato serve` (args) and it exited with an error (code) while a restart
// was pending, it failed to start (its port taken, say) before the health
// check could roll the restart back, so it rolls it back now, in the
// database the pointer names, and starts again (again). It reports whether
// it did.
func rollBackFailedStart(args []string, code int, configPath string, again func() error) bool {
	if code == 0 || code == exitRestart || (len(args) > 0 && args[0] != "serve") {
		return false
	}
	rolled, err := (&service.RestartRollback{}).RollBackFailedStart(context.Background(), configPath, fmt.Errorf("it exited with status %d before it was healthy; see the log", code))
	if err != nil {
		fmt.Fprintf(os.Stderr, "restart rollback: %v\n", err)
	}
	if !rolled {
		return false
	}
	fmt.Fprintln(os.Stderr, "Legato did not start after the restart; it is rolled back to the configuration before it. Starting again.")
	if err := again(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return true
}

// checkHealth is healthy when healthURL (if any) answers 200 and the broker
// answers at its socket, whichever process serves it.
func checkHealth(ctx context.Context, healthURL string) error {
	if healthURL != "" {
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("/health did not answer: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("/health answered %d", resp.StatusCode)
		}
	}
	socket := filepath.Join(ipc.SocketDir(), brokerSocketDir, brokerSocketName)
	err := broker.Call(broker.WithoutWaiting(ctx), socket, "", "health", nil, nil)
	var refused *broker.Error
	if err != nil && !errors.As(err, &refused) {
		return fmt.Errorf("the broker did not answer: %w", err)
	}
	return nil
}
