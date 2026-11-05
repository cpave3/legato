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
// provider ("jira", "burnlite"), "github" for pull requests and "workers"
// for a main's workers when it has them. "jira" and "github" are always
// there.
type ProviderSyncResult map[string]ProviderSyncSourceResult

type ProviderSyncService interface {
	Sync(context.Context) (ProviderSyncResult, error)
}

type providerSyncCoordinator struct {
	tickets SyncService
	prs     PRTrackingService
	workers WorkerSyncer
	mu      sync.Mutex
	running bool
}

// WorkerSyncer reconnects to a main's workers and pulls each one
// (WorkerConnections.SyncWorkers).
type WorkerSyncer interface {
	SyncWorkers(ctx context.Context) error
}

// ProviderSyncOption adds to what a manual sync does.
type ProviderSyncOption func(*providerSyncCoordinator)

// SyncWorkersToo makes a manual sync also reconnect to and pull every
// worker this main paired with; the result names them "workers".
func SyncWorkersToo(w WorkerSyncer) ProviderSyncOption {
	return func(c *providerSyncCoordinator) { c.workers = w }
}

func NewProviderSyncService(tickets SyncService, prs PRTrackingService, opts ...ProviderSyncOption) ProviderSyncService {
	c := &providerSyncCoordinator{tickets: tickets, prs: prs}
	for _, o := range opts {
		o(c)
	}
	return c
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
	if s.workers != nil {
		if err := s.workers.SyncWorkers(ctx); err != nil {
			result["workers"] = ProviderSyncSourceResult{Status: ProviderSyncFailed, Error: err.Error()}
		} else {
			result["workers"] = ProviderSyncSourceResult{Status: ProviderSyncCompleted}
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
