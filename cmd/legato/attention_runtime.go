package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireAttention builds an AttentionService with all runtime producers
// registered and reconciled, then attaches it to the server.
func wireAttention(ctx context.Context, db *store.Store, bus *events.Bus, srv *server.Server, planSvc *service.PlanService, reviewSvc *service.ReviewService, agentSvc service.AgentService, workflowSvc *service.WorkflowService, approvals *approvalRuntime, triggerCoordinator ...*service.WorkflowTriggerCoordinator) *service.AttentionService {
	attention := service.NewAttentionService(db, bus)
	if planSvc != nil {
		adapter := service.NewPlanAttentionAdapter(planSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventPlanChanged))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}
	if reviewSvc != nil {
		adapter := service.NewReviewAttentionAdapter(reviewSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventReviewChanged))
	}
	prAdapter := service.NewPRAttentionAdapter(db)
	mustRegister(attention.RegisterProducerWithEvents(prAdapter, events.EventPRStatusUpdated))
	if agentSvc != nil {
		adapter := service.NewAgentAttentionAdapter(agentSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventAgentActivityChanged, events.EventAgentStarted, events.EventAgentDied, events.EventAgentCompleted))
	}
	if workflowSvc != nil {
		adapter := service.NewWorkflowAttentionAdapter(workflowSvc)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventWorkflowChanged, events.EventAgentDied, events.EventAgentCompleted))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}
	if len(triggerCoordinator) > 0 && triggerCoordinator[0] != nil {
		adapter := service.NewWorkflowTriggerAttentionAdapter(triggerCoordinator[0], db)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventWorkflowTriggerChanged))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}

	registerApprovalAttention(attention, approvals)

	attention.StartEventReconciliation(ctx)

	srv.SetAttentionService(attention)
	srv.StartAttentionEvents()

	if err := attention.ReconcileAll(ctx); err != nil {
		log.Printf("attention: startup reconciliation: %v", err)
	} else if counts, err := attention.CountsBySignal(ctx); err != nil {
		log.Printf("attention: startup signal counts: %v", err)
	} else {
		log.Printf("attention: startup signal counts: %v", counts)
	}
	return attention
}

func mustRegister(err error) {
	if err != nil {
		log.Printf("attention: %v", err)
	}
}
