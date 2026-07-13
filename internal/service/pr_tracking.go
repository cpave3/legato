package service

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	FetchPRsForCommit(owner, repo, sha string) (*github.PRStatus, error)
	DetectBranch() (string, error)
	DetectRepo() (owner, repo string, err error)
	FetchCommentCount(owner, repo string, prNumber int) (int, error)
}

// PRTrackingService manages PR-to-task linking and polling.
type PRTrackingService interface {
	LinkBranch(ctx context.Context, taskID, branch, repo string) error
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

func (p *prTrackingService) LinkBranch(ctx context.Context, taskID, branch, repo string) error {
	meta := &store.PRMeta{Branch: branch, Repo: repo}
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

	// Unresolved tasks with a recorded head SHA get exact commit-based
	// discovery (immune to branch-name reuse); everything else goes through
	// the branch batch query.
	type shaTarget struct {
		taskID string
		meta   *store.PRMeta
	}
	var shaTargets []shaTarget
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
		// Skip branches with no repo — gh needs --repo to work from non-git dirs.
		if meta.Repo == "" {
			continue
		}
		if meta.PRNumber == 0 && meta.HeadSHA != "" {
			shaTargets = append(shaTargets, shaTarget{taskID: t.ID, meta: meta})
			continue
		}
		branchToTasks[meta.Branch] = append(branchToTasks[meta.Branch], t.ID)
		if branchRepos[meta.Branch] == "" {
			branchRepos[meta.Branch] = meta.Repo
		}
	}

	if len(branchToTasks) == 0 && len(shaTargets) == 0 {
		return nil
	}

	changed := false

	for _, st := range shaTargets {
		status := p.discoverPR(st.meta)
		if p.applyStatus(ctx, st.taskID, st.meta.Branch, status) {
			changed = true
		}
	}

	if len(branchToTasks) > 0 {
		queries := make([]github.BranchQuery, 0, len(branchToTasks))
		for b := range branchToTasks {
			queries = append(queries, github.BranchQuery{Branch: b, Repo: branchRepos[b]})
		}

		statuses, err := p.gh.BatchFetchPRStatusWithRepo(queries)
		if err != nil {
			log.Printf("PR poll error: %v", err)
			// Continue with partial results
		}

		for branch, status := range statuses {
			for _, taskID := range branchToTasks[branch] {
				if p.applyStatus(ctx, taskID, branch, status) {
					changed = true
				}
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

// discoverPR resolves the PR for an unresolved link. When a head SHA was
// recorded at link time, the commit→pulls lookup is tried first — it's an
// exact match on commit identity. Falls back to the branch-name query.
func (p *prTrackingService) discoverPR(meta *store.PRMeta) *github.PRStatus {
	if meta.HeadSHA != "" {
		if owner, name, ok := splitRepo(meta.Repo); ok {
			if status, err := p.gh.FetchPRsForCommit(owner, name, meta.HeadSHA); err == nil && status.HasPR {
				return status
			}
		}
	}
	status, err := p.gh.FetchPRStatus(meta.Branch, meta.Repo)
	if err != nil {
		return &github.PRStatus{HasPR: false}
	}
	return status
}

// applyStatus writes a fetched PR status onto a task's pr_meta, preserving
// link-time fields (repo, head SHA, linked-at). For unresolved links, a
// candidate PR created before the link time fails the filter and is treated
// as "no PR yet" — a reused branch name must not resurrect an old PR.
// Returns true when the stored meta changed.
func (p *prTrackingService) applyStatus(ctx context.Context, taskID, branch string, status *github.PRStatus) bool {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return false
	}
	oldMeta, _ := store.ParsePRMeta(task.PRMeta)

	newMeta := &store.PRMeta{
		Branch:    branch,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if oldMeta != nil {
		newMeta.Repo = oldMeta.Repo
		newMeta.HeadSHA = oldMeta.HeadSHA
		newMeta.LinkedAt = oldMeta.LinkedAt
	}

	unresolved := oldMeta == nil || oldMeta.PRNumber == 0
	if status != nil && status.HasPR && (!unresolved || passesLinkFilter(newMeta.LinkedAt, status.CreatedAt)) {
		newMeta.PRNumber = status.Number
		newMeta.PRURL = status.URL
		newMeta.State = status.State
		newMeta.IsDraft = status.IsDraft
		newMeta.ReviewDecision = status.ReviewDecision
		newMeta.CheckStatus = status.CheckStatus
		newMeta.CommentCount = status.CommentCount
	}

	if !prMetaChanged(oldMeta, newMeta) {
		return false
	}
	raw, err := store.MarshalPRMeta(newMeta)
	if err != nil {
		return false
	}
	if err := p.store.UpdatePRMeta(ctx, taskID, raw); err != nil {
		return false
	}
	return true
}

// passesLinkFilter reports whether a candidate PR is plausibly the one the
// link anticipated: it must not have been created before the link time
// (minus a small slack for clock skew). Missing or unparseable timestamps
// pass through — the filter only rejects on positive evidence.
func passesLinkFilter(linkedAt, prCreatedAt string) bool {
	if linkedAt == "" || prCreatedAt == "" {
		return true
	}
	linked, err := time.Parse(time.RFC3339, linkedAt)
	if err != nil {
		return true
	}
	created, err := time.Parse(time.RFC3339, prCreatedAt)
	if err != nil {
		return true
	}
	return !created.Before(linked.Add(-5 * time.Minute))
}

// splitRepo splits an "owner/repo" string into its parts.
func splitRepo(repo string) (owner, name string, ok bool) {
	i := strings.IndexByte(repo, '/')
	if i <= 0 || i == len(repo)-1 {
		return "", "", false
	}
	return repo[:i], repo[i+1:], true
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

// AutoLinkBranch detects the current branch and repo, then links them to a task if not already linked.
// Skips silently if not in a git repo or repo detection fails (avoids linking wrong repo context).
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
	if branch == "main" || branch == "master" {
		return // default branch is never the PR branch — avoid mislinks
	}

	owner, repo, err := p.gh.DetectRepo()
	if err != nil {
		return // can't determine repo — skip to avoid polling without context
	}

	p.LinkBranch(ctx, taskID, branch, owner+"/"+repo)
}

func (p *prTrackingService) pollSingleBranch(ctx context.Context, taskID, branch string) {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	meta, _ := store.ParsePRMeta(task.PRMeta)

	// Skip if no repo — gh needs --repo to work from non-git dirs.
	if meta == nil || meta.Repo == "" {
		return
	}

	status := p.discoverPR(meta)
	if !p.applyStatus(ctx, taskID, branch, status) {
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
