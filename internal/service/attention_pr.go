package service

import (
	"context"
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

func (a *PRAttentionAdapter) Producer() string { return "pull_requests" }

func (a *PRAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	tasks, err := a.tasks.ListPRTrackedTasks(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0)
	for _, task := range tasks {
		meta, err := store.ParsePRMeta(task.PRMeta)
		if err != nil {
			return nil, fmt.Errorf("parsing PR metadata for task %s: %w", task.ID, err)
		}
		if meta == nil || meta.PRNumber == 0 || !strings.EqualFold(meta.State, "OPEN") || meta.IsDraft {
			continue
		}
		changesRequested := strings.EqualFold(meta.ReviewDecision, "CHANGES_REQUESTED")
		reviewRequired := strings.EqualFold(meta.ReviewDecision, "REVIEW_REQUIRED")
		ciFailed := strings.EqualFold(meta.CheckStatus, "fail")
		if !changesRequested && !reviewRequired && !ciFailed {
			continue
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
		reason := strings.Join(blockers, " · ")
		detail := meta.PRURL
		items = append(items, AttentionInput{
			SignalKind:     AttentionSignalPRMergeBlocked,
			SourceKey:      fmt.Sprintf("%s:%d", task.ID, meta.PRNumber),
			SourceRevision: fmt.Sprintf("%d:%s:%s:%s:%t", meta.PRNumber, strings.ToUpper(meta.State), strings.ToUpper(meta.ReviewDecision), strings.ToLower(meta.CheckStatus), meta.IsDraft),
			TaskID:         task.ID,
			WorkspaceID:    task.WorkspaceID,
			Title:          fmt.Sprintf("PR #%d · %s", meta.PRNumber, task.Title),
			Reason:         reason,
			Detail:         detail,
			Category:       "pull_request",
			Urgency:        "high",
			Navigation:     AttentionNavigation{Kind: "task", Target: task.ID},
			Actions:        []AttentionAction{{ID: "open", Label: "Open task", Kind: "navigate"}},
		})
	}
	return items, nil
}
