package service

import (
	"context"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// planMilestones are the plan changes that move its task's main slot, by
// PlanChangedPayload.Kind.
var planMilestones = map[string]WorkMilestone{
	"submitted":         {Stage: store.WorkStagePlanning, Reason: "plan submitted"},
	"changes_requested": {Stage: store.WorkStagePlanning, Reason: "plan sent back"},
	"rejected":          {Stage: store.WorkStagePlanning, Reason: "plan rejected"},
	"approved":          {Stage: store.WorkStageImplementing, Reason: "plan approved"},
}

// StartWorkStageMilestones applies the milestones published on bus to the
// stages: EventWorkMilestone (pull request edges, review passes), plan
// changes, review tours marked ready and changes requested on a pull
// request. Each is applied once, when it happens. It runs until ctx ends
// or the returned function is called.
func StartWorkStageMilestones(ctx context.Context, bus *events.Bus, stages *WorkStageService, db *store.Store) (stop func()) {
	if bus == nil || stages == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	types := []events.EventType{events.EventWorkMilestone, events.EventPlanChanged, events.EventReviewChanged, events.EventPRChangesRequested}
	merged := make(chan events.Event, 64)
	var subs []<-chan events.Event
	for _, t := range types {
		sub := bus.Subscribe(t)
		subs = append(subs, sub)
		go func(sub <-chan events.Event) {
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-sub:
					if !ok {
						return
					}
					select {
					case merged <- event:
					case <-ctx.Done():
						return
					}
				}
			}
		}(sub)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-merged:
				place, m, ok := milestoneOf(ctx, db, event)
				if !ok {
					continue
				}
				if m.ExecutionID == "" {
					if session, err := db.GetAgentSessionInSlot(ctx, place.TaskID, place.Slot); err == nil {
						m.ExecutionID = session.ExecutionID
					}
				}
				if _, err := stages.Milestone(ctx, place, m); err != nil {
					log.Printf("stage milestone for %s: %v", AgentSlotKey(place.TaskID, place.Slot), err)
				}
			}
		}
	}()
	return func() {
		cancel()
		for _, sub := range subs {
			bus.Unsubscribe(sub)
		}
	}
}

// milestoneOf is the milestone an event stands for, and the place it
// moves; false for an event that moves none.
func milestoneOf(ctx context.Context, db *store.Store, event events.Event) (WorkStagePlace, WorkMilestone, bool) {
	switch p := event.Payload.(type) {
	case events.WorkMilestonePayload:
		if p.TaskID == "" || p.Stage == "" {
			return WorkStagePlace{}, WorkMilestone{}, false
		}
		place := TaskPlace(p.TaskID, p.Slot)
		place.PlaceExecutionID = p.ExecutionID
		return place, WorkMilestone{Stage: p.Stage, Reason: p.Reason, Feedback: p.Back}, true
	case events.PlanChangedPayload:
		m, ok := planMilestones[p.Kind]
		if !ok {
			return WorkStagePlace{}, WorkMilestone{}, false
		}
		taskID := p.TaskID
		if taskID == "" && p.PlanID != "" {
			if plan, err := db.GetPlan(ctx, p.PlanID); err == nil {
				taskID = plan.TaskID
			}
		}
		if taskID == "" {
			return WorkStagePlace{}, WorkMilestone{}, false
		}
		return TaskPlace(taskID, ""), m, true
	case events.ReviewChangedPayload:
		if p.Kind != "ready" {
			return WorkStagePlace{}, WorkMilestone{}, false
		}
		taskID := p.TaskID
		if taskID == "" && p.TourID != "" {
			if tour, err := db.GetReviewTour(ctx, p.TourID); err == nil {
				taskID = tour.TaskID
			}
		}
		if taskID == "" {
			return WorkStagePlace{}, WorkMilestone{}, false
		}
		return TaskPlace(taskID, ""), WorkMilestone{Stage: store.WorkStageReviewing, Reason: "review tour ready"}, true
	case events.PRChangesRequestedPayload:
		if p.TaskID == "" {
			return WorkStagePlace{}, WorkMilestone{}, false
		}
		return TaskPlace(p.TaskID, ""), WorkMilestone{
			Stage: store.WorkStageImplementing, Reason: fmt.Sprintf("changes requested on PR #%d", p.Number), Feedback: true,
		}, true
	}
	return WorkStagePlace{}, WorkMilestone{}, false
}

// publishMilestone publishes a stage milestone on bus (nil for none).
func publishMilestone(bus *events.Bus, p events.WorkMilestonePayload) {
	if bus == nil || p.TaskID == "" {
		return
	}
	bus.Publish(events.Event{Type: events.EventWorkMilestone, Payload: p})
}
