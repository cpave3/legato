package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type WorkflowTriggerCoordinator struct {
	store       *store.Store
	workflows   *WorkflowService
	definitions func() workflowengine.DiscoveryResult
	bus         *events.Bus
}

func NewWorkflowTriggerCoordinator(s *store.Store, workflows *WorkflowService, definitions func() workflowengine.DiscoveryResult) *WorkflowTriggerCoordinator {
	return &WorkflowTriggerCoordinator{store: s, workflows: workflows, definitions: definitions}
}
func (c *WorkflowTriggerCoordinator) SetEventBus(bus *events.Bus) { c.bus = bus }
func (c *WorkflowTriggerCoordinator) changed(requestID string) {
	if c.bus != nil {
		c.bus.Publish(events.Event{Type: events.EventWorkflowTriggerChanged, Payload: requestID})
	}
}
func (c *WorkflowTriggerCoordinator) update(ctx context.Context, id, status, runID, inputs, missing, errorText string) error {
	if err := c.store.UpdateWorkflowTriggerRequest(ctx, id, status, runID, inputs, missing, errorText); err != nil {
		return err
	}
	c.changed(id)
	return nil
}

func (c *WorkflowTriggerCoordinator) LatestTransitionForTask(ctx context.Context, taskID string) (*store.TaskTransition, error) {
	return c.store.LatestTaskTransition(ctx, taskID)
}

func (c *WorkflowTriggerCoordinator) CaptureLatestForTask(ctx context.Context, taskID string) error {
	transition, err := c.store.LatestTaskTransition(ctx, taskID)
	if err != nil {
		return err
	}
	return c.CaptureTransition(ctx, transition.ID)
}

func (c *WorkflowTriggerCoordinator) CaptureTransition(ctx context.Context, transitionID string) error {
	transition, err := c.store.GetTaskTransition(ctx, transitionID)
	if err != nil {
		return err
	}
	for _, definition := range c.definitions().Definitions {
		trigger := definition.On.TaskEnteredColumn
		if trigger == nil || trigger.Column != transition.ToColumn {
			continue
		}
		definitionJSON, _ := json.Marshal(definition)
		digest := sha256.Sum256(definitionJSON)
		request, err := c.store.UpsertWorkflowTriggerRequest(ctx, store.WorkflowTriggerRequest{ID: workflowID("wtr-"), TransitionID: transition.ID, WorkflowName: definition.Name, DefinitionSource: definition.Source, DefinitionDigest: hex.EncodeToString(digest[:]), DefinitionJSON: string(definitionJSON), InputsJSON: `{}`, MissingInputsJSON: `[]`, Status: "pending"})
		if err != nil {
			return err
		}
		if request.Status == "pending" {
			if err := c.process(ctx, *transition, definition, request, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *WorkflowTriggerCoordinator) Reconcile(ctx context.Context) error {
	requests, err := c.store.ListWorkflowTriggerRequests(ctx, "pending")
	if err != nil {
		return err
	}
	for _, request := range requests {
		transition, err := c.store.GetTaskTransition(ctx, request.TransitionID)
		if err != nil {
			return err
		}
		var definition workflowengine.Definition
		if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
			return err
		}
		if err := c.process(ctx, *transition, &definition, &request, nil); err != nil {
			return err
		}
	}
	return nil
}
func (c *WorkflowTriggerCoordinator) process(ctx context.Context, transition store.TaskTransition, definition *workflowengine.Definition, request *store.WorkflowTriggerRequest, supplied map[string]string) error {
	var task map[string]any
	if err := json.Unmarshal([]byte(transition.TaskJSON), &task); err != nil {
		return err
	}
	matched, err := definition.On.TaskEnteredColumn.MatchesEvent(task, map[string]any{"from_column": transition.FromColumn, "to_column": transition.ToColumn})
	if err != nil {
		return err
	}
	if !matched {
		return c.update(ctx, request.ID, "skipped", "", `{}`, `[]`, "condition did not match")
	}
	inputs, missing := resolveTriggerInputs(definition, task, supplied, c.workflows.repositories)
	encodedInputs, _ := json.Marshal(inputs)
	encodedMissing, _ := json.Marshal(missing)
	if len(missing) > 0 {
		return c.update(ctx, request.ID, "waiting_input", "", string(encodedInputs), string(encodedMissing), "")
	}
	view, err := c.workflows.StartTriggered(ctx, definition, inputs, request.ID)
	if err != nil {
		if existing, findErr := c.store.FindWorkflowRunByTriggerRequest(ctx, request.ID); findErr == nil {
			view = existing
		} else {
			_ = c.update(ctx, request.ID, "failed", "", string(encodedInputs), `[]`, err.Error())
			return nil
		}
	}
	return c.update(ctx, request.ID, "started", view.Run.ID, string(encodedInputs), `[]`, "")
}

func resolveTriggerInputs(definition *workflowengine.Definition, task map[string]any, supplied map[string]string, presets []workflowengine.RepositoryPreset) (map[string]string, []string) {
	inputs := map[string]string{}
	for key, value := range supplied {
		inputs[key] = value
	}
	for key, mapping := range definition.On.TaskEnteredColumn.Inputs {
		if _, ok := inputs[key]; ok {
			continue
		}
		if text, ok := mapping.(string); ok && strings.HasPrefix(text, "${{") {
			expr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "${{"), "}}"))
			var value any
			switch {
			case expr == "event.task":
				value = task["id"]
			case strings.HasPrefix(expr, "task."):
				value, _ = lookupTriggerPath(task, strings.TrimPrefix(expr, "task."))
			}
			if value != nil {
				if text, ok := value.(string); ok {
					inputs[key] = text
				} else if encoded, err := json.Marshal(value); err == nil {
					inputs[key] = string(encoded)
				}
			}
		} else if mapping != nil {
			inputs[key] = fmt.Sprint(mapping)
		}
	}
	for key, spec := range definition.Inputs {
		if _, ok := inputs[key]; ok {
			continue
		}
		if spec.Type == "repository" && len(presets) == 1 {
			inputs[key] = presets[0].Name
			continue
		}
		if spec.Default != nil {
			inputs[key] = fmt.Sprint(spec.Default)
		}
	}
	missing := []string{}
	for key, spec := range definition.Inputs {
		if spec.Required && inputs[key] == "" {
			missing = append(missing, key)
		}
	}
	return inputs, missing
}

func lookupTriggerPath(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func (c *WorkflowTriggerCoordinator) SupplyInputs(ctx context.Context, requestID string, supplied map[string]string, remember bool) (*store.WorkflowTriggerRequest, error) {
	request, err := c.store.GetWorkflowTriggerRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request.Status != "waiting_input" && request.Status != "failed" {
		return nil, fmt.Errorf("trigger request is %s", request.Status)
	}
	transition, err := c.store.GetTaskTransition(ctx, request.TransitionID)
	if err != nil {
		return nil, err
	}
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
		return nil, fmt.Errorf("decode workflow definition: %w", err)
	}
	if remember {
		if repository := supplied["repository"]; repository != "" {
			var remembered any = repository
			if strings.HasPrefix(strings.TrimSpace(repository), "{") {
				_ = json.Unmarshal([]byte(repository), &remembered)
			}
			parts, _ := store.ValidateTaskMetadataPath("repository")
			if err := c.store.SetTaskMetadata(ctx, transition.TaskID, parts, remembered); err != nil {
				return nil, err
			}
		}
	}
	if err := c.update(ctx, request.ID, "pending", "", request.InputsJSON, request.MissingInputsJSON, ""); err != nil {
		return nil, err
	}
	request.Status = "pending"
	if err := c.process(ctx, *transition, &definition, request, supplied); err != nil {
		return nil, err
	}
	return c.store.GetWorkflowTriggerRequest(ctx, requestID)
}

func (c *WorkflowTriggerCoordinator) Request(ctx context.Context, requestID string) (*store.WorkflowTriggerRequest, error) {
	return c.store.GetWorkflowTriggerRequest(ctx, requestID)
}

func (c *WorkflowTriggerCoordinator) RequestsForTransition(ctx context.Context, transitionID string) ([]store.WorkflowTriggerRequest, error) {
	return c.store.ListWorkflowTriggerRequestsForTransition(ctx, transitionID)
}

func (c *WorkflowTriggerCoordinator) RequestsForTask(ctx context.Context, taskID string) ([]store.WorkflowTriggerRequest, error) {
	return c.store.ListWorkflowTriggerRequestsForTask(ctx, taskID)
}

func (c *WorkflowTriggerCoordinator) Skip(ctx context.Context, requestID string) error {
	request, err := c.store.GetWorkflowTriggerRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if request.Status != "waiting_input" && request.Status != "failed" {
		return fmt.Errorf("trigger request is %s", request.Status)
	}
	return c.update(ctx, request.ID, "skipped", "", request.InputsJSON, `[]`, "skipped by user")
}
