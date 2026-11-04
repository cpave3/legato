package main

import (
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/attachments"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ticketSources are the ticket providers this configuration names.
func ticketSources(cfg *config.Config) []service.TicketSource {
	var sources []service.TicketSource
	if cfg.Jira.BaseURL != "" && cfg.Jira.Email != "" && cfg.Jira.APIToken != "" {
		provider := service.NewJiraProvider(jira.NewProvider(cfg.Jira.BaseURL, cfg.Jira.Email, cfg.Jira.APIToken, 30*time.Second))
		interval := time.Duration(cfg.Jira.SyncIntervalSeconds) * time.Second
		sources = append(sources, service.NewJiraSource(provider, cfg.Jira.JQLFilter, cfg.Jira.ProjectKeys, interval, cfg.Jira.StatusSyncEnabled))
	}
	return sources
}

// newTicketSync is the sync service over every ticket provider, nil when
// there is none. cache may be nil.
func newTicketSync(cfg *config.Config, db *store.Store, bus *events.Bus, cache *attachments.Cache) service.SyncService {
	sources := ticketSources(cfg)
	if len(sources) == 0 {
		return nil
	}
	return service.NewTicketSync(db, bus, cache, sources...)
}
