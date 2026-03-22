package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

// GitHubClient abstracts the GitHub PR client for testability.
type GitHubClient interface {
	FetchPRStatus(branch string, repo ...string) (*github.PRStatus, error)
	BatchFetchPRStatus(branches []string) (map[string]*github.PRStatus, error)
	BatchFetchPRStatusWithRepo(queries []github.BranchQuery) (map[string]*github.PRStatus, error)
	FetchPRByNumber(owner, repo string, prNumber int) (*github.PRStatus, error)
	DetectBranch() (string, error)
	DetectRepo() (owner, repo string, err error)
	FetchCommentCount(owner, repo string, prNumber int) (int, error)
}

// PRTrackingService manages PR-to-task linking and polling.
type PRTrackingService interface {
	LinkBranch(ctx context.Context, taskID, branch string) error
	LinkPR(ctx context.Context, taskID string, owner, repo string, prNumber int) error
	UnlinkBranch(ctx context.Context, taskID string) error
	PollOnce(ctx context.Context) error
	PollAll(ctx context.Context) error
	StartPolling(ctx context.Context) func()
	GetPRStatus(ctx context.Context, taskID string) (*store.PRMeta, error)
	DetectRepo() (owner, repo string, err error)
	FetchPRByNumber(owner, repo string, prNumber int) (*github.PRStatus, error)
}

type prTrackingService struct {
	store            *store.Store
	bus              *events.Bus
	gh               GitHubClient
	interval         time.Duration // unresolved PRs (branch-only)
	resolvedInterval time.Duration // resolved PRs (have PR number)

	mu               sync.Mutex // protects lastResolvedPoll
	lastResolvedPoll time.Time  // tracks when resolved PRs were last polled
}

// NewPRTrackingService creates a new PR tracking service.
func NewPRTrackingService(s *store.Store, bus *events.Bus, gh GitHubClient, interval, resolvedInterval time.Duration) PRTrackingService {
	if interval == 0 {
		interval = 10 * time.Minute
	}
	if resolvedInterval == 0 {
		resolvedInterval = 10 * time.Minute
	}
	return &prTrackingService{
		store:            s,
		bus:              bus,
		gh:               gh,
		interval:         interval,
		resolvedInterval: resolvedInterval,
	}
}

func (p *prTrackingService) LinkBranch(ctx context.Context, taskID, branch string) error {
	meta := &store.PRMeta{Branch: branch}
	raw, err := store.MarshalPRMeta(meta)
	if err != nil {
		return err
	}
	if err := p.store.UpdatePRMeta(ctx, taskID, raw); err != nil {
		return err
	}

	// Trigger immediate poll for this branch
	go p.pollSingleBranch(ctx, taskID, branch)
	return nil
}

func (p *prTrackingService) LinkPR(ctx context.Context, taskID string, owner, repo string, prNumber int) error {
	status, err := p.gh.FetchPRByNumber(owner, repo, prNumber)
	if err != nil {
		return fmt.Errorf("fetching PR: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	meta := &store.PRMeta{
		Repo:         owner + "/" + repo,
		Branch:       status.HeadBranch,
		PRNumber:     status.Number,
		PRURL:        status.URL,
		State:        status.State,
		IsDraft:      status.IsDraft,
		CheckStatus:  status.CheckStatus,
		CommentCount: status.CommentCount,
		UpdatedAt:    now,
	}

	raw, err := store.MarshalPRMeta(meta)
	if err != nil {
		return err
	}
	if err := p.store.UpdatePRMeta(ctx, taskID, raw); err != nil {
		return err
	}

	if p.bus != nil {
		p.bus.Publish(events.Event{
			Type: events.EventPRStatusUpdated,
			At:   time.Now(),
		})
	}
	return nil
}

func (p *prTrackingService) UnlinkBranch(ctx context.Context, taskID string) error {
	return p.store.UpdatePRMeta(ctx, taskID, nil)
}

// PollAll fetches PR status for all tracked tasks regardless of resolved state.
// Used on startup to ensure all data is fresh.
func (p *prTrackingService) PollAll(ctx context.Context) error {
	p.mu.Lock()
	p.lastResolvedPoll = time.Now()
	p.mu.Unlock()
	return p.pollInternal(ctx, false)
}

func (p *prTrackingService) PollOnce(ctx context.Context) error {
	// Check if resolved PRs should be included this cycle.
	p.mu.Lock()
	includeResolved := time.Since(p.lastResolvedPoll) >= p.resolvedInterval
	if includeResolved {
		p.lastResolvedPoll = time.Now()
	}
	p.mu.Unlock()
	return p.pollInternal(ctx, !includeResolved)
}

func (p *prTrackingService) pollInternal(ctx context.Context, skipResolved bool) error {
	tasks, err := p.store.ListPRTrackedTasks(ctx)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}

	// Extract branches with repo info for scoped queries.
	branchToTasks := make(map[string][]string) // branch → task IDs
	branchRepos := make(map[string]string)     // branch → repo (first seen)
	for _, t := range tasks {
		meta, err := store.ParsePRMeta(t.PRMeta)
		if err != nil || meta == nil || meta.Branch == "" {
			continue
		}
		// Skip resolved PRs (already have a PR number) when not due.
		if skipResolved && meta.PRNumber > 0 {
			continue
		}
		branchToTasks[meta.Branch] = append(branchToTasks[meta.Branch], t.ID)
		if meta.Repo != "" && branchRepos[meta.Branch] == "" {
			branchRepos[meta.Branch] = meta.Repo
		}
	}

	if len(branchToTasks) == 0 {
		return nil
	}

	queries := make([]github.BranchQuery, 0, len(branchToTasks))
	for b := range branchToTasks {
		queries = append(queries, github.BranchQuery{Branch: b, Repo: branchRepos[b]})
	}

	statuses, err := p.gh.BatchFetchPRStatusWithRepo(queries)
	if err != nil {
		log.Printf("PR poll error: %v", err)
		// Continue with partial results
	}

	changed := false
	now := time.Now().UTC().Format(time.RFC3339)

	for branch, status := range statuses {
		taskIDs := branchToTasks[branch]
		for _, taskID := range taskIDs {
			task, err := p.store.GetTask(ctx, taskID)
			if err != nil {
				continue
			}
			oldMeta, _ := store.ParsePRMeta(task.PRMeta)

			repo := ""
			if oldMeta != nil {
				repo = oldMeta.Repo
			}
			newMeta := &store.PRMeta{
				Repo:      repo,
				Branch:    branch,
				UpdatedAt: now,
			}
			if status.HasPR {
				newMeta.PRNumber = status.Number
				newMeta.PRURL = status.URL
				newMeta.State = status.State
				newMeta.IsDraft = status.IsDraft
				newMeta.ReviewDecision = status.ReviewDecision
				newMeta.CheckStatus = status.CheckStatus
				newMeta.CommentCount = status.CommentCount
			}

			if prMetaChanged(oldMeta, newMeta) {
				raw, err := store.MarshalPRMeta(newMeta)
				if err != nil {
					continue
				}
				if err := p.store.UpdatePRMeta(ctx, taskID, raw); err != nil {
					continue
				}
				changed = true
			}
		}
	}

	if changed && p.bus != nil {
		p.bus.Publish(events.Event{
			Type: events.EventPRStatusUpdated,
			At:   time.Now(),
		})
	}

	return nil
}

func (p *prTrackingService) StartPolling(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.PollOnce(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
	return cancel
}

func (p *prTrackingService) GetPRStatus(ctx context.Context, taskID string) (*store.PRMeta, error) {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return store.ParsePRMeta(task.PRMeta)
}

func (p *prTrackingService) DetectRepo() (owner, repo string, err error) {
	return p.gh.DetectRepo()
}

func (p *prTrackingService) FetchPRByNumber(owner, repo string, prNumber int) (*github.PRStatus, error) {
	return p.gh.FetchPRByNumber(owner, repo, prNumber)
}

// AutoLinkBranch detects the current branch and links it to a task if not already linked.
func (p *prTrackingService) AutoLinkBranch(ctx context.Context, taskID string) {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	meta, _ := store.ParsePRMeta(task.PRMeta)
	if meta != nil && meta.Branch != "" {
		return // already linked
	}

	branch, err := p.gh.DetectBranch()
	if err != nil {
		return // not in a git repo — silently skip
	}
	if branch == "" || branch == "HEAD" {
		return // detached HEAD
	}

	p.LinkBranch(ctx, taskID, branch)
}

func (p *prTrackingService) pollSingleBranch(ctx context.Context, taskID, branch string) {
	// Look up existing repo for scoped query.
	var repo string
	if task, err := p.store.GetTask(ctx, taskID); err == nil {
		if meta, _ := store.ParsePRMeta(task.PRMeta); meta != nil {
			repo = meta.Repo
		}
	}

	status, err := p.gh.FetchPRStatus(branch, repo)
	if err != nil {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	newMeta := &store.PRMeta{
		Repo:      repo,
		Branch:    branch,
		UpdatedAt: now,
	}
	if status.HasPR {
		newMeta.PRNumber = status.Number
		newMeta.PRURL = status.URL
		newMeta.State = status.State
		newMeta.IsDraft = status.IsDraft
		newMeta.ReviewDecision = status.ReviewDecision
		newMeta.CheckStatus = status.CheckStatus
		newMeta.CommentCount = status.CommentCount
	}

	raw, err := store.MarshalPRMeta(newMeta)
	if err != nil {
		return
	}
	if err := p.store.UpdatePRMeta(ctx, taskID, raw); err != nil {
		return
	}

	if p.bus != nil {
		p.bus.Publish(events.Event{
			Type: events.EventPRStatusUpdated,
			At:   time.Now(),
		})
	}
}

// prMetaChanged compares two PRMeta structs to detect meaningful changes.
func prMetaChanged(old, new *store.PRMeta) bool {
	if old == nil {
		return true
	}
	return old.PRNumber != new.PRNumber ||
		old.State != new.State ||
		old.IsDraft != new.IsDraft ||
		old.ReviewDecision != new.ReviewDecision ||
		old.CheckStatus != new.CheckStatus ||
		old.CommentCount != new.CommentCount ||
		old.PRURL != new.PRURL
}
