package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireAttention builds an AttentionService with the MVP producers (plans,
// reviews, workflows) registered and reconciled, then attaches it to the
// server. It returns the service so callers can also wire it into the TUI.
// planSvc, reviewSvc, and workflowSvc may be nil; nil sources are skipped.
func wireAttention(ctx context.Context, db *store.Store, bus *events.Bus, srv *server.Server, planSvc *service.PlanService, reviewSvc *service.ReviewService, workflowSvc *service.WorkflowService) *service.AttentionService {
	attention := service.NewAttentionService(db, bus)

	if planSvc != nil {
		adapter := service.NewPlanAttentionAdapter(planSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventPlanChanged))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}
	if reviewSvc != nil {
		adapter := service.NewReviewAttentionAdapter(reviewSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventReviewChanged))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}
	if workflowSvc != nil {
		adapter := service.NewWorkflowAttentionAdapter(workflowSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventWorkflowChanged, events.EventAgentDied, events.EventAgentCompleted))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}

	attention.StartEventReconciliation(ctx)

	srv.SetAttentionService(attention)
	srv.StartAttentionEvents()

	if err := attention.ReconcileAll(ctx); err != nil {
		log.Printf("attention: startup reconciliation: %v", err)
	}
	return attention
}

func mustRegister(err error) {
	if err != nil {
		log.Printf("attention: %v", err)
	}
}
