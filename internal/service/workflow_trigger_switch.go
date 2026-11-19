package service

import (
	"context"
	"errors"
	"fmt"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// ErrNoSuchTrigger is a switch for a workflow or trigger kind that is not
// declared.
var ErrNoSuchTrigger = errors.New("no such workflow trigger")

// WorkflowTriggerState is one trigger as the Workflows page shows it: on
// here or paused here, what its file says, and whether this Legato switched
// it.
type WorkflowTriggerState struct {
	workflowengine.TriggerInfo
	Enabled    bool `json:"enabled"`
	Overridden bool `json:"overridden"`
	// Schedule is a schedule's next due times and last fire.
	Schedule *ScheduleState `json:"schedule,omitempty"`
}

// triggerEnabled says whether a definition's trigger is on here: this
// Legato's switch, else the definition's enabled field.
func (c *WorkflowTriggerCoordinator) triggerEnabled(ctx context.Context, definition *workflowengine.Definition, kind string) (bool, error) {
	enabled, ok, err := c.store.WorkflowTriggerSwitchFor(ctx, definition.Name, kind)
	if err != nil || ok {
		return enabled, err
	}
	for _, trigger := range definition.On.List() {
		if trigger.Kind == kind {
			return trigger.EnabledByDefault, nil
		}
	}
	return false, nil
}

// TriggerStates is every declared trigger's state, by workflow name.
func (c *WorkflowTriggerCoordinator) TriggerStates(ctx context.Context) (map[string][]WorkflowTriggerState, error) {
	switches, err := c.store.WorkflowTriggerSwitches(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]WorkflowTriggerState{}
	for _, definition := range c.definitions().Definitions {
		for _, trigger := range definition.On.List() {
			state := WorkflowTriggerState{TriggerInfo: trigger, Enabled: trigger.EnabledByDefault}
			if trigger.Kind == workflowengine.TriggerSchedule {
				if state.Schedule, err = c.scheduleState(ctx, definition); err != nil {
					return nil, err
				}
			}
			for _, sw := range switches {
				if sw.WorkflowName == definition.Name && sw.Trigger == trigger.Kind {
					state.Enabled, state.Overridden = sw.Enabled, true
				}
			}
			out[definition.Name] = append(out[definition.Name], state)
		}
	}
	return out, nil
}

// SetTriggerEnabled switches a declared trigger on or off in this Legato
// only; the definition's file is never touched.
func (c *WorkflowTriggerCoordinator) SetTriggerEnabled(ctx context.Context, workflowName, kind string, enabled bool) error {
	if err := c.declared(workflowName, kind); err != nil {
		return err
	}
	return c.store.SetWorkflowTriggerSwitch(ctx, workflowName, kind, enabled)
}

// ResetTrigger leaves a trigger to its definition again.
func (c *WorkflowTriggerCoordinator) ResetTrigger(ctx context.Context, workflowName, kind string) error {
	if err := c.declared(workflowName, kind); err != nil {
		return err
	}
	return c.store.DeleteWorkflowTriggerSwitch(ctx, workflowName, kind)
}

func (c *WorkflowTriggerCoordinator) declared(workflowName, kind string) error {
	for _, definition := range c.definitions().Definitions {
		if definition.Name != workflowName {
			continue
		}
		for _, trigger := range definition.On.List() {
			if trigger.Kind == kind {
				return nil
			}
		}
		return fmt.Errorf("%s has no %s trigger: %w", workflowName, kind, ErrNoSuchTrigger)
	}
	return fmt.Errorf("no workflow %q: %w", workflowName, ErrNoSuchTrigger)
}
