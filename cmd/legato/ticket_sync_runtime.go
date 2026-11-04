package main

import (
	"context"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/attachments"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ticketSources are the ticket providers this configuration names, and
// Burnlite, which takes part once it is connected (from the CLI or the web
// UI, so possibly while Legato runs).
func ticketSources(cfg *config.Config, db *store.Store) []service.TicketSource {
	var sources []service.TicketSource
	if cfg.Jira.BaseURL != "" && cfg.Jira.Email != "" && cfg.Jira.APIToken != "" {
		provider := service.NewJiraProvider(jira.NewProvider(cfg.Jira.BaseURL, cfg.Jira.Email, cfg.Jira.APIToken, 30*time.Second))
		interval := time.Duration(cfg.Jira.SyncIntervalSeconds) * time.Second
		sources = append(sources, service.NewJiraSourceWith(provider, service.JiraSourceOptions{JQL: cfg.Jira.JQLFilter, ProjectKeys: cfg.Jira.ProjectKeys,
			Interval: interval, StatusSync: cfg.Jira.StatusSyncEnabled, Site: cfg.Jira.BaseURL}))
	}
	secrets := service.SealedIntegrationSecrets{Store: configuredSecretStore(cfg, db)}
	b := cfg.Burnlite
	sources = append(sources, service.NewBurnliteSource(db, service.NewOAuthTokens(db, secrets, nil, service.BurnliteIntegration), service.BurnliteSourceOptions{
		Projects:   func(ctx context.Context) []string { return service.BurnliteProjects(ctx, db, b.Projects) },
		Columns:    service.BurnliteColumns{Ready: b.Columns.Ready, Blocked: b.Columns.Blocked, Claimed: b.Columns.Claimed, Closed: b.Columns.Closed},
		Interval:   time.Duration(b.SyncIntervalSeconds) * time.Second,
		MovesCards: b.StatusSyncEnabled,
	}))
	return sources
}

// newTicketSync is the sync service over every ticket provider. cache may
// be nil.
func newTicketSync(cfg *config.Config, db *store.Store, bus *events.Bus, cache *attachments.Cache) service.SyncService {
	return service.NewTicketSync(db, bus, cache, ticketSources(cfg, db)...)
}
