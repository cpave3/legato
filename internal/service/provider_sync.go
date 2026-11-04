package service

import (
	"context"
	"errors"
	"sync"
)

const (
	ProviderSyncCompleted   = "completed"
	ProviderSyncFailed      = "failed"
	ProviderSyncUnavailable = "unavailable"
)

var ErrProviderSyncInProgress = errors.New("provider sync already in progress")

type ProviderSyncSourceResult struct {
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	TasksSynced int    `json:"tasks_synced,omitempty"`
}

// ProviderSyncResult is each provider's outcome by name: every ticket
// provider ("jira", "burnlite") and "github" for pull requests. "jira" and
// "github" are always there.
type ProviderSyncResult map[string]ProviderSyncSourceResult

type ProviderSyncService interface {
	Sync(context.Context) (ProviderSyncResult, error)
}

type providerSyncCoordinator struct {
	tickets SyncService
	prs     PRTrackingService
	mu      sync.Mutex
	running bool
}

func NewProviderSyncService(tickets SyncService, prs PRTrackingService) ProviderSyncService {
	return &providerSyncCoordinator{tickets: tickets, prs: prs}
}

func (s *providerSyncCoordinator) Sync(ctx context.Context) (ProviderSyncResult, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ProviderSyncResult{}, ErrProviderSyncInProgress
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	result := ProviderSyncResult{
		"jira":   {Status: ProviderSyncUnavailable},
		"github": {Status: ProviderSyncUnavailable},
	}
	if s.tickets != nil {
		syncResult, err := s.tickets.Sync(ctx)
		switch {
		case syncResult != nil && len(syncResult.Sources) > 0:
			for name, source := range syncResult.Sources {
				switch {
				case source.Disabled:
					result[name] = ProviderSyncSourceResult{Status: ProviderSyncUnavailable}
				case source.Err != nil:
					result[name] = ProviderSyncSourceResult{Status: ProviderSyncFailed, Error: source.Err.Error()}
				default:
					result[name] = ProviderSyncSourceResult{Status: ProviderSyncCompleted, TasksSynced: source.TasksSynced}
				}
			}
		case err != nil:
			result["jira"] = ProviderSyncSourceResult{Status: ProviderSyncFailed, Error: err.Error()}
		default:
			r := ProviderSyncSourceResult{Status: ProviderSyncCompleted}
			if syncResult != nil {
				r.TasksSynced = syncResult.TasksSynced
			}
			result["jira"] = r
		}
	}
	if s.prs != nil {
		if err := s.prs.PollAll(ctx); err != nil {
			result["github"] = ProviderSyncSourceResult{Status: ProviderSyncFailed, Error: err.Error()}
		} else {
			result["github"] = ProviderSyncSourceResult{Status: ProviderSyncCompleted}
		}
	}
	return result, nil
}
