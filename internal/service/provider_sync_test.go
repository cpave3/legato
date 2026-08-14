package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

type providerSyncTickets struct {
	result  *SyncResult
	err     error
	calls   int
	started chan struct{}
	release chan struct{}
}

func (s *providerSyncTickets) Sync(ctx context.Context) (*SyncResult, error) {
	s.calls++
	if s.started != nil {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.result, s.err
}
func (*providerSyncTickets) Status() SyncStatus                    { return SyncStatus{} }
func (*providerSyncTickets) Subscribe() <-chan SyncEvent           { return nil }
func (*providerSyncTickets) StartScheduler(context.Context) func() { return func() {} }
func (*providerSyncTickets) SearchRemote(context.Context, string) ([]RemoteSearchResult, error) {
	return nil, nil
}
func (*providerSyncTickets) ImportRemoteTask(context.Context, string, *int) (*Card, error) {
	return nil, nil
}

type providerSyncPRs struct {
	err   error
	calls int
}

func (*providerSyncPRs) LinkBranch(context.Context, string, string, string) error  { return nil }
func (*providerSyncPRs) LinkPR(context.Context, string, string, string, int) error { return nil }
func (*providerSyncPRs) UnlinkBranch(context.Context, string) error                { return nil }
func (*providerSyncPRs) PollOnce(context.Context) error                            { return nil }
func (s *providerSyncPRs) PollAll(context.Context) error {
	s.calls++
	return s.err
}
func (*providerSyncPRs) StartPolling(context.Context) func() { return func() {} }
func (*providerSyncPRs) GetPRStatus(context.Context, string) (*store.PRMeta, error) {
	return nil, nil
}
func (*providerSyncPRs) DetectRepo() (string, string, error) { return "", "", nil }
func (*providerSyncPRs) FetchPRByNumber(string, string, int) (*github.PRStatus, error) {
	return nil, nil
}

func TestProviderSyncAttemptsEveryConfiguredSource(t *testing.T) {
	tickets := &providerSyncTickets{result: &SyncResult{TasksSynced: 4}, err: errors.New("jira unavailable")}
	prs := &providerSyncPRs{}
	result, err := NewProviderSyncService(tickets, prs).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tickets.calls != 1 || prs.calls != 1 {
		t.Fatalf("calls = tickets:%d prs:%d", tickets.calls, prs.calls)
	}
	if result.Jira.Status != ProviderSyncFailed || result.Jira.Error != "jira unavailable" {
		t.Fatalf("jira result = %#v", result.Jira)
	}
	if result.GitHub.Status != ProviderSyncCompleted {
		t.Fatalf("github result = %#v", result.GitHub)
	}
}

func TestProviderSyncReportsCountsAndUnavailableSources(t *testing.T) {
	tickets := &providerSyncTickets{result: &SyncResult{TasksSynced: 4}}
	result, err := NewProviderSyncService(tickets, nil).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Jira.Status != ProviderSyncCompleted || result.Jira.TasksSynced != 4 {
		t.Fatalf("jira result = %#v", result.Jira)
	}
	if result.GitHub.Status != ProviderSyncUnavailable {
		t.Fatalf("github result = %#v", result.GitHub)
	}

	result, err = NewProviderSyncService(nil, nil).Sync(context.Background())
	if err != nil || result.Jira.Status != ProviderSyncUnavailable || result.GitHub.Status != ProviderSyncUnavailable {
		t.Fatalf("empty result = %#v, %v", result, err)
	}
}

func TestProviderSyncReturnsGitHubFailureWithoutDiscardingJiraSuccess(t *testing.T) {
	tickets := &providerSyncTickets{result: &SyncResult{TasksSynced: 2}}
	prs := &providerSyncPRs{err: errors.New("rate limited")}
	result, err := NewProviderSyncService(tickets, prs).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Jira.Status != ProviderSyncCompleted || result.GitHub.Status != ProviderSyncFailed || result.GitHub.Error != "rate limited" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProviderSyncRejectsOverlap(t *testing.T) {
	tickets := &providerSyncTickets{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewProviderSyncService(tickets, nil)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = svc.Sync(context.Background())
	}()
	<-tickets.started
	if _, err := svc.Sync(context.Background()); !errors.Is(err, ErrProviderSyncInProgress) {
		t.Fatalf("error = %v", err)
	}
	close(tickets.release)
	wg.Wait()
}

func TestProviderSyncPropagatesCancellationAsSourceFailure(t *testing.T) {
	tickets := &providerSyncTickets{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewProviderSyncService(tickets, nil).Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Jira.Status != ProviderSyncFailed || result.Jira.Error != context.Canceled.Error() {
		t.Fatalf("jira result = %#v", result.Jira)
	}
}
