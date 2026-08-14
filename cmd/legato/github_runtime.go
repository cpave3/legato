package main

import (
	"context"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	gh "github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func buildGitHubClient(cfg *config.Config, command gh.TokenCommand) (*gh.Client, gh.CredentialSource, error) {
	return gh.NewAuthenticatedClient(gh.AuthOptions{
		Mode:    gh.AuthMode(cfg.GitHub.Auth),
		Token:   cfg.GitHub.Token,
		Command: command,
	})
}

func startPRTrackingRuntime(ctx context.Context, cfg *config.Config, db *store.Store, bus *events.Bus) (service.PRTrackingService, func(), error) {
	client, _, err := buildGitHubClient(cfg, nil)
	if err != nil {
		return nil, func() {}, err
	}
	interval := time.Duration(cfg.GitHub.PollIntervalSeconds) * time.Second
	resolvedInterval := time.Duration(cfg.GitHub.ResolvedPollIntervalSeconds) * time.Second
	svc := service.NewPRTrackingServiceWithInspector(db, bus, client, interval, resolvedInterval, checkoutInspector(), githubRepositoryMappings(cfg)...)
	go func() { _ = svc.PollAll(ctx) }()
	return svc, svc.StartPolling(ctx), nil
}
