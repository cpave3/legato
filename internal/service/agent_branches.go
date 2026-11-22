package service

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// agentBranchPrefix is the branches an agent's push is tracked for: those
// of the convention agents push under (the github profile's session grant).
const agentBranchPrefix = "legato/"

// AgentBranch is a branch an agent's session pushed through Legato's proxy,
// or opened a pull request from (Opened), as the credential channels saw it.
type AgentBranch struct {
	TaskID      string
	ExecutionID string
	Repo        string
	Branch      string
	Opened      bool
}

// AgentBranches tracks the branches agents push and open pull requests
// from on their tasks the moment the broker sees them, so a card, a review
// loop or an Attention item finds a pull request without waiting for the
// next poll: a pushed legato/ branch is added to the task's tracked
// branches (source agent), and its pull request looked for when it is new;
// a pushed branch already tracked is looked for again while it has none;
// a pull request opened from any branch adds the branch and is always looked
// for. Discovery runs off the caller's goroutine, one at a time per task,
// as away tasks' does. See docs/ai/pr-tracking.md § Agents' branches.
type AgentBranches struct {
	db  *store.Store
	prs PRBranchActions
	run *awayPRs
}

// NewAgentBranches tracks agents' branches; prs nil records them only, for
// the PR tracking a later start has.
func NewAgentBranches(db *store.Store, prs PRBranchActions) *AgentBranches {
	return &AgentBranches{db: db, prs: prs, run: newAwayPRs(db, prs)}
}

// Track records one branch on the session's task and starts looking for
// its pull request.
func (a *AgentBranches) Track(b AgentBranch) {
	if a == nil || b.TaskID == "" || b.Repo == "" || b.Branch == "" {
		return
	}
	if !b.Opened && !strings.HasPrefix(b.Branch, agentBranchPrefix) {
		return
	}
	ctx := context.Background()
	a.trackContextBranch(ctx, b)
	added, err := a.db.TrackTaskBranches(ctx, b.TaskID, []store.BranchRef{{Repo: b.Repo, Branch: b.Branch}}, BranchSourceAgent)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Printf("task %s: tracking the branch %s its agent pushed: %v", b.TaskID, b.Branch, err)
		}
		return
	}
	if a.prs != nil && (b.Opened || len(added) > 0 || a.lacksPR(ctx, b)) {
		a.run.discover(b.TaskID, a.prs)
	}
}

// lacksPR says whether the pushed branch is tracked with no pull request
// found yet: a push looks again at once, so a stack whose pull requests
// opened after a first look is found whole.
func (a *AgentBranches) lacksPR(ctx context.Context, b AgentBranch) bool {
	rows, err := a.db.ListTaskBranches(ctx, b.TaskID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.Repo == b.Repo && row.Branch == b.Branch {
			return row.PR == nil || row.PR.PRNumber == 0
		}
	}
	return false
}

// trackContextBranch tracks the task's code context branch before an
// agent's new branch, as a sync would, so the stack keeps the branch the
// agent started on below those it built from it.
func (a *AgentBranches) trackContextBranch(ctx context.Context, b AgentBranch) {
	task, err := a.db.GetTask(ctx, b.TaskID)
	if err != nil {
		return
	}
	cc, err := store.ParseCodeContext(task.CodeContext)
	if err != nil || cc == nil || cc.Repo != b.Repo || cc.Branch == b.Branch || isDefaultBranch(cc.Branch, cc.BaseBranch) {
		return
	}
	if _, err := a.db.TrackTaskBranches(ctx, b.TaskID, []store.BranchRef{{Repo: cc.Repo, Branch: cc.Branch}}, BranchSourceCodeContext); err != nil {
		log.Printf("task %s: tracking its code context's branch: %v", b.TaskID, err)
	}
}

// Wait waits for the discoveries under way.
func (a *AgentBranches) Wait() { a.run.wait() }
