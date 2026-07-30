package service

import (
	"context"
	"log"

	"github.com/cpave3/legato/internal/engine/events"
)

func (c *WorkflowTriggerCoordinator) StartEventLoop(ctx context.Context, bus *events.Bus) func() {
	ctx, cancel := context.WithCancel(ctx)
	if err := c.Reconcile(ctx); err != nil {
		log.Printf("workflow triggers: startup reconciliation: %v", err)
	}
	if bus == nil {
		return cancel
	}
	ch := bus.Subscribe(events.EventCardMoved)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-ch:
				transitionID := ""
				if payload, ok := event.Payload.(map[string]string); ok {
					transitionID = payload["transition_id"]
				}
				if transitionID != "" {
					if err := c.CaptureTransition(ctx, transitionID); err != nil {
						log.Printf("workflow triggers: capture: %v", err)
					}
				}
			}
		}
	}()
	return cancel
}
