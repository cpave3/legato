package main

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

// interruptedSweepTimeout bounds the startup sweep for interrupted agents.
const interruptedSweepTimeout = 30 * time.Second

// interruptedAgentService is the agent service as the wiring of interrupted
// agents needs it.
type interruptedAgentService interface {
	service.InterruptedAgents
	AddInterruptedResumeRoute(service.InterruptedResumeRoute)
	AddInterruptedListener(func())
	SetResourceResolver(service.ResourceResolver)
}

// sweepInterruptedAgents marks the agents that were running when Legato last
// stopped and whose tmux session is gone as interrupted: the machine or
// tmux went down. It runs once, as Legato starts, before anything else
// reconciles agents (which would mark them ended), and it gives the agent
// service the resources an interrupted agent is resumed with.
func sweepInterruptedAgents(agents service.AgentService, resources service.ResourceResolver) {
	svc, ok := agents.(interruptedAgentService)
	if !ok {
		return
	}
	if resources != nil {
		svc.SetResourceResolver(resources)
	}
	ctx, cancel := context.WithTimeout(context.Background(), interruptedSweepTimeout)
	defer cancel()
	if _, err := svc.SweepInterruptedAgents(ctx); err != nil {
		log.Printf("agents: looking for interrupted agents: %v", err)
	}
}

// addInterruptedResumeRoute lets another service resume the interrupted
// agents it starts its own way.
func addInterruptedResumeRoute(agents service.AgentService, route service.InterruptedResumeRoute) {
	if svc, ok := agents.(interruptedAgentService); ok {
		svc.AddInterruptedResumeRoute(route)
	}
}

// registerInterruptedAttention shows the interrupted agents in Attention,
// reconciled each time they change.
func registerInterruptedAttention(attention *service.AttentionService, agents service.AgentService) {
	svc, ok := agents.(interruptedAgentService)
	if !ok {
		return
	}
	adapter := service.NewInterruptedAttentionAdapter(svc)
	mustRegister(attention.RegisterProducerWithEvents(adapter, events.EventAgentStarted, events.EventAgentDied))
	mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	svc.AddInterruptedListener(func() {
		if err := attention.Reconcile(context.Background(), adapter.Producer()); err != nil {
			log.Printf("attention: %v", err)
		}
	})
}
