package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/store"
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
// loop reviews, raises one item whose action starts review-pr-passes on the
// stack with the card's agent as the author. It prompts; it never starts a
// loop by itself.
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

// unreviewed is a card's open pull requests, in stack order, when it should
// prompt a review; nil when not.
func (a *ReviewPromptAttentionAdapter) unreviewed(ctx context.Context, task *store.Task, tracked []store.TaskBranch, reviews *ReviewPassService) []PRMetaView {
	if RefuseAway(ctx, a.db, task.ID) != nil {
		return nil // away: its worker reviews it
	}
	if h, err := a.db.OpenHandoffForTask(ctx, task.ID); err == nil && h != nil {
		return nil // a main's task: its handoff's loop reviews it
	}
	var open []PRMetaView
	for _, pr := range TaskPullRequests(task, tracked) {
		if pr.Repo == "" || strings.EqualFold(pr.State, "MERGED") || strings.EqualFold(pr.State, "CLOSED") {
			continue
		}
		if reviews != nil && reviews.ActiveLoopOn(ctx, pr.Repo, pr.PRNumber) != nil {
			return nil
		}
		open = append(open, pr)
	}
	return open
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
	if a.launcher == nil || !a.launcher.Has(ReviewPassesWorkflow) {
		return "Add the review-pr-passes workflow (legato workflow add review-pr-passes) to review them here."
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
			open := a.unreviewed(ctx, task, tracked[task.ID], reviews)
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
				detail += " Start review passes to review them with the card's agent as the author."
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

// Execute starts review-pr-passes on the card's stack, from its bottom open
// pull request, with the card's agent as the author. A refusal (a loop
// running on one of them) is the error, naming that loop.
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
	open := a.unreviewed(ctx, task, tracked, reviews)
	if len(open) == 0 {
		return errors.New("its pull requests are reviewed already, or none is open")
	}
	_, err = a.launcher.Start(ctx, ReviewPassesWorkflow, map[string]string{
		"repo": open[0].Repo, "pr_number": fmt.Sprint(open[0].PRNumber), "agent_author": "true", "scope": ReviewScopeStack,
	})
	return err
}
