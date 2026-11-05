package service

import (
	"context"
	"log"
	"sync"

	"github.com/cpave3/legato/internal/engine/store"
)

// A main tracks the PRs of a task away on a worker itself, with its own
// GitHub read token: the worker reports the branches it pushed for the
// task, in stack order, in each summary row, and the main adds them to the
// task's tracked branches (source api, as Return adds them) and finds their
// PRs by repository and branch. A branch it has not seen before is looked
// for at once, not at the next poll; after that the PR follows the main's
// normal polling, as any task's does, since an away task is an ordinary
// task on the main. See docs/ai/pr-tracking.md § Away tasks.

// awayPRs keeps away tasks' tracked branches up to date from summary rows
// and runs a task's discovery off the connection's read loop, one at a
// time per task, so a slow GitHub never holds up a worker's rows.
type awayPRs struct {
	db *store.Store

	mu       sync.Mutex
	prs      PRBranchActions
	inFlight map[string]bool // task ID → a discovery runs
	again    map[string]bool // task ID → a branch came while it ran
	wg       sync.WaitGroup
}

func newAwayPRs(db *store.Store, prs PRBranchActions) *awayPRs {
	return &awayPRs{db: db, prs: prs, inFlight: map[string]bool{}, again: map[string]bool{}}
}

func (a *awayPRs) setTracking(prs PRBranchActions) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prs = prs
}

// track adds the branches a row brings for an away task, in order, and
// starts a discovery for the task when any is new. Without PR tracking it
// only records them, for the PR tracking a later start of the main has.
func (a *awayPRs) track(ctx context.Context, away store.AwayTask, branches []string) {
	if len(branches) == 0 || away.Repo == "" {
		return
	}
	a.mu.Lock()
	prs := a.prs
	a.mu.Unlock()
	var added []string
	var err error
	if prs != nil {
		added, err = prs.AddTaskBranches(ctx, away.TaskID, away.Repo, branches)
	} else {
		added, err = AddTaskBranches(ctx, a.db, away.TaskID, away.Repo, branches)
	}
	if err != nil {
		log.Printf("away task %s: tracking its branches: %v", away.TaskID, err)
		return
	}
	if len(added) > 0 && prs != nil {
		a.discover(away.TaskID, prs)
	}
}

// discover runs DiscoverPRs for a task in the background; while one runs,
// a second request makes it run once more when it ends.
func (a *awayPRs) discover(taskID string, prs PRBranchActions) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inFlight[taskID] {
		a.again[taskID] = true
		return
	}
	a.inFlight[taskID] = true
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			// Not the connection's context: a discovery under way finishes
			// when the connection drops.
			if err := prs.DiscoverPRs(context.Background(), taskID); err != nil {
				log.Printf("away task %s: looking for its PRs: %v", taskID, err)
			}
			a.mu.Lock()
			if !a.again[taskID] {
				delete(a.inFlight, taskID)
				a.mu.Unlock()
				return
			}
			delete(a.again, taskID)
			a.mu.Unlock()
		}
	}()
}

// wait waits for the discoveries under way.
func (a *awayPRs) wait() { a.wg.Wait() }

// SetPRTracking gives the connections the main's PR tracking, which finds
// the PRs of the branches away tasks' rows bring; nil records the branches
// only.
func (c *WorkerConnections) SetPRTracking(prs PRBranchActions) {
	c.prs.setTracking(prs)
}
