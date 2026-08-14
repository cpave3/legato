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

type ProviderSyncResult struct {
	Jira   ProviderSyncSourceResult `json:"jira"`
	GitHub ProviderSyncSourceResult `json:"github"`
}

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
		Jira:   ProviderSyncSourceResult{Status: ProviderSyncUnavailable},
		GitHub: ProviderSyncSourceResult{Status: ProviderSyncUnavailable},
	}
	if s.tickets != nil {
		syncResult, err := s.tickets.Sync(ctx)
		if err != nil {
			result.Jira = ProviderSyncSourceResult{Status: ProviderSyncFailed, Error: err.Error()}
		} else {
			result.Jira.Status = ProviderSyncCompleted
			if syncResult != nil {
				result.Jira.TasksSynced = syncResult.TasksSynced
			}
		}
	}
	if s.prs != nil {
		if err := s.prs.PollAll(ctx); err != nil {
			result.GitHub = ProviderSyncSourceResult{Status: ProviderSyncFailed, Error: err.Error()}
		} else {
			result.GitHub.Status = ProviderSyncCompleted
		}
	}
	return result, nil
}
