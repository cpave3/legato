package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/codexusage"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"golang.org/x/sys/unix"
)

// IPC messages of subscription usage: a reading was kept, and a process
// that does not poll asks the one that does to read now.
const (
	ipcUsageChanged = "usage_changed"
	ipcUsageRefresh = "usage_refresh"
)

// claimUsagePoller is the per-data-directory lock of the one process that
// reads the subscriptions' usage, so two never ask a provider twice.
func claimUsagePoller(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dataDir, "usage.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }) }, nil
}

// usageReaders are the providers the settings turn on, Claude first. Both
// read on this machine with the tools' own logins, never in a sandbox.
func usageReaders(cfg *config.Config) []service.SubscriptionUsageReader {
	home, _ := os.UserHomeDir()
	var readers []service.SubscriptionUsageReader
	if cfg.Usage.Claude.IsEnabled() {
		readers = append(readers, &service.ClaudeUsageReader{Client: &claudeusage.Client{
			Credentials: claudeusage.HostCredentials(os.Getenv, home),
		}})
	}
	if cfg.Usage.Codex.IsEnabled() {
		readers = append(readers, &service.CodexUsageReader{
			AppServer:   codexusage.AppServer{Command: "codex"},
			SessionsDir: codexusage.SessionsDir(os.Getenv, home),
		})
	}
	return readers
}

// subscriptionUsageFor serves the subscriptions' usage kept in db; nil with
// both providers off. A kept reading is announced on bus and to the other
// processes.
func subscriptionUsageFor(cfg *config.Config, db *store.Store, bus *events.Bus) *service.SubscriptionUsageService {
	readers := usageReaders(cfg)
	if len(readers) == 0 {
		return nil
	}
	return service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{
		Store:        db,
		Readers:      readers,
		PollInterval: cfg.Usage.PollIntervalDuration(),
		OnChange: func() {
			bus.Publish(events.Event{Type: events.EventSubscriptionUsageChanged, At: time.Now()})
			ipc.Broadcast(ipc.Message{Type: ipcUsageChanged})
		},
		Forward: func() { ipc.Broadcast(ipc.Message{Type: ipcUsageRefresh}) },
	})
}

// startSubscriptionUsage polls in the one process of the data folder that
// holds usage.lock; elsewhere it does nothing, and the service serves what
// that process keeps.
func startSubscriptionUsage(cfg *config.Config, usage *service.SubscriptionUsageService, bus *events.Bus) func() {
	if usage == nil {
		return func() {}
	}
	release, err := claimUsagePoller(config.ResolveDataDir(cfg))
	if err != nil {
		log.Print("subscription usage: another process reads it")
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	requests, unsubscribe := bus.SubscribeInvalidations(events.EventSubscriptionUsageRefreshRequested)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); usage.Run(ctx) }()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-requests:
				usage.Refresh(ctx)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
			unsubscribe()
			release()
		})
	}
}
