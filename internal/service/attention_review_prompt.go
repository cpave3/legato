package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// ReviewPromptAttentionProducer names the Attention producer that prompts a
// review of local work.
const ReviewPromptAttentionProducer = "review_prompt"

// AttentionSignalPRsUnreviewed: a local card reached Done or the review
// column with open pull requests no review loop reviews.
const AttentionSignalPRsUnreviewed = "prs_unreviewed"

// ReviewPromptAttentionAdapter prompts a review of work done locally: a
// card that is not away (and not a main's handoff), in the Done or review
// column, that tracks open pull requests none of which an active review
// loop reviews, raises one item whose action starts the built-in
// review-loop on the stack, with the card's agent as the author when
// Legato's author App opened them (agentAuthoredPRs). It prompts; it never
// starts a loop by itself.
type ReviewPromptAttentionAdapter struct {
	db       *store.Store
	launcher *TaskWorkflowLauncher
	mu       sync.RWMutex
	reviews  *ReviewPassService
}

// NewReviewPromptAttentionAdapter is the producer; launcher starts the
// workflow (nil: it is never offered). It says review passes are off until
// SetReviews gives it the engine.
func NewReviewPromptAttentionAdapter(db *store.Store, launcher *TaskWorkflowLauncher) *ReviewPromptAttentionAdapter {
	return &ReviewPromptAttentionAdapter{db: db, launcher: launcher}
}

// heldBy is the loop that holds a pull request, whichever caller opened it,
// nil when none does: the claim on the loop record (which a process without
// the engine reads too), else, with the engine here, a review step still
// waiting to open on it.
func (a *ReviewPromptAttentionAdapter) heldBy(ctx context.Context, repo string, number int, reviews *ReviewPassService) *ReviewLoopActiveError {
	if loop, err := a.db.ActiveReviewLoop(ctx, repo, number); err == nil && loop != nil {
		name, link := reviewLoopName(loop)
		return &ReviewLoopActiveError{Repo: repo, PR: number, Name: name, Link: link, LoopID: loop.ID}
	}
	if reviews != nil {
		return reviews.ActiveLoopOn(ctx, repo, number)
	}
	return nil
}

// SetReviews gives the adapter this instance's review-pass engine, once it
// runs; nil when review passes are off.
func (a *ReviewPromptAttentionAdapter) SetReviews(reviews *ReviewPassService) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reviews = reviews
}

func (a *ReviewPromptAttentionAdapter) engine() *ReviewPassService {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.reviews
}

func (a *ReviewPromptAttentionAdapter) Producer() string { return ReviewPromptAttentionProducer }

// unreviewed is a card's open pull requests that no review loop reviewed,
// in stack order, when it should prompt a review (nil when not), whether
// it left out any open one as approved, and the loop that holds one of them
// (when one does, it prompts nothing).
func (a *ReviewPromptAttentionAdapter) unreviewed(ctx context.Context, task *store.Task, tracked []store.TaskBranch, reviews *ReviewPassService) (open []PRMetaView, approved bool, held *ReviewLoopActiveError) {
	if RefuseAway(ctx, a.db, task.ID) != nil {
		return nil, false, nil // away: its worker reviews it
	}
	if h, err := a.db.OpenHandoffForTask(ctx, task.ID); err == nil && h != nil {
		return nil, false, nil // a main's task: its handoff's loop reviews it
	}
	for _, pr := range TaskPullRequests(task, tracked) {
		if pr.Repo == "" || strings.EqualFold(pr.State, "MERGED") || strings.EqualFold(pr.State, "CLOSED") {
			continue
		}
		if held := a.heldBy(ctx, pr.Repo, pr.PRNumber, reviews); held != nil {
			return nil, false, held // an open loop holds it
		}
		// Legato's latest review at its head approved it, whichever task
		// the loop is kept on: a workflow run's is on the run's own
		// (LEG-613, LEG-573). A push since asks again.
		if ok, err := a.db.ReviewApprovedHead(ctx, pr.Repo, pr.PRNumber, pr.Head); err == nil && ok {
			approved = true
			continue
		}
		open = append(open, pr)
	}
	return open, approved, nil
}

// columns are the Done and review columns.
func (a *ReviewPromptAttentionAdapter) columns(reviews *ReviewPassService) []string {
	if reviews != nil {
		cfg := reviews.config()
		return []string{cfg.DoneColumn, cfg.ReviewColumn}
	}
	return []string{"Done", "Review"}
}

// available says why the item has no action, or "" when it can start a loop.
func (a *ReviewPromptAttentionAdapter) available(reviews *ReviewPassService) string {
	if reviews == nil || !reviews.config().Enabled {
		return "Review passes are off on this Legato: turn on review_passes, with a reviewer App credential, to review them here."
	}
	// review-loop is built in: only a process that runs no workflows
	// lacks it.
	if a.launcher == nil || !a.launcher.Has(workflowengine.ReviewLoopWorkflowName) {
		return "This Legato runs no workflows, so it cannot start a review loop here: start one from the web UI of legato serve."
	}
	return ""
}

func (a *ReviewPromptAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	reviews := a.engine()
	tracked, err := a.db.ListTrackedTaskBranches(ctx)
	if err != nil {
		return nil, err
	}
	unavailable := a.available(reviews)
	var items []AttentionInput
	seen := map[string]bool{}
	for _, column := range a.columns(reviews) {
		if seen[strings.ToLower(column)] {
			continue
		}
		seen[strings.ToLower(column)] = true
		tasks, err := a.db.ListTasksByStatus(ctx, column)
		if err != nil {
			return nil, err
		}
		for i := range tasks {
			task := &tasks[i]
			open, _, _ := a.unreviewed(ctx, task, tracked[task.ID], reviews)
			if len(open) == 0 {
				continue
			}
			var heads, names []string
			for _, pr := range open {
				heads = append(heads, fmt.Sprintf("%s#%d:%s", pr.Repo, pr.PRNumber, pr.Head))
				names = append(names, fmt.Sprintf("#%d", pr.PRNumber))
			}
			sort.Strings(heads)
			detail := fmt.Sprintf("%s: %s.", task.Title, strings.Join(names, ", "))
			actions := []AttentionAction{{ID: "open", Label: "Open task", Kind: "navigate"}}
			if unavailable == "" {
				detail += " Start review passes to start a review loop on them."
				actions = append([]AttentionAction{{ID: "start_review", Label: "Start review passes", Kind: "execute", Intent: "primary"}}, actions...)
			} else {
				detail += " " + unavailable
			}
			items = append(items, AttentionInput{
				SignalKind: AttentionSignalPRsUnreviewed,
				SourceKey:  task.ID,
				// A push to any of them brings a dismissed item back.
				SourceRevision: strings.Join(heads, ","),
				TaskID:         task.ID, WorkspaceID: task.WorkspaceID,
				Title:    fmt.Sprintf("%s has %d open %s not reviewed", task.ID, len(open), plural(len(open), "PR", "PRs")),
				Reason:   "Its work is done, and no review loop reviews its pull requests",
				Detail:   detail,
				Category: "review", Urgency: "normal",
				Navigation: AttentionNavigation{Kind: "task", Target: task.ID},
				Actions:    actions,
			})
		}
	}
	return items, nil
}

// Execute starts the built-in review-loop on the card's stack, from its
// bottom open pull request, with the card's agent as the author when
// Legato's author App opened them (agentAuthoredPRs). When a loop approved
// some of them at their heads, it starts on the bottom one not approved
// alone (scope pr), so no approved pull request is reviewed again; the item
// comes back for the rest once that loop ends. A loop that holds one of them
// already, whichever caller opened it, is not started again: the action
// refuses with an *AttentionElsewhereError naming that loop and where it is,
// which a client opens instead (AC 2).
func (a *ReviewPromptAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	if request.ActionID != "start_review" {
		return fmt.Errorf("unknown action %q", request.ActionID)
	}
	reviews := a.engine()
	if why := a.available(reviews); why != "" {
		return errors.New(why)
	}
	task, err := a.db.GetTask(ctx, request.SourceKey)
	if err != nil {
		return err
	}
	tracked, err := a.db.ListTaskBranches(ctx, task.ID)
	if err != nil {
		return err
	}
	open, approved, held := a.unreviewed(ctx, task, tracked, reviews)
	if held != nil {
		return &AttentionElsewhereError{Err: held, Href: held.Link}
	}
	if len(open) == 0 {
		return errors.New("its pull requests are reviewed already, or none is open")
	}
	scope, reviewed := ReviewScopeStack, open
	if approved {
		scope, reviewed = ReviewScopePR, open[:1]
	}
	logins, _ := a.db.GitHubAppLogins(ctx)
	if reviews != nil {
		logins = reviews.legatoLogins(ctx)
	}
	_, err = a.launcher.Start(ctx, workflowengine.ReviewLoopWorkflowName, map[string]string{
		"repo": open[0].Repo, "pr_number": fmt.Sprint(open[0].PRNumber), "scope": scope,
		"agent_author": fmt.Sprint(agentAuthoredPRs(tracked, reviewed, logins)),
	})
	if err != nil {
		// Refused at open: a loop took one of them since.
		for _, pr := range open {
			if held := a.heldBy(ctx, pr.Repo, pr.PRNumber, reviews); held != nil {
				return &AttentionElsewhereError{Err: held, Href: held.Link}
			}
		}
	}
	return err
}
