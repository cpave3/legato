package service

import (
	"context"
	"fmt"
)

type PlanAttentionSource interface {
	Queue(context.Context) ([]PlanQueueItem, error)
	Approve(context.Context, string, ...bool) error
}

type PlanAttentionAdapter struct {
	plans PlanAttentionSource
}

func NewPlanAttentionAdapter(plans PlanAttentionSource) *PlanAttentionAdapter {
	return &PlanAttentionAdapter{plans: plans}
}

func (a *PlanAttentionAdapter) Producer() string { return "plans" }

func (a *PlanAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	queue, err := a.plans.Queue(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(queue))
	for _, plan := range queue {
		if plan.Status != "proposed" {
			continue
		}
		reason := "Plan is awaiting your decision"
		actions := []AttentionAction{{ID: "open", Label: "Open plan", Kind: "navigate"}}
		if plan.Status == "proposed" && plan.UnansweredRequired == 0 {
			actions = append(actions, AttentionAction{
				ID: "approve", Label: "Approve", Kind: "execute", Intent: "primary", RequiresConfirm: true,
			})
		}
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalPlanDecision,
			SourceKey:  plan.PlanID, SourceRevision: fmt.Sprintf("%s:%d:%d", plan.Status, plan.Revision, plan.UnansweredRequired),
			TaskID: plan.TaskID, Title: plan.Title, Reason: reason, Detail: plan.Summary,
			Category: "approval", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "plan", Target: plan.PlanID}, Actions: actions,
		})
	}
	return items, nil
}

func (a *PlanAttentionAdapter) Execute(ctx context.Context, request AttentionActionRequest) error {
	if request.ActionID != "approve" {
		return fmt.Errorf("unsupported plan attention action %q", request.ActionID)
	}
	return a.plans.Approve(ctx, request.SourceKey)
}
