package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// AgentEnded starts the agent_ended step that a waiting run of taskID is
// paused on, once the task has no running agent. It returns the runs it
// moved on.
func (s *WorkflowService) AgentEnded(ctx context.Context, taskID string) ([]string, error) {
	return s.startAgentEndedSteps(ctx, func(run store.WorkflowRun) bool { return run.TaskID == taskID })
}

// StartAgentEndedLoop moves runs on when the agent they wait for ends: first
// any whose agent ended while no Legato was running, then each agent that
// dies from now on. It returns a function that stops the loop.
func (s *WorkflowService) StartAgentEndedLoop(ctx context.Context, bus *events.Bus) func() {
	ctx, cancel := context.WithCancel(ctx)
	announce := func(runs []string, err error) {
		if err != nil {
			log.Printf("workflows: agent ended: %v", err)
		}
		for _, runID := range runs {
			bus.Publish(events.Event{Type: events.EventWorkflowChanged, Payload: events.WorkflowChangedPayload{RunID: runID}})
		}
	}
	announce(s.startAgentEndedSteps(ctx, func(store.WorkflowRun) bool { return true }))
	died := bus.Subscribe(events.EventAgentDied)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-died:
				payload, ok := event.Payload.(events.AgentDiedPayload)
				if !ok || payload.TaskID == "" {
					continue
				}
				announce(s.AgentEnded(ctx, payload.TaskID))
			}
		}
	}()
	return cancel
}

func (s *WorkflowService) startAgentEndedSteps(ctx context.Context, include func(store.WorkflowRun) bool) ([]string, error) {
	runs, err := s.store.ListWorkflowRuns(ctx, store.WorkflowRunFilter{Status: "waiting", Limit: math.MaxInt})
	if err != nil {
		return nil, err
	}
	var started []string
	for _, run := range runs {
		if run.TaskID == "" || !include(run) || !waitsForAgentToEnd(run) || s.taskHasRunningAgent(ctx, run.TaskID) {
			continue
		}
		if _, err := s.TriggerStep(ctx, run.ID, run.CurrentStepID, false); err != nil {
			// Another Legato process saw the same agent end and started it.
			if errors.Is(err, store.ErrWorkflowStateConflict) {
				continue
			}
			return started, err
		}
		started = append(started, run.ID)
	}
	return started, nil
}

func waitsForAgentToEnd(run store.WorkflowRun) bool {
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(run.DefinitionJSON), &definition); err != nil {
		return false
	}
	for _, step := range definition.Steps {
		if step.ID == run.CurrentStepID {
			return step.Trigger == workflowengine.TriggerAgentEnded
		}
	}
	return false
}

func (s *WorkflowService) taskHasRunningAgent(ctx context.Context, taskID string) bool {
	_, err := s.store.GetAgentSessionByTaskID(ctx, taskID)
	return err == nil
}
