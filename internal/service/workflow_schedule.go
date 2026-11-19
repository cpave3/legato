package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// scheduleTickWindow is how late a tick may be and still fire a due
// time; an older due time was missed while no Legato ran.
const scheduleTickWindow = 5 * time.Minute

// SetHostZone sets the zone a schedule without a timezone follows: the
// host's (time.Local) by default.
func (c *WorkflowTriggerCoordinator) SetHostZone(loc *time.Location) { c.hostZone = loc }

func (c *WorkflowTriggerCoordinator) zone() *time.Location {
	if c.hostZone != nil {
		return c.hostZone
	}
	return time.Local
}

// TickSchedules fires each schedule's latest due time, when it fell within
// the last few minutes. Every process that runs triggers ticks; a due time
// is one request (on workflow, trigger and due time) whichever ticks first.
func (c *WorkflowTriggerCoordinator) TickSchedules(ctx context.Context) error {
	now := c.clock()
	var errs []error
	for _, definition := range c.definitions().Definitions {
		schedule := definition.On.Schedule
		if schedule == nil {
			continue
		}
		due, ok := schedule.Prev(now.Add(time.Nanosecond), c.zone())
		if !ok || now.Sub(due) > scheduleTickWindow {
			continue
		}
		if _, err := c.fireDue(ctx, definition, due, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// fireDue fires one due time of a definition's schedule. Its inputs may
// read ${{ schedule.time }} and ${{ schedule.previous_time }} (the due time
// before it), each RFC 3339 in the schedule's zone.
func (c *WorkflowTriggerCoordinator) fireDue(ctx context.Context, definition *workflowengine.Definition, due, now time.Time) (*store.WorkflowTriggerRequest, error) {
	return c.fireDueSince(ctx, definition, due, now, time.Time{})
}

// fireDueSince is fireDue with the previous time given (a catch-up run
// covers every missed due time since the last one that ran).
func (c *WorkflowTriggerCoordinator) fireDueSince(ctx context.Context, definition *workflowengine.Definition, due, now, previous time.Time) (*store.WorkflowTriggerRequest, error) {
	schedule := definition.On.Schedule
	loc := schedule.Location(c.zone())
	if previous.IsZero() {
		previous, _ = schedule.Prev(due, c.zone())
	}
	eventContext := map[string]any{"schedule": map[string]any{"time": due.In(loc).Format(time.RFC3339), "previous_time": formatTime(previous, loc)}}
	key := due.UTC().Format(time.RFC3339)
	return c.Fire(ctx, TriggerEvent{Definition: definition, Kind: workflowengine.TriggerSchedule, Key: key, Item: key, Context: eventContext, At: now})
}

func formatTime(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(time.RFC3339)
}

// ScheduleState is a schedule as the Workflows page shows it: in words, its
// next due times and what its last one did.
type ScheduleState struct {
	Description string        `json:"description"`
	Timezone    string        `json:"timezone"`
	Next        []string      `json:"next"`
	Last        *ScheduleFire `json:"last,omitempty"`
	// Missed is the newest due times missed while no Legato ran, with why.
	Missed []ScheduleFire `json:"missed"`
	// CatchUp is once or skip.
	CatchUp string `json:"catch_up"`
}

// ScheduleFire is what a due time did.
type ScheduleFire struct {
	DueTime string `json:"due_time"`
	Status  string `json:"status"`
	RunID   string `json:"run_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (c *WorkflowTriggerCoordinator) scheduleState(ctx context.Context, definition *workflowengine.Definition) (*ScheduleState, error) {
	schedule := definition.On.Schedule
	loc := schedule.Location(c.zone())
	state := &ScheduleState{Description: schedule.Describe(), Timezone: loc.String(), Next: []string{}, Missed: []ScheduleFire{}, CatchUp: schedule.CatchUpPolicy()}
	missed, err := c.store.ListTriggerEvents(ctx, definition.Name, workflowengine.TriggerSchedule, "missed", 10)
	if err != nil {
		return nil, err
	}
	for _, m := range missed {
		state.Missed = append(state.Missed, ScheduleFire{DueTime: m.EventKey, Status: m.Status, Error: m.Error})
	}
	after := c.clock()
	for len(state.Next) < 3 {
		next, ok := schedule.Next(after, c.zone())
		if !ok {
			break
		}
		state.Next = append(state.Next, next.In(loc).Format(time.RFC3339))
		after = next
	}
	last, err := c.store.LatestTriggerEvent(ctx, definition.Name, workflowengine.TriggerSchedule)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if last != nil && last.Status != "missed" {
		state.Last = &ScheduleFire{DueTime: last.EventKey, Status: last.Status, RunID: last.RunID, Error: last.Error}
	}
	return state, nil
}

// CatchUpSchedules records the due times that passed while no Legato ran:
// those after the newest one recorded (a schedule never recorded has none)
// and before the tick window, each as a missed request, once. With
// catch_up: once (the default) one late run follows for all of them, its
// schedule.previous_time the due time before the first missed, so it
// covers them; skip makes none. Run at start, before the first tick.
func (c *WorkflowTriggerCoordinator) CatchUpSchedules(ctx context.Context) error {
	now := c.clock()
	var errs []error
	for _, definition := range c.definitions().Definitions {
		schedule := definition.On.Schedule
		if schedule == nil {
			continue
		}
		if on, err := c.triggerEnabled(ctx, definition, workflowengine.TriggerSchedule); err != nil || !on {
			errs = append(errs, err)
			continue
		}
		lastItem, err := c.store.LatestTriggerItem(ctx, definition.Name, workflowengine.TriggerSchedule)
		if err != nil || lastItem == "" {
			errs = append(errs, err)
			continue
		}
		last, err := time.Parse(time.RFC3339, lastItem)
		if err != nil {
			continue
		}
		missed := schedule.DueBetween(last, now.Add(-scheduleTickWindow), c.zone(), 1000)
		recorded := 0
		loc := schedule.Location(c.zone())
		for _, due := range missed {
			if err := c.recordMissed(ctx, definition, due, now, loc); errors.Is(err, store.ErrTriggerEventSeen) {
				continue
			} else if err != nil {
				errs = append(errs, err)
				continue
			}
			recorded++
		}
		if recorded == 0 || schedule.CatchUpPolicy() != workflowengine.CatchUpOnce {
			continue
		}
		latest := missed[len(missed)-1]
		eventContext := map[string]any{"schedule": map[string]any{"time": latest.In(loc).Format(time.RFC3339), "previous_time": last.In(loc).Format(time.RFC3339), "late": true}}
		if _, err := c.Fire(ctx, TriggerEvent{Definition: definition, Kind: workflowengine.TriggerSchedule, Key: "late:" + latest.UTC().Format(time.RFC3339), Item: latest.UTC().Format(time.RFC3339), Context: eventContext, At: now}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// recordMissed records one due time no Legato ran.
func (c *WorkflowTriggerCoordinator) recordMissed(ctx context.Context, definition *workflowengine.Definition, due, now time.Time, loc *time.Location) error {
	definitionJSON, _ := json.Marshal(definition)
	key := due.UTC().Format(time.RFC3339)
	request, err := c.store.InsertWorkflowTriggerEvent(ctx, store.WorkflowTriggerRequest{
		ID: workflowID("wtr-"), WorkflowName: definition.Name, DefinitionSource: definition.Source, DefinitionJSON: string(definitionJSON),
		Status: "missed", TriggerKind: workflowengine.TriggerSchedule, Item: key, EventKey: key, CreatedAt: now.UTC().Format(time.DateTime),
	})
	if err != nil {
		return err
	}
	reason := fmt.Sprintf("Legato was not running at %s (catch_up: %s)", due.In(loc).Format("2006-01-02 15:04 MST"), definition.On.Schedule.CatchUpPolicy())
	return c.update(ctx, request.ID, "missed", "", `{}`, `[]`, reason)
}
