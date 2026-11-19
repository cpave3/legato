package service

import (
	"context"
	"log"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
)

func (c *WorkflowTriggerCoordinator) StartEventLoop(ctx context.Context, bus *events.Bus) func() {
	ctx, cancel := context.WithCancel(ctx)
	if err := c.Reconcile(ctx); err != nil {
		log.Printf("workflow triggers: startup reconciliation: %v", err)
	}
	if err := c.CatchUpSchedules(ctx); err != nil {
		log.Printf("workflow triggers: missed schedules: %v", err)
	}
	go c.scheduleLoop(ctx)
	if bus == nil {
		return cancel
	}
	ch := bus.Subscribe(events.EventCardMoved)
	reviews := bus.Subscribe(events.EventPRChangesRequested)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-reviews:
				if payload, ok := event.Payload.(events.PRChangesRequestedPayload); ok {
					if err := c.ChangesRequested(ctx, payload); err != nil {
						log.Printf("workflow triggers: changes requested: %v", err)
					}
				}
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

// scheduleLoop ticks the schedules each minute, just after the minute.
func (c *WorkflowTriggerCoordinator) scheduleLoop(ctx context.Context) {
	for {
		if err := c.TickSchedules(ctx); err != nil {
			log.Printf("workflow triggers: schedules: %v", err)
		}
		now := time.Now()
		wait := now.Truncate(time.Minute).Add(time.Minute + time.Second).Sub(now)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
