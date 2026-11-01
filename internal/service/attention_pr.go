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
		argusHasIt := ciFailed && argusPRs[argusPRKey(meta.Repo, meta.PRNumber)]
		if argusHasIt {
			ciFailed = false
		}
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
		revision := fmt.Sprintf("%d:%s:%s:%s:%t", meta.PRNumber, strings.ToUpper(meta.State), strings.ToUpper(meta.ReviewDecision), strings.ToLower(meta.CheckStatus), meta.IsDraft)
		if argusHasIt {
			revision += ":argus"
		}
		items = append(items, AttentionInput{
			SignalKind:     AttentionSignalPRMergeBlocked,
			SourceKey:      fmt.Sprintf("%s:%d", task.ID, meta.PRNumber),
			SourceRevision: revision,
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
