package service

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
)

// The states of a repository's copy in Legato's cache.
const (
	CacheMissing  = "missing"  // never copied here
	CacheCopying  = "copying"  // its first copy is being made
	CacheFetching = "fetching" // a copy is being brought up to date
	CacheReady    = "ready"
	CacheFailed   = "failed" // the last copy or fetch failed; Error says why
)

// RepositoryCacheStatus is how a repository's copy in the cache stands,
// with git's progress while it is copied or fetched.
type RepositoryCacheStatus struct {
	Repo  string `json:"repo"`
	State string `json:"state"`
	// Phase, Percent, Received and Rate are git's progress while copying
	// or fetching: "Receiving objects", 38, "310.00 MiB", "3.00 MiB/s".
	Phase    string `json:"phase,omitempty"`
	Percent  int    `json:"percent,omitempty"`
	Received string `json:"received,omitempty"`
	Rate     string `json:"rate,omitempty"`
	Error    string `json:"error,omitempty"`
	// FetchedAt is when the copy was last made or fetched.
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
}

// progressEvery is how often git's progress is reported, at most, for one
// repository: a change of state is reported at once.
const progressEvery = 500 * time.Millisecond

// SetCacheListener is told each change of a repository's copy: its state,
// and git's progress twice a second at most.
func (s *RepositoryService) SetCacheListener(listener func(RepositoryCacheStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listener = listener
}

// CacheStatus is how a repository's copy stands now.
func (s *RepositoryService) CacheStatus(repo string) RepositoryCacheStatus {
	s.mu.Lock()
	status, busy := s.statuses[repo]
	s.mu.Unlock()
	if busy {
		return status
	}
	return s.diskStatus(repo)
}

// diskStatus reads a copy's state from the cache folder.
func (s *RepositoryService) diskStatus(repo string) RepositoryCacheStatus {
	path := s.CachePath(repo)
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		return RepositoryCacheStatus{Repo: repo, State: CacheMissing}
	}
	status := RepositoryCacheStatus{Repo: repo, State: CacheReady}
	if info, err := os.Stat(filepath.Join(path, fetchedStamp)); err == nil {
		at := info.ModTime()
		status.FetchedAt = &at
	}
	return status
}

// setStatus records a copy's state and tells the listener. A failure is
// kept until the next copy or fetch; ready is read from the folder again.
func (s *RepositoryService) setStatus(status RepositoryCacheStatus) {
	s.mu.Lock()
	if status.State == CacheReady {
		delete(s.statuses, status.Repo)
	} else {
		s.statuses[status.Repo] = status
	}
	listener := s.listener
	s.mu.Unlock()
	if status.State == CacheReady {
		status = s.diskStatus(status.Repo)
	}
	if listener != nil {
		listener(status)
	}
}

// reporter turns git's progress into reports of a copy that is copying or
// fetching, twice a second at most.
func (s *RepositoryService) reporter(repo, state string) func(gitrepo.Progress) {
	var last time.Time
	var lastPhase string
	return func(p gitrepo.Progress) {
		now := time.Now()
		if p.Phase == lastPhase && now.Sub(last) < progressEvery {
			return
		}
		last, lastPhase = now, p.Phase
		s.setStatus(RepositoryCacheStatus{Repo: repo, State: state, Phase: p.Phase, Percent: p.Percent, Received: p.Received, Rate: p.Rate})
	}
}

// Warm copies or fetches each repository in turn, so that an agent started
// in one later waits for nothing but its own clone. Repositories the GitHub
// credential does not list are skipped, as are names given twice. It
// returns when all are done or ctx ends; failures are in their status.
func (s *RepositoryService) Warm(ctx context.Context, repos []string) {
	seen := map[string]bool{}
	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		if repo == "" || seen[repo] || validRepo(repo) != nil {
			continue
		}
		seen[repo] = true
		if s.CloneProblem(repo) != "" {
			continue
		}
		_, _ = s.Ensure(ctx, repo)
	}
}
