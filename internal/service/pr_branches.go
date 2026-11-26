package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

// Where a tracked branch came from (store.TaskBranch.Source).
const (
	BranchSourceCodeContext = "code_context" // the branch of the task's code context
	BranchSourceStack       = "stack"        // a Staccato stack recorded for one of the task's job clones
	BranchSourceAPI         = "api"          // added through AddTaskBranches (handoff)
	BranchSourceManual      = "manual"       // the head branch of a PR linked by number
	BranchSourceDiscovered  = "discovered"   // found by commit or ticket key in the repository's PRs
	BranchSourceReview      = "review"       // the stack of open PRs a review loop found on GitHub (a worker's card)
	BranchSourceAgent       = "agent"        // pushed or opened as a PR by the task's agent, or its checkout's branch before it moved
)

// PRBranchActions tracks several branches per task, each with its PR, in
// stack order.
type PRBranchActions interface {
	// AddTaskBranches adds branches of a repository to a task in the order
	// given (stack order, bottom first) and returns those it did not track
	// before. It does not ask GitHub; call DiscoverPRs when any were added.
	AddTaskBranches(ctx context.Context, taskID, repo string, branches []string) ([]string, error)
	// TrackedBranches lists a task's branches in stack order, each with the
	// PR found for it.
	TrackedBranches(ctx context.Context, taskID string) ([]store.TaskBranch, error)
	// DiscoverPRs looks for the PRs of a task's branches that have none yet,
	// with one listing per repository, leaving the PRs found before as they
	// are.
	DiscoverPRs(ctx context.Context, taskID string) error
	// RefreshBranchStates brings the PRs of some of a task's branches in one
	// repository up to date, and no others: a branch with none is looked for
	// in one listing, and a PR found before is fetched again by number,
	// state only (no reviews or checks, which it keeps), unless it is
	// merged. It is for a reader that must not act on a stale state, such
	// as a workflow step's prs output. Any GitHub error is returned.
	RefreshBranchStates(ctx context.Context, taskID, repo string, branches []string) error
}

// AddTaskBranches records branches for a task without GitHub: the form a
// caller uses when PR tracking may be off. See PRBranchActions.
func AddTaskBranches(ctx context.Context, db *store.Store, taskID, repo string, branches []string) ([]string, error) {
	if _, _, ok := splitRepo(repo); !ok {
		return nil, fmt.Errorf("repository must be owner/name")
	}
	if _, err := db.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	refs := make([]store.BranchRef, 0, len(branches))
	for _, b := range branches {
		if b = strings.TrimSpace(b); b != "" {
			refs = append(refs, store.BranchRef{Repo: repo, Branch: b})
		}
	}
	added, err := db.TrackTaskBranches(ctx, taskID, refs, BranchSourceAPI)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(added))
	for _, a := range added {
		names = append(names, a.Branch)
	}
	return names, nil
}

func (p *prTrackingService) AddTaskBranches(ctx context.Context, taskID, repo string, branches []string) ([]string, error) {
	return AddTaskBranches(ctx, p.store, taskID, repo, branches)
}

func (p *prTrackingService) TrackedBranches(ctx context.Context, taskID string) ([]store.TaskBranch, error) {
	return p.store.ListTaskBranches(ctx, taskID)
}

func (p *prTrackingService) DiscoverPRs(ctx context.Context, taskID string) error {
	task, cc, err := p.taskContext(ctx, taskID)
	if err != nil {
		return err
	}
	changed, err := p.syncTask(ctx, task, cc, syncMode{discover: true}, newPRListings(p.gh))
	p.publishIfChanged(changed)
	return err
}

func (p *prTrackingService) RefreshBranchStates(ctx context.Context, taskID, repo string, branches []string) error {
	task, cc, err := p.taskContext(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Ephemeral {
		return nil // as syncTask: an ephemeral task links no PRs (LEG-519)
	}
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return err
	}
	changed := false
	// A PR of this repository held in the single field only (linked before
	// branches were tracked) is moved onto its branch first, state only:
	// read from the single field, a branch with no PR would be its stale
	// state, and the PR may be too old for the listing.
	if legacy, _ := store.ParsePRMeta(task.PRMeta); legacy != nil && legacy.PRNumber > 0 && !holdsPR(rows, legacy.PRNumber) && cc != nil && cc.Repo == repo {
		adopted, err := p.adoptLegacyPR(ctx, taskID, cc, legacy, true)
		if err != nil {
			p.publishIfChanged(p.applyDiscoveryOutcome(ctx, taskID, "error", err.Error()))
			return fmt.Errorf("refresh %s PR #%d: %w", taskID, legacy.PRNumber, err)
		}
		changed = adopted
		if rows, err = p.store.ListTaskBranches(ctx, taskID); err != nil {
			return err
		}
		rows = slices.DeleteFunc(rows, func(row store.TaskBranch) bool { return row.PR != nil && row.PR.PRNumber == legacy.PRNumber })
	}
	listings := newPRListings(p.gh)
	var errs []error
	for _, row := range rows {
		if row.Repo != repo || !slices.Contains(branches, row.Branch) {
			continue
		}
		if row.PR == nil || row.PR.PRNumber == 0 {
			c, _, err := p.discoverBranchPR(ctx, row, listings)
			changed = changed || c
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if strings.EqualFold(row.PR.State, "MERGED") {
			continue // a merge is final
		}
		c, err := p.refreshBranchState(ctx, row)
		changed = changed || c
		if err != nil {
			errs = append(errs, fmt.Errorf("refresh %s PR #%d: %w", taskID, row.PR.PRNumber, err))
		}
	}
	if p.writeSummary(ctx, taskID, "", "") {
		changed = true
	}
	p.publishIfChanged(changed)
	return errors.Join(errs...)
}

// refreshBranchState fetches a branch's PR again by number, its state only,
// keeping the reviews and checks it had. A failure keeps the last PR it had
// and records the error.
func (p *prTrackingService) refreshBranchState(ctx context.Context, row store.TaskBranch) (bool, error) {
	owner, name, ok := splitRepo(row.Repo)
	if !ok {
		return false, fmt.Errorf("repository must be owner/name")
	}
	status, err := p.gh.FetchPRState(owner, name, row.PR.PRNumber)
	if err != nil {
		return p.setBranchPR(ctx, row, withOutcome(row.PR, "error", err.Error())), err
	}
	return p.setBranchPR(ctx, row, withState(row.PR, status)), nil
}

// withState is a PR with the state FetchPRState read, keeping its review
// decision, check status, pin, match method, head SHA and link time.
func withState(known *store.PRMeta, status *github.PRStatus) *store.PRMeta {
	pr := *known
	pr.PRURL, pr.State, pr.IsDraft = status.URL, status.State, status.IsDraft
	pr.Base, pr.Head, pr.CommentCount, pr.CreatedAt = status.BaseBranch, status.HeadSHA, status.CommentCount, status.CreatedAt
	pr.SyncOutcome, pr.SyncError = "linked", ""
	return &pr
}

// discoverBranchPR looks for the PR of a branch that has none in its
// repository's listing, and stores what it finds (the PR, no_match, or the
// listing's error). It reports whether the branch changed and whether a PR
// was found.
func (p *prTrackingService) discoverBranchPR(ctx context.Context, row store.TaskBranch, listings *prListings) (changed, found bool, err error) {
	prs, err := listings.get(row.Repo)
	if err != nil {
		return p.setBranchPR(ctx, row, withOutcome(row.PR, "error", err.Error())), false, err
	}
	matches := filterPRs(prs, func(pr github.PRStatus) bool {
		return pr.HeadBranch == row.Branch && (pr.HeadRepo == "" || pr.HeadRepo == row.Repo)
	})
	if len(matches) == 0 {
		return p.setBranchPR(ctx, row, withOutcome(row.PR, "no_match", "")), false, nil
	}
	best := pickServicePR(matches)
	return p.setBranchPR(ctx, row, prFromStatus(&best, "branch", false)), true, nil
}

// syncMode says what one pass over a task does.
type syncMode struct {
	// rediscover forgets the PRs found before (and the branches found by
	// commit or ticket key) and looks again.
	rediscover bool
	// discover looks for the PRs of branches that have none.
	discover bool
	// refresh fetches the PRs found before again, by number.
	refresh bool
}

// prListings lists each repository's PRs at most once in a pass.
type prListings struct {
	gh   GitHubClient
	prs  map[string][]github.PRStatus
	errs map[string]error
}

func newPRListings(gh GitHubClient) *prListings {
	return &prListings{gh: gh, prs: map[string][]github.PRStatus{}, errs: map[string]error{}}
}

func (l *prListings) get(repo string) ([]github.PRStatus, error) {
	if prs, ok := l.prs[repo]; ok {
		return prs, nil
	}
	if err, ok := l.errs[repo]; ok {
		return nil, err
	}
	owner, name, ok := splitRepo(repo)
	if !ok {
		return nil, fmt.Errorf("repository must be owner/name")
	}
	prs, err := l.gh.ListRepositoryPRs(owner, name)
	if err != nil {
		err = fmt.Errorf("list %s pull requests: %w", repo, err)
		l.errs[repo] = err
		return nil, err
	}
	l.prs[repo] = prs
	return prs, nil
}

// taskContext reads a task and its repository context, recovering the
// context from its worktree when it has none.
func (p *prTrackingService) taskContext(ctx context.Context, taskID string) (*store.Task, *store.CodeContext, error) {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	cc, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return nil, nil, err
	}
	if cc == nil || cc.Repo == "" {
		cc = p.contextFromWorktree(ctx, task)
	}
	return task, cc, nil
}

// SetCheckoutHead gives PR tracking a way to read the branch a checkout
// has checked out, so a task's agent-sourced code context follows its
// agent to another branch (followCheckout). The runtime gives it
// gitrepo.Head, which reads git's files and runs nothing in the checkout.
var _ interface {
	SetCheckoutHead(func(string) (string, string, error))
} = (*prTrackingService)(nil)

func (p *prTrackingService) SetCheckoutHead(head func(dir string) (branch, commit string, err error)) {
	p.checkoutHead = head
}

// followCheckout moves a code context Legato recorded for an agent (from
// its job clone or its working directory) to the branch the agent's
// checkout is on now, keeping the branch it was on tracked (source agent),
// so a stack built in the clone is found branch by branch. A context the
// person set (manual), a handoff's or a worktree's never moves, nor does
// one whose checkout is detached, gone or on the default branch. Nor does
// one whose checkout the task no longer owns (ownsCheckout).
func (p *prTrackingService) followCheckout(ctx context.Context, taskID string, cc *store.CodeContext) *store.CodeContext {
	if p.checkoutHead == nil || cc == nil || cc.Repo == "" || cc.WorkingDir == "" || cc.RepoSource == "manual" ||
		(cc.BranchSource != "agent" && cc.BranchSource != "job_clone") {
		return cc
	}
	if !p.ownsCheckout(ctx, taskID, cc.WorkingDir) {
		return cc
	}
	branch, commit, err := p.checkoutHead(cc.WorkingDir)
	if err != nil || branch == "" || branch == cc.Branch || isDefaultBranch(branch, cc.BaseBranch) {
		return cc
	}
	if cc.Branch != "" && !isDefaultBranch(cc.Branch, cc.BaseBranch) {
		if _, err := p.store.TrackTaskBranches(ctx, taskID, []store.BranchRef{{Repo: cc.Repo, Branch: cc.Branch}}, BranchSourceAgent); err != nil {
			return cc
		}
	}
	moved := *cc
	moved.Branch, moved.HeadSHA, moved.BranchSource = branch, commit, "agent"
	moved.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := p.store.SetCodeContext(ctx, taskID, &moved); err != nil {
		return cc
	}
	return &moved
}

// ownsCheckout says whether the checkout at dir is the task's now: the
// task's running main-slot agent works in it, and no other task's running
// agent does. Neither who a job clone was made for nor which tasks were
// attached to it says who uses it now (a clone is reused by another task
// once its agent ends), so a checkout no agent of the task runs in is
// never followed.
func (p *prTrackingService) ownsCheckout(ctx context.Context, taskID, dir string) bool {
	sessions, err := p.store.ListAgentSessions(ctx)
	if err != nil {
		return false
	}
	owns := false
	for _, session := range sessions {
		if session.WorkingDir != dir {
			continue
		}
		if session.TaskID != taskID {
			return false
		}
		owns = owns || session.Slot == "" || session.Slot == store.SlotMain
	}
	return owns
}

// trackContextBranches adds the code context's branch and the branches of
// the Staccato stacks recorded for the task's job clones.
func (p *prTrackingService) trackContextBranches(ctx context.Context, taskID string, cc *store.CodeContext) error {
	if cc != nil && cc.Repo != "" && !isDefaultBranch(cc.Branch, cc.BaseBranch) {
		if _, err := p.store.TrackTaskBranches(ctx, taskID, []store.BranchRef{{Repo: cc.Repo, Branch: cc.Branch}}, BranchSourceCodeContext); err != nil {
			return err
		}
	}
	stacks, err := p.store.ListTaskStacks(ctx, taskID)
	if err != nil {
		return err
	}
	for _, stack := range stacks {
		if stack.Repo == "" {
			continue
		}
		var branches []TaskStackBranch
		if json.Unmarshal([]byte(stack.Branches), &branches) != nil {
			continue
		}
		refs := make([]store.BranchRef, 0, len(branches))
		for _, b := range branches {
			if b.State == "pushed" || b.State == "unpushed" {
				refs = append(refs, store.BranchRef{Repo: stack.Repo, Branch: b.Name})
			}
		}
		if _, err := p.store.TrackTaskBranches(ctx, taskID, refs, BranchSourceStack); err != nil {
			return err
		}
	}
	return nil
}

// syncTask brings a task's tracked branches and their PRs up to date, as
// mode says, and the single PR field after them. It reports whether
// anything a person sees changed. An ephemeral task links no PRs (LEG-519):
// polls never refresh it, and its working folder's branch is not its work.
func (p *prTrackingService) syncTask(ctx context.Context, task *store.Task, cc *store.CodeContext, mode syncMode, listings *prListings) (bool, error) {
	if task.Ephemeral {
		return false, nil
	}
	taskID := task.ID
	changed := false
	var errs []error
	if mode.rediscover {
		if err := p.forgetPRs(ctx, taskID); err != nil {
			return false, err
		}
		changed = true
	}
	cc = p.followCheckout(ctx, taskID, cc)
	if err := p.trackContextBranches(ctx, taskID, cc); err != nil {
		return changed, err
	}
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return changed, err
	}

	// A task linked before branches were tracked holds its PR in the single
	// field only. It is moved onto its branch when it is next refreshed;
	// until then it counts as found.
	if legacy, _ := store.ParsePRMeta(task.PRMeta); !mode.rediscover && legacy != nil && legacy.PRNumber > 0 && !holdsPR(rows, legacy.PRNumber) {
		if !mode.refresh || cc == nil || cc.Repo == "" {
			return changed, nil
		}
		adopted, err := p.adoptLegacyPR(ctx, taskID, cc, legacy, false)
		if err != nil {
			if p.applyDiscoveryOutcome(ctx, taskID, "error", err.Error()) {
				changed = true
			}
			return changed, err
		}
		changed = changed || adopted
		if rows, err = p.store.ListTaskBranches(ctx, taskID); err != nil {
			return changed, err
		}
	}

	if mode.refresh {
		for _, row := range rows {
			if row.PR == nil || row.PR.PRNumber == 0 {
				continue
			}
			c, err := p.refreshBranchPR(ctx, row)
			changed = changed || c
			if err != nil {
				errs = append(errs, fmt.Errorf("refresh %s PR #%d: %w", taskID, row.PR.PRNumber, err))
			}
		}
	}

	outcome, syncError := "", ""
	if mode.discover {
		var newly []store.TaskBranch
		linked := false
		for _, row := range rows {
			if row.PR != nil && row.PR.PRNumber > 0 {
				linked = true
				continue
			}
			c, found, err := p.discoverBranchPR(ctx, row, listings)
			changed = changed || c
			if err != nil {
				errs = append(errs, err)
			}
			if found {
				newly = append(newly, row)
				linked = true
			}
		}

		// Nothing found by branch: look by commit, then by the task's key in
		// branch names, where several PRs that form one stack are taken
		// together.
		if !linked && cc != nil && cc.Repo != "" {
			prs, err := listings.get(cc.Repo)
			if err != nil {
				errs = append(errs, err)
				outcome, syncError = "error", err.Error()
			} else {
				found, method, result := matchTaskPRs(taskID, cc.Repo, cc, prs)
				outcome = result
				refs := make([]store.BranchRef, 0, len(found))
				for _, pr := range found {
					refs = append(refs, store.BranchRef{Repo: cc.Repo, Branch: pr.HeadBranch})
				}
				if _, err := p.store.TrackTaskBranches(ctx, taskID, refs, BranchSourceDiscovered); err != nil {
					return changed, err
				}
				for i := range found {
					row := store.TaskBranch{TaskID: taskID, Repo: cc.Repo, Branch: found[i].HeadBranch}
					if p.setBranchPR(ctx, row, prFromStatus(&found[i], method, false)) {
						changed = true
					}
					newly = append(newly, row)
				}
			}
		}

		// The listing has no reviews or checks: fetch what was just found.
		for _, row := range newly {
			current, err := p.branch(ctx, row)
			if err != nil || current.PR == nil || current.PR.PRNumber == 0 {
				continue
			}
			c, err := p.refreshBranchPR(ctx, current)
			changed = changed || c
			if err != nil {
				errs = append(errs, fmt.Errorf("refresh %s PR #%d: %w", taskID, current.PR.PRNumber, err))
			}
		}
	}

	if p.writeSummary(ctx, taskID, outcome, syncError) {
		changed = true
	}
	return changed, errors.Join(errs...)
}

// forgetPRs clears what discovery found for a task: the PRs of its
// branches (pins included), the branches found by commit or ticket key, and
// the single PR field.
func (p *prTrackingService) forgetPRs(ctx context.Context, taskID string) error {
	if err := p.store.RemoveTaskBranches(ctx, taskID, BranchSourceDiscovered); err != nil {
		return err
	}
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := p.store.SetTaskBranchPR(ctx, taskID, row.Repo, row.Branch, nil); err != nil {
			return err
		}
	}
	return p.store.UpdatePRMeta(ctx, taskID, nil)
}

// adoptLegacyPR moves a PR held only in the single field onto the branch
// it is on, keeping its pin and how it was matched. stateOnly fetches its
// state alone (FetchPRState) and keeps the reviews and checks it had.
func (p *prTrackingService) adoptLegacyPR(ctx context.Context, taskID string, cc *store.CodeContext, legacy *store.PRMeta, stateOnly bool) (bool, error) {
	owner, name, ok := splitRepo(cc.Repo)
	if !ok {
		return false, fmt.Errorf("repository must be owner/name")
	}
	fetch := p.gh.FetchPRByNumber
	if stateOnly {
		fetch = p.gh.FetchPRState
	}
	status, err := fetch(owner, name, legacy.PRNumber)
	if err != nil {
		return false, err
	}
	branch := status.HeadBranch
	if branch == "" {
		branch = cc.Branch
	}
	if branch == "" {
		return false, fmt.Errorf("PR #%d names no head branch", legacy.PRNumber)
	}
	source := BranchSourceDiscovered
	switch {
	case branch == cc.Branch:
		source = BranchSourceCodeContext
	case legacy.Pinned:
		source = BranchSourceManual
	}
	if _, err := p.store.TrackTaskBranches(ctx, taskID, []store.BranchRef{{Repo: cc.Repo, Branch: branch}}, source); err != nil {
		return false, err
	}
	pr := prFromStatus(status, legacy.MatchMethod, legacy.Pinned)
	pr.HeadSHA, pr.LinkedAt = legacy.HeadSHA, legacy.LinkedAt
	if stateOnly {
		pr = withState(legacy, status)
		pr.Repo, pr.Branch = "", ""
	}
	return true, p.store.SetTaskBranchPR(ctx, taskID, cc.Repo, branch, pr)
}

// refreshBranchPR fetches a branch's PR again by number. A failure keeps
// the last PR it had and records the error.
func (p *prTrackingService) refreshBranchPR(ctx context.Context, row store.TaskBranch) (bool, error) {
	owner, name, ok := splitRepo(row.Repo)
	if !ok {
		return false, fmt.Errorf("repository must be owner/name")
	}
	status, err := p.gh.FetchPRByNumber(owner, name, row.PR.PRNumber)
	if err != nil {
		return p.setBranchPR(ctx, row, withOutcome(row.PR, "error", err.Error())), err
	}
	pr := prFromStatus(status, row.PR.MatchMethod, row.PR.Pinned)
	pr.HeadSHA, pr.LinkedAt = row.PR.HeadSHA, row.PR.LinkedAt
	p.observeReviews(ctx, row, status)
	return p.setBranchPR(ctx, row, pr), nil
}

func (p *prTrackingService) branch(ctx context.Context, ref store.TaskBranch) (store.TaskBranch, error) {
	rows, err := p.store.ListTaskBranches(ctx, ref.TaskID)
	if err != nil {
		return store.TaskBranch{}, err
	}
	for _, row := range rows {
		if row.Repo == ref.Repo && row.Branch == ref.Branch {
			return row, nil
		}
	}
	return store.TaskBranch{}, store.ErrNotFound
}

// setBranchPR stores a branch's PR when it differs from what the branch
// had, and reports whether it did.
func (p *prTrackingService) setBranchPR(ctx context.Context, row store.TaskBranch, pr *store.PRMeta) bool {
	var before *store.PRMeta
	if current, err := p.branch(ctx, row); err == nil {
		if samePR(current.PR, pr) {
			return false
		}
		before = current.PR
	}
	if pr != nil {
		pr.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if p.store.SetTaskBranchPR(ctx, row.TaskID, row.Repo, row.Branch, pr) != nil {
		return false
	}
	p.publishPREdge(ctx, row.TaskID, before, pr)
	return true
}

// publishPREdge publishes the stage milestone of a branch's PR changing
// from before to after, if it is one: opened ready for review (a new PR,
// which may follow finished work), a draft marked ready, or merged or
// closed leaving every PR of the task's own merged or closed with one
// merged. A PR opened before the task's work began is inherited
// (inheritedPR): newly tracked, open or merged, it is no edge (LEG-519),
// and its merge or close is none either (LEG-626). Only a draft marked
// ready while the agent runs moves the stage.
func (p *prTrackingService) publishPREdge(ctx context.Context, taskID string, before, after *store.PRMeta) {
	if p.bus == nil || after == nil || after.PRNumber == 0 {
		return
	}
	began := p.workBegan(ctx, taskID)
	inherited := inheritedPR(after, began)
	newPR := before == nil || before.PRNumber != after.PRNumber
	if newPR && inherited {
		return
	}
	switch {
	case after.State == "OPEN" && !after.IsDraft && newPR:
		publishMilestone(p.bus, events.WorkMilestonePayload{TaskID: taskID, Stage: store.WorkStageReviewing,
			Reason: fmt.Sprintf("PR #%d open", after.PRNumber), Back: true})
	case after.State == "OPEN" && !after.IsDraft && !newPR && before.State == "OPEN" && before.IsDraft:
		publishMilestone(p.bus, events.WorkMilestonePayload{TaskID: taskID, Stage: store.WorkStageReviewing,
			Reason: fmt.Sprintf("PR #%d ready for review", after.PRNumber)})
	case (after.State == "MERGED" || after.State == "CLOSED") && (newPR || before.State != after.State) && !inherited:
		if p.everyPRMerged(ctx, taskID, began) {
			publishMilestone(p.bus, events.WorkMilestonePayload{TaskID: taskID, Stage: store.WorkStageFinished, Reason: "every PR merged"})
		}
	}
}

// workBegan is when the work of a task's main slot began: its newest stage
// start, which a resume, reload or sandbox switch keeps, else its running
// agent's start, else zero (not known).
func (p *prTrackingService) workBegan(ctx context.Context, taskID string) time.Time {
	if at, err := p.store.WorkStageStartedAt(ctx, TaskPlace(taskID, "")); err == nil {
		if began := parseAddedAt(at); !began.IsZero() {
			return began
		}
	}
	session, err := p.store.GetAgentSessionInSlot(ctx, taskID, store.SlotMain)
	if err != nil {
		return time.Time{}
	}
	return parseAddedAt(session.StartedAt)
}

// inheritedPR says whether a PR was opened before the work began. GitHub
// gives the opening time to the second, so a PR opened in the second the
// work began is the work's own. Without either time it says false: the PR
// counts as the work's own.
func inheritedPR(pr *store.PRMeta, began time.Time) bool {
	created := parseAddedAt(pr.CreatedAt)
	if created.IsZero() || began.IsZero() {
		return false
	}
	return created.Before(began.Truncate(time.Second))
}

// everyPRMerged says whether each of a task's own PRs (inheritedPR against
// began) is merged or closed, with at least one merged; branches with no
// PR, and inherited PRs, are skipped.
func (p *prTrackingService) everyPRMerged(ctx context.Context, taskID string, began time.Time) bool {
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return false
	}
	merged := false
	for _, row := range rows {
		if row.PR == nil || row.PR.PRNumber == 0 || inheritedPR(row.PR, began) {
			continue
		}
		switch row.PR.State {
		case "MERGED":
			merged = true
		case "CLOSED":
		default:
			return false
		}
	}
	return merged
}

// writeSummary keeps the single PR field naming the task's first PR in
// stack order, or, with none, how discovery last ended. It reports whether
// the field changed.
func (p *prTrackingService) writeSummary(ctx context.Context, taskID, outcome, syncError string) bool {
	task, err := p.store.GetTask(ctx, taskID)
	if err != nil {
		return false
	}
	old, _ := store.ParsePRMeta(task.PRMeta)
	rows, err := p.store.ListTaskBranches(ctx, taskID)
	if err != nil {
		return false
	}
	var summary *store.PRMeta
	for _, row := range rows {
		if row.PR != nil && row.PR.PRNumber > 0 {
			copy := *row.PR
			copy.Repo, copy.Branch = row.Repo, row.Branch
			summary = &copy
			break
		}
	}
	if summary == nil {
		if old != nil && old.PRNumber > 0 && !holdsPR(rows, old.PRNumber) {
			return false // a PR from before branches were tracked, not yet moved
		}
		if outcome == "" {
			for _, row := range rows {
				if row.PR != nil && row.PR.SyncOutcome == "error" {
					outcome, syncError = "error", row.PR.SyncError
					break
				}
				if row.PR != nil && row.PR.SyncOutcome != "" {
					outcome = row.PR.SyncOutcome
				}
			}
		}
		if outcome == "" {
			return false
		}
		summary = &store.PRMeta{SyncOutcome: outcome, SyncError: syncError}
		if old != nil {
			summary.HeadSHA, summary.LinkedAt = old.HeadSHA, old.LinkedAt
		}
	}
	if old != nil && samePR(old, summary) && old.Repo == summary.Repo && old.Branch == summary.Branch {
		return false
	}
	summary.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := store.MarshalPRMeta(summary)
	if err != nil {
		return false
	}
	return p.store.UpdatePRMeta(ctx, taskID, raw) == nil
}

func (p *prTrackingService) publishIfChanged(changed bool) {
	if changed && p.bus != nil {
		p.bus.Publish(prStatusUpdated())
	}
}

// matchTaskPRs finds a task's PRs in a repository's when none of its
// branches has one: one by commit, else by the task's key in branch names.
// Several key matches are taken when they form one stack (each based on the
// one before), and are otherwise ambiguous.
func matchTaskPRs(taskID, repo string, cc *store.CodeContext, prs []github.PRStatus) ([]github.PRStatus, string, string) {
	status, method, outcome := matchContextPR(taskID, repo, cc, prs)
	if status != nil {
		return []github.PRStatus{*status}, method, outcome
	}
	if outcome != "ambiguous" {
		return nil, "", outcome
	}
	if cc.HeadSHA != "" && len(filterPRs(prs, func(pr github.PRStatus) bool { return pr.HeadSHA == cc.HeadSHA })) > 0 {
		return nil, "", outcome
	}
	keyed := filterPRs(prs, func(pr github.PRStatus) bool {
		return !isDefaultBranch(pr.HeadBranch, cc.BaseBranch) && branchContainsKey(pr.HeadBranch, taskID)
	})
	if chain := stackChain(keyed); chain != nil {
		return chain, "ticket_key", "linked"
	}
	return nil, "", outcome
}

// stackChain orders PRs bottom first when they form a single stack: one is
// based outside the set, and each other is based on exactly the head of
// the one before it. It returns nil otherwise.
func stackChain(prs []github.PRStatus) []github.PRStatus {
	heads := map[string]bool{}
	for _, pr := range prs {
		if heads[pr.HeadBranch] {
			return nil
		}
		heads[pr.HeadBranch] = true
	}
	var bottom *github.PRStatus
	above := map[string][]github.PRStatus{}
	for i := range prs {
		if heads[prs[i].BaseBranch] {
			above[prs[i].BaseBranch] = append(above[prs[i].BaseBranch], prs[i])
			continue
		}
		if bottom != nil {
			return nil
		}
		bottom = &prs[i]
	}
	if bottom == nil {
		return nil
	}
	chain := []github.PRStatus{*bottom}
	for next := above[bottom.HeadBranch]; len(next) > 0; next = above[next[0].HeadBranch] {
		if len(next) > 1 {
			return nil
		}
		chain = append(chain, next[0])
	}
	if len(chain) != len(prs) {
		return nil
	}
	return chain
}

func prFromStatus(status *github.PRStatus, method string, pinned bool) *store.PRMeta {
	return &store.PRMeta{
		PRNumber: status.Number, PRURL: status.URL, State: status.State,
		IsDraft: status.IsDraft, ReviewDecision: status.ReviewDecision,
		CheckStatus: status.CheckStatus, CommentCount: status.CommentCount, Base: status.BaseBranch,
		Pinned: pinned, MatchMethod: method, SyncOutcome: "linked", Head: status.HeadSHA,
		CreatedAt: status.CreatedAt,
	}
}

// withOutcome is a branch's PR with a new discovery outcome, keeping the
// last PR it had.
func withOutcome(pr *store.PRMeta, outcome, syncError string) *store.PRMeta {
	next := &store.PRMeta{}
	if pr != nil {
		copy := *pr
		next = &copy
	}
	next.SyncOutcome, next.SyncError = outcome, syncError
	return next
}

func samePR(a, b *store.PRMeta) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	// Head counts: a push alone changes a PR's cached head, which the
	// review prompt's revision follows.
	// So does Base, so a refresh fills it for a PR tracked before it was
	// kept.
	return !prMetaChanged(a, b) && a.Pinned == b.Pinned && a.MatchMethod == b.MatchMethod &&
		a.SyncOutcome == b.SyncOutcome && a.SyncError == b.SyncError && a.Head == b.Head && a.Base == b.Base
}

func holdsPR(rows []store.TaskBranch, number int) bool {
	for _, row := range rows {
		if row.PR != nil && row.PR.PRNumber == number {
			return true
		}
	}
	return false
}

// PullRequestLister lists tasks' PRs in stack order without GitHub. The
// board service is one.
type PullRequestLister interface {
	ListPullRequests(ctx context.Context, taskIDs []string) (map[string][]PRMetaView, error)
}

// ListPullRequests lists the PRs of the given tasks, each in stack order;
// tasks without one are left out.
func (b *boardService) ListPullRequests(ctx context.Context, taskIDs []string) (map[string][]PRMetaView, error) {
	tracked, err := b.store.ListTrackedTaskBranches(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]PRMetaView, len(taskIDs))
	for _, id := range taskIDs {
		task, err := b.store.GetTask(ctx, id)
		if err != nil {
			continue
		}
		if prs := TaskPullRequests(task, tracked[id]); len(prs) > 0 {
			out[id] = prs
		}
	}
	return out, nil
}

// TaskPullRequests lists a task's PRs in stack order, from its tracked
// branches; a task linked before branches were tracked has its one PR from
// the single field until that PR is moved onto its branch.
func TaskPullRequests(task *store.Task, branches []store.TaskBranch) []PRMetaView {
	out := make([]PRMetaView, 0, len(branches))
	for _, b := range branches {
		if b.PR != nil && b.PR.PRNumber > 0 {
			out = append(out, prView(b.Repo, b.Branch, b.PR))
		}
	}
	if len(out) > 0 {
		return stackOrder(out)
	}
	legacy, err := store.ParsePRMeta(task.PRMeta)
	if err != nil || legacy == nil || legacy.PRNumber == 0 {
		return out
	}
	repo, branch := legacy.Repo, legacy.Branch
	if cc, _ := store.ParseCodeContext(task.CodeContext); cc != nil {
		if repo == "" {
			repo = cc.Repo
		}
		if branch == "" {
			branch = cc.Branch
		}
	}
	return append(out, prView(repo, branch, legacy))
}

// stackOrder puts a task's PRs in the order they merge, bottom first. Each
// repository keeps the place of its first PR. Within one, the roots (a PR
// whose base is no other PR's head) come first by number, then the PRs
// built on each, level by level, each PR's children by number. A PR with
// no base yet (fetched before bases were kept) follows, in stored order.
func stackOrder(prs []PRMetaView) []PRMetaView {
	var repos []string
	byRepo := map[string][]PRMetaView{}
	for _, pr := range prs {
		if _, ok := byRepo[pr.Repo]; !ok {
			repos = append(repos, pr.Repo)
		}
		byRepo[pr.Repo] = append(byRepo[pr.Repo], pr)
	}
	out := make([]PRMetaView, 0, len(prs))
	for _, repo := range repos {
		out = append(out, repoStackOrder(byRepo[repo])...)
	}
	return out
}

func repoStackOrder(prs []PRMetaView) []PRMetaView {
	heads := map[string]bool{}
	for _, pr := range prs {
		heads[pr.Branch] = true
	}
	children := map[string][]PRMetaView{}
	var roots, unknown []PRMetaView
	for _, pr := range prs {
		switch {
		case pr.Base == "":
			unknown = append(unknown, pr)
		case heads[pr.Base] && pr.Base != pr.Branch:
			children[pr.Base] = append(children[pr.Base], pr)
		default:
			roots = append(roots, pr)
		}
	}
	byNumber := func(list []PRMetaView) {
		sort.SliceStable(list, func(i, j int) bool { return list[i].PRNumber < list[j].PRNumber })
	}
	byNumber(roots)
	out := make([]PRMetaView, 0, len(prs))
	placed := map[string]bool{}
	walk := func(level []PRMetaView) {
		for len(level) > 0 {
			var next []PRMetaView
			for _, pr := range level {
				if placed[pr.Branch] {
					continue
				}
				placed[pr.Branch] = true
				out = append(out, pr)
				kids := append([]PRMetaView(nil), children[pr.Branch]...)
				byNumber(kids)
				next = append(next, kids...)
			}
			level = next
		}
	}
	walk(roots)
	// A PR with no base yet, and those built on it; then any left in a
	// cycle, in stored order.
	for _, pr := range unknown {
		walk([]PRMetaView{pr})
	}
	for _, pr := range prs {
		if !placed[pr.Branch] {
			placed[pr.Branch] = true
			out = append(out, pr)
		}
	}
	return out
}

func prView(repo, branch string, pr *store.PRMeta) PRMetaView {
	return PRMetaView{
		Repo: repo, Branch: branch, PRNumber: pr.PRNumber, PRURL: pr.PRURL, State: pr.State,
		IsDraft: pr.IsDraft, ReviewDecision: pr.ReviewDecision, CheckStatus: pr.CheckStatus,
		CommentCount: pr.CommentCount, Base: pr.Base, Pinned: pr.Pinned, MatchMethod: pr.MatchMethod,
		SyncOutcome: pr.SyncOutcome, SyncError: pr.SyncError, Head: pr.Head,
	}
}

// TrackedBranchViews lists a task's tracked branches for display, each with
// its PR when one was found.
func TrackedBranchViews(branches []store.TaskBranch) []TrackedBranchView {
	out := make([]TrackedBranchView, 0, len(branches))
	for _, b := range branches {
		view := TrackedBranchView{Repo: b.Repo, Branch: b.Branch, Source: b.Source}
		if b.PR != nil {
			pr := prView(b.Repo, b.Branch, b.PR)
			view.PR = &pr
		}
		out = append(out, view)
	}
	return out
}
