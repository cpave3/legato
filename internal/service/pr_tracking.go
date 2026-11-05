package service

import (
	"context"
	"errors"
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
	ListRepositoryPRs(owner, repo string) ([]github.PRStatus, error)
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

// PRTaskActions exposes targeted context and synchronization operations.
type PRTaskActions interface {
	RefreshPR(ctx context.Context, taskID string) error
	RediscoverPR(ctx context.Context, taskID string) error
	SetCodeContext(ctx context.Context, taskID string, codeContext *store.CodeContext) error
	GetCodeContext(ctx context.Context, taskID string) (*store.CodeContext, error)
}

// RepositoryMapping associates tasks with a configured GitHub repository.
type RepositoryMapping struct {
	Repo         string
	BaseBranch   string
	JiraProjects []string
	Workspaces   []string
}

type prTrackingService struct {
	store            *store.Store
	bus              *events.Bus
	gh               GitHubClient
	interval         time.Duration // unresolved PRs (branch-only)
	resolvedInterval time.Duration // resolved PRs (have PR number)
	repositories     []RepositoryMapping
	inspector        CheckoutInspector

	mu               sync.Mutex // protects lastResolvedPoll
	lastResolvedPoll time.Time  // tracks when resolved PRs were last polled
	pollMu           sync.Mutex // serializes scheduled and forced polling
}

// NewPRTrackingService creates a new PR tracking service.
func NewPRTrackingService(s *store.Store, bus *events.Bus, gh GitHubClient, interval, resolvedInterval time.Duration, repositories ...RepositoryMapping) PRTrackingService {
	return newPRTrackingService(s, bus, gh, interval, resolvedInterval, nil, repositories...)
}

func NewPRTrackingServiceWithInspector(s *store.Store, bus *events.Bus, gh GitHubClient, interval, resolvedInterval time.Duration, inspector CheckoutInspector, repositories ...RepositoryMapping) PRTrackingService {
	return newPRTrackingService(s, bus, gh, interval, resolvedInterval, inspector, repositories...)
}

func newPRTrackingService(s *store.Store, bus *events.Bus, gh GitHubClient, interval, resolvedInterval time.Duration, inspector CheckoutInspector, repositories ...RepositoryMapping) PRTrackingService {
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
		repositories:     append([]RepositoryMapping(nil), repositories...),
		inspector:        inspector,
	}
}

func (p *prTrackingService) LinkBranch(ctx context.Context, taskID, branch, repo string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	codeContext := &store.CodeContext{
		Repo: repo, Branch: branch, RepoSource: "manual", BranchSource: "manual", LinkedAt: now, UpdatedAt: now,
	}
	if err := p.store.SetCodeContext(ctx, taskID, codeContext); err != nil {
		return err
	}
	if err := p.store.RemoveTaskBranches(ctx, taskID, BranchSourceCodeContext, BranchSourceManual, BranchSourceDiscovered); err != nil {
		return err
	}
	if err := p.store.UpdatePRMeta(ctx, taskID, nil); err != nil {
		return err
	}
	go func() { _ = p.RediscoverPR(context.Background(), taskID) }()
	return nil
}

func (p *prTrackingService) LinkPR(ctx context.Context, taskID string, owner, repo string, prNumber int) error {
	status, err := p.gh.FetchPRByNumber(owner, repo, prNumber)
	if err != nil {
		return fmt.Errorf("fetching PR: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	full := owner + "/" + repo
	codeContext := &store.CodeContext{
		Repo: full, Branch: status.HeadBranch, HeadSHA: status.HeadSHA,
		RepoSource: "manual", BranchSource: "manual", LinkedAt: now, UpdatedAt: now,
	}
	if err := SetTaskCodeContext(ctx, p.store, taskID, codeContext); err != nil {
		return err
	}
	if _, err := p.store.TrackTaskBranches(ctx, taskID, []store.BranchRef{{Repo: full, Branch: status.HeadBranch}}, BranchSourceManual); err != nil {
		return err
	}
	// A PR linked by number replaces any other PR found on its branch.
	pr := prFromStatus(status, "manual", true)
	pr.UpdatedAt = now
	if err := p.store.SetTaskBranchPR(ctx, taskID, full, status.HeadBranch, pr); err != nil {
		return err
	}
	p.writeSummary(ctx, taskID, "", "")
	p.publishIfChanged(true)
	return nil
}

func (p *prTrackingService) UnlinkBranch(ctx context.Context, taskID string) error {
	if err := p.store.RemoveTaskBranches(ctx, taskID); err != nil {
		return err
	}
	if err := p.store.UpdatePRMeta(ctx, taskID, nil); err != nil {
		return err
	}
	return p.store.SetCodeContext(ctx, taskID, nil)
}

func (p *prTrackingService) SetCodeContext(ctx context.Context, taskID string, codeContext *store.CodeContext) error {
	return SetTaskCodeContext(ctx, p.store, taskID, codeContext)
}

// SetTaskCodeContext updates local repository identity and clears a stale PR
// association when that identity changes. It requires no GitHub authentication.
func SetTaskCodeContext(ctx context.Context, taskStore *store.Store, taskID string, codeContext *store.CodeContext) error {
	task, err := taskStore.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	existing, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return err
	}
	identityChanged := existing != nil && codeContext != nil && (existing.Repo != codeContext.Repo || existing.Branch != codeContext.Branch)
	if err := taskStore.SetCodeContext(ctx, taskID, codeContext); err != nil {
		return err
	}
	if !identityChanged {
		return nil
	}
	// The old branch was the task's because of its context; the branches of
	// a stack or added by handoff stay.
	if err := ForgetContextBranch(ctx, taskStore, taskID, existing); err != nil {
		return err
	}
	return taskStore.UpdatePRMeta(ctx, taskID, nil)
}

func (p *prTrackingService) GetCodeContext(ctx context.Context, taskID string) (*store.CodeContext, error) {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return store.ParseCodeContext(task.CodeContext)
}

func (p *prTrackingService) RefreshPR(ctx context.Context, taskID string) error {
	task, cc, err := p.taskContext(ctx, taskID)
	if err != nil {
		return err
	}
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return err
	}
	legacy, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		return err
	}
	hasLegacy := legacy != nil && legacy.PRNumber > 0 && cc != nil && cc.Repo != ""
	if !hasLinkedBranch(rows) && !hasLegacy {
		return fmt.Errorf("task %s has no linked PR with repository context", taskID)
	}
	changed, err := p.syncTask(ctx, task, cc, syncMode{refresh: true}, newPRListings(p.gh))
	p.publishIfChanged(changed)
	return err
}

// RediscoverPR forgets the PRs found for every branch of a task and looks
// for them again, with one listing per repository.
func (p *prTrackingService) RediscoverPR(ctx context.Context, taskID string) error {
	task, cc, err := p.taskContext(ctx, taskID)
	if err != nil {
		return err
	}
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return err
	}
	if (cc == nil || cc.Repo == "") && len(rows) == 0 {
		return fmt.Errorf("task %s has no repository context", taskID)
	}
	if cc != nil && cc.Repo != "" {
		if _, _, ok := splitRepo(cc.Repo); !ok {
			return fmt.Errorf("repository must be owner/name")
		}
	}
	changed, err := p.syncTask(ctx, task, cc, syncMode{rediscover: true, discover: true}, newPRListings(p.gh))
	p.publishIfChanged(changed)
	return err
}

func hasLinkedBranch(rows []store.TaskBranch) bool {
	for _, row := range rows {
		if row.PR != nil && row.PR.PRNumber > 0 {
			return true
		}
	}
	return false
}

// ForgetContextBranch stops tracking the branch a task's old code context
// named, when that context is why it was tracked.
func ForgetContextBranch(ctx context.Context, taskStore *store.Store, taskID string, old *store.CodeContext) error {
	if old == nil || old.Repo == "" || old.Branch == "" {
		return nil
	}
	rows, err := taskStore.ListTaskBranches(ctx, taskID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Repo == old.Repo && row.Branch == old.Branch && (row.Source == BranchSourceCodeContext || row.Source == BranchSourceManual) {
			return taskStore.RemoveTaskBranch(ctx, taskID, row.Repo, row.Branch)
		}
	}
	return nil
}

func prStatusUpdated() events.Event {
	return events.Event{Type: events.EventPRStatusUpdated, At: time.Now()}
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
	p.pollMu.Lock()
	defer p.pollMu.Unlock()

	tasks, err := p.store.ListPRSyncCandidates(ctx)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}

	// Tasks with repository context or tracked branches are brought up to
	// date branch by branch, each repository listed at most once. Legacy
	// branch-only links without context keep the branch query; those with a
	// recorded head SHA get exact commit-based discovery.
	type shaTarget struct {
		taskID string
		meta   *store.PRMeta
	}
	var shaTargets []shaTarget
	var pollErrors []error
	changed := false
	listings := newPRListings(p.gh)
	mode := syncMode{discover: true, refresh: !skipResolved}
	branchToTasks := make(map[string][]string) // branch → task IDs
	branchRepos := make(map[string]string)     // branch → repo (first seen)
	tracked, err := p.store.ListTrackedTaskBranches(ctx)
	if err != nil {
		return err
	}
	for i := range tasks {
		t := &tasks[i]
		meta, err := store.ParsePRMeta(t.PRMeta)
		if err != nil {
			continue
		}
		codeContext, err := store.ParseCodeContext(t.CodeContext)
		if err != nil {
			continue
		}
		if codeContext == nil || codeContext.Repo == "" {
			codeContext = p.contextFromWorktree(ctx, t)
		}
		if codeContext == nil || codeContext.Repo == "" {
			codeContext = p.contextFromMappings(ctx, t)
			if codeContext != nil {
				_ = p.store.SetCodeContext(ctx, t.ID, codeContext)
			}
		}
		if (codeContext != nil && codeContext.Repo != "") || len(tracked[t.ID]) > 0 {
			taskChanged, syncErr := p.syncTask(ctx, t, codeContext, mode, listings)
			changed = changed || taskChanged
			if syncErr != nil {
				pollErrors = append(pollErrors, syncErr)
			}
			continue
		}
		if meta == nil || meta.Branch == "" {
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
			pollErrors = append(pollErrors, err)
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

	p.publishIfChanged(changed)
	return errors.Join(pollErrors...)
}

func (p *prTrackingService) contextFromWorktree(ctx context.Context, task *store.Task) *store.CodeContext {
	if p.inspector == nil || task.WorktreePath == nil || *task.WorktreePath == "" {
		return nil
	}
	captured, err := p.inspector.InspectCodeContext(*task.WorktreePath)
	if err != nil || captured == nil || captured.Repo == "" {
		return nil
	}
	captured.WorkingDir = *task.WorktreePath
	if task.WorktreeBaseBranch != nil {
		captured.BaseBranch = *task.WorktreeBaseBranch
	}
	captured.RepoSource = "worktree"
	captured.BranchSource = "worktree"
	if existing, parseErr := store.ParseCodeContext(task.CodeContext); parseErr == nil && existing != nil {
		if existing.BranchSource == "manual" {
			captured.Branch = existing.Branch
			captured.BranchSource = existing.BranchSource
		}
		if existing.BaseBranch != "" {
			captured.BaseBranch = existing.BaseBranch
		}
	}
	captured.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := p.store.SetCodeContext(ctx, task.ID, captured); err != nil {
		return nil
	}
	return captured
}

func (p *prTrackingService) contextFromMappings(ctx context.Context, task *store.Task) *store.CodeContext {
	matches := make([]RepositoryMapping, 0)
	project := ""
	if task.RemoteID != nil {
		if index := strings.IndexByte(*task.RemoteID, '-'); index > 0 {
			project = strings.ToUpper((*task.RemoteID)[:index])
		}
	}
	workspace := ""
	if task.WorkspaceID != nil {
		if value, err := p.store.GetWorkspace(ctx, *task.WorkspaceID); err == nil {
			workspace = value.Name
		}
	}
	for _, mapping := range p.repositories {
		if containsFold(mapping.JiraProjects, project) || containsFold(mapping.Workspaces, workspace) {
			matches = append(matches, mapping)
		}
	}
	if len(matches) != 1 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return &store.CodeContext{Repo: matches[0].Repo, BaseBranch: matches[0].BaseBranch, RepoSource: "repository_preset", UpdatedAt: now}
}

func containsFold(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func matchContextPR(taskID, repo string, codeContext *store.CodeContext, prs []github.PRStatus) (*github.PRStatus, string, string) {
	if codeContext.HeadSHA != "" {
		matches := filterPRs(prs, func(pr github.PRStatus) bool { return pr.HeadSHA == codeContext.HeadSHA })
		if len(matches) == 1 {
			return &matches[0], "commit", "linked"
		}
		if len(matches) > 1 {
			return nil, "", "ambiguous"
		}
	}
	if codeContext.Branch != "" && !isDefaultBranch(codeContext.Branch, codeContext.BaseBranch) {
		matches := filterPRs(prs, func(pr github.PRStatus) bool {
			return pr.HeadBranch == codeContext.Branch && (pr.HeadRepo == "" || pr.HeadRepo == repo)
		})
		if len(matches) == 1 {
			return &matches[0], "branch", "linked"
		}
		if len(matches) > 1 {
			best := pickServicePR(matches)
			return &best, "branch", "linked"
		}
	}
	matches := filterPRs(prs, func(pr github.PRStatus) bool {
		return !isDefaultBranch(pr.HeadBranch, codeContext.BaseBranch) && branchContainsKey(pr.HeadBranch, taskID)
	})
	if len(matches) == 1 {
		return &matches[0], "ticket_key", "linked"
	}
	if len(matches) > 1 {
		return nil, "", "ambiguous"
	}
	return nil, "", "no_match"
}

func filterPRs(prs []github.PRStatus, keep func(github.PRStatus) bool) []github.PRStatus {
	matches := make([]github.PRStatus, 0)
	for _, pr := range prs {
		if keep(pr) {
			matches = append(matches, pr)
		}
	}
	return matches
}

func pickServicePR(prs []github.PRStatus) github.PRStatus {
	best := prs[0]
	for _, pr := range prs[1:] {
		bestOpen := strings.EqualFold(best.State, "OPEN")
		prOpen := strings.EqualFold(pr.State, "OPEN")
		if prOpen != bestOpen {
			if prOpen {
				best = pr
			}
			continue
		}
		if pr.CreatedAt > best.CreatedAt {
			best = pr
		}
	}
	return best
}

func branchContainsKey(branch, key string) bool {
	branch = strings.ToLower(branch)
	key = strings.ToLower(key)
	for start := 0; ; {
		index := strings.Index(branch[start:], key)
		if index < 0 {
			return false
		}
		index += start
		leftOK := index == 0 || !isKeyCharacter(branch[index-1])
		end := index + len(key)
		rightOK := end == len(branch) || !isKeyCharacter(branch[end])
		if leftOK && rightOK {
			return true
		}
		start = index + 1
	}
}

func isKeyCharacter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func isDefaultBranch(branch, configured string) bool {
	return branch == "" || branch == "HEAD" || branch == "main" || branch == "master" || configured != "" && branch == configured
}

func (p *prTrackingService) applyDiscoveryOutcome(ctx context.Context, taskID, outcome, syncError string) bool {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return false
	}
	meta, _ := store.ParsePRMeta(task.PRMeta)
	if meta == nil {
		meta = &store.PRMeta{}
	}
	if meta.SyncOutcome == outcome && meta.SyncError == syncError {
		return false
	}
	meta.SyncOutcome = outcome
	meta.SyncError = syncError
	meta.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := store.MarshalPRMeta(meta)
	if err != nil || p.store.UpdatePRMeta(ctx, taskID, raw) != nil {
		return false
	}
	return true
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
