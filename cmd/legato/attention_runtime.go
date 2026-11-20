package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireAttention builds an AttentionService with all runtime producers
// registered and reconciled, then attaches it to the server.
func wireAttention(ctx context.Context, db *store.Store, bus *events.Bus, srv *server.Server, planSvc *service.PlanService, reviewSvc *service.ReviewService, agentSvc service.AgentService, workflowSvc *service.WorkflowService, approvals *approvalRuntime, updates *service.UpdateService, handoffs *mainHandoffs, reviewPrompt *service.ReviewPromptAttentionAdapter, triggerCoordinator ...*service.WorkflowTriggerCoordinator) *service.AttentionService {
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
	if agentSvc != nil {
		registerInterruptedAttention(attention, agentSvc)
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

	if updates != nil {
		adapter := service.NewUpdateAttentionAdapter(updates)
		mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventUpdateChanged))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}

	registerReviewPrompt(attention, reviewPrompt)

	registerApprovalAttention(attention, approvals)
	mustRegister(attention.RegisterProducer(service.NewRestartAttentionAdapter(db)))
	mustRegister(attention.RegisterProducerWithEvents(engramAttention(db), events.EventAgentStarted, events.EventAgentDied, events.EventAgentCompleted))
	mustRegister(registerOperatorItemsAttention(attention, db, agentSvc))
	if handoffs != nil {
		handoffs.registerAttention(attention, approvals.config(), approvals.serverURL())
	}

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

// newReviewPrompt is the Attention producer that prompts a review of local
// work; wireIntegrations gives it the review-pass engine once it runs.
func newReviewPrompt(cfg *config.Config, db *store.Store, workflows *service.WorkflowService) *service.ReviewPromptAttentionAdapter {
	var launcher *service.TaskWorkflowLauncher
	if workflows != nil {
		launcher = service.NewTaskWorkflowLauncher(db, workflows, workflowDiscoveryProvider(cfg))
	}
	return service.NewReviewPromptAttentionAdapter(db, launcher)
}

// registerReviewPrompt registers the review prompt and its action with an
// Attention service: the web server's and the TUI's each, so Start review
// passes works from both. Local work in Done or the review column with
// open pull requests no loop reviews raises it.
func registerReviewPrompt(attention *service.AttentionService, reviewPrompt *service.ReviewPromptAttentionAdapter) {
	if reviewPrompt == nil {
		return
	}
	mustRegister(attention.RegisterProducerWithEvents(reviewPrompt, events.EventCardMoved, events.EventCardUpdated, events.EventCardsRefreshed,
		events.EventPRStatusUpdated, events.EventWorkflowChanged, events.EventHandoffChanged))
	mustRegister(attention.RegisterHandler(reviewPrompt.Producer(), reviewPrompt))
}

// registerOperatorItemsAttention registers the producer of what agents post
// with for_operator, reconciled when an item changes and when an agent
// starts or ends, and, with the agent service, its Done and answers.
func registerOperatorItemsAttention(attention *service.AttentionService, db *store.Store, agents service.AgentService) error {
	adapter := service.NewOperatorItemsAttentionAdapter(db)
	messenger, acts := agents.(service.OperatorItemMessenger)
	if acts {
		adapter.SetMessenger(messenger)
	}
	if err := attention.RegisterProducerWithEvents(adapter,
		events.EventOperatorItemsChanged, events.EventAgentStarted, events.EventAgentDied, events.EventAgentCompleted); err != nil {
		return err
	}
	if !acts {
		return nil
	}
	return attention.RegisterHandler(adapter.Producer(), adapter)
}
