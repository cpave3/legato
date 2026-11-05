package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type prAttentionSource interface {
	ListPRTrackedTasks(context.Context) ([]store.Task, error)
}

type PRAttentionAdapter struct {
	tasks prAttentionSource
}

func NewPRAttentionAdapter(tasks prAttentionSource) *PRAttentionAdapter {
	return &PRAttentionAdapter{tasks: tasks}
}

const prAttentionProducer = "pull_requests"

func (a *PRAttentionAdapter) Producer() string { return prAttentionProducer }

// argusCaseSource lists Argus's open cases. A PR one of them targets is
// Argus's to report as failing CI.
type argusCaseSource interface {
	ListOpenIntegrationAttention(ctx context.Context, integration string) ([]store.IntegrationAttention, error)
}

func (a *PRAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	tasks, err := a.tasks.ListPRTrackedTasks(ctx)
	if err != nil {
		return nil, err
	}
	argusPRs, err := a.argusPRs(ctx)
	if err != nil {
		return nil, err
	}
	branches, err := a.trackedBranches(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0)
	for i := range tasks {
		task := &tasks[i]
		if _, err := store.ParsePRMeta(task.PRMeta); err != nil {
			return nil, fmt.Errorf("parsing PR metadata for task %s: %w", task.ID, err)
		}
		// One item per PR of the task, in stack order.
		for _, pr := range TaskPullRequests(task, branches[task.ID]) {
			if item, ok := prMergeBlocker(task, pr, argusPRs); ok {
				items = append(items, item)
			}
		}
	}
	return items, nil
}

// trackedBranches lists every task's tracked branches, when the source
// keeps them.
func (a *PRAttentionAdapter) trackedBranches(ctx context.Context) (map[string][]store.TaskBranch, error) {
	source, ok := a.tasks.(interface {
		ListTrackedTaskBranches(context.Context) (map[string][]store.TaskBranch, error)
	})
	if !ok {
		return nil, nil
	}
	return source.ListTrackedTaskBranches(ctx)
}

// prMergeBlocker is the Merge blocker item of one of a task's PRs: an open,
// ready PR whose review or checks block it.
func prMergeBlocker(task *store.Task, meta PRMetaView, argusPRs map[string]bool) (AttentionInput, bool) {
	if meta.PRNumber == 0 || !strings.EqualFold(meta.State, "OPEN") || meta.IsDraft {
		return AttentionInput{}, false
	}
	changesRequested := strings.EqualFold(meta.ReviewDecision, "CHANGES_REQUESTED")
	reviewRequired := strings.EqualFold(meta.ReviewDecision, "REVIEW_REQUIRED")
	ciFailed := strings.EqualFold(meta.CheckStatus, "fail")
	argusHasIt := ciFailed && argusPRs[argusPRKey(meta.Repo, meta.PRNumber)]
	if argusHasIt {
		ciFailed = false
	}
	if !changesRequested && !reviewRequired && !ciFailed {
		return AttentionInput{}, false
	}

	blockers := make([]string, 0, 2)
	if changesRequested {
		blockers = append(blockers, "Changes requested")
	} else if reviewRequired {
		blockers = append(blockers, "Review required")
	}
	if ciFailed {
		blockers = append(blockers, "CI failing")
	}
	revision := fmt.Sprintf("%d:%s:%s:%s:%t", meta.PRNumber, strings.ToUpper(meta.State), strings.ToUpper(meta.ReviewDecision), strings.ToLower(meta.CheckStatus), meta.IsDraft)
	if argusHasIt {
		revision += ":argus"
	}
	return AttentionInput{
		SignalKind:     AttentionSignalPRMergeBlocked,
		SourceKey:      fmt.Sprintf("%s:%d", task.ID, meta.PRNumber),
		SourceRevision: revision,
		TaskID:         task.ID,
		WorkspaceID:    task.WorkspaceID,
		Title:          fmt.Sprintf("PR #%d · %s", meta.PRNumber, task.Title),
		Reason:         strings.Join(blockers, " · "),
		Detail:         meta.PRURL,
		Category:       "pull_request",
		Urgency:        "high",
		Navigation:     AttentionNavigation{Kind: "task", Target: task.ID},
		Actions:        []AttentionAction{{ID: "open", Label: "Open task", Kind: "navigate"}},
	}, true
}

// argusPRs is the set of PRs Argus has an open case on, when the source
// knows Argus's cases.
func (a *PRAttentionAdapter) argusPRs(ctx context.Context) (map[string]bool, error) {
	cases, ok := a.tasks.(argusCaseSource)
	if !ok {
		return nil, nil
	}
	rows, err := cases.ListOpenIntegrationAttention(ctx, caseAttentionProducer)
	if err != nil {
		return nil, err
	}
	prs := map[string]bool{}
	for _, row := range rows {
		var target CaseTarget
		if json.Unmarshal([]byte(row.Target), &target) == nil && target.Repo != "" && target.PR > 0 {
			prs[argusPRKey(target.Repo, target.PR)] = true
		}
	}
	return prs, nil
}

func argusPRKey(repo string, pr int) string { return fmt.Sprintf("%s#%d", strings.ToLower(repo), pr) }
