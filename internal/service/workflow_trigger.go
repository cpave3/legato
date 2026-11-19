package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

type WorkflowTriggerCoordinator struct {
	store       *store.Store
	workflows   *WorkflowService
	definitions func() workflowengine.DiscoveryResult
	bus         *events.Bus
	now         func() time.Time
	hostZone    *time.Location
}

// SetClock replaces the coordinator's clock, for tests.
func (c *WorkflowTriggerCoordinator) SetClock(now func() time.Time) { c.now = now }

func (c *WorkflowTriggerCoordinator) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
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
		// A paused trigger asks for nothing.
		if on, err := c.triggerEnabled(ctx, definition, workflowengine.TriggerTaskEnteredColumn); err != nil {
			return err
		} else if !on {
			continue
		}
		definitionJSON, _ := json.Marshal(definition)
		digest := sha256.Sum256(definitionJSON)
		request, err := c.store.UpsertWorkflowTriggerRequest(ctx, store.WorkflowTriggerRequest{ID: workflowID("wtr-"), TransitionID: transition.ID, WorkflowName: definition.Name, DefinitionSource: definition.Source, DefinitionDigest: hex.EncodeToString(digest[:]), DefinitionJSON: string(definitionJSON), InputsJSON: `{}`, MissingInputsJSON: `[]`, Status: "pending",
			TriggerKind: workflowengine.TriggerTaskEnteredColumn, Item: transition.TaskID, CreatedAt: c.clock().UTC().Format(time.DateTime)})
		if err != nil {
			return err
		}
		if request.Status == "pending" {
			if err := c.process(ctx, *transition, definition, request, nil, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *WorkflowTriggerCoordinator) Reconcile(ctx context.Context) error {
	starting, err := c.store.ListWorkflowTriggerRequests(ctx, "starting")
	if err != nil {
		return err
	}
	for _, request := range starting {
		if run, findErr := c.store.FindWorkflowRunByTriggerRequest(ctx, request.ID); findErr == nil {
			if err := c.update(ctx, request.ID, "started", run.Run.ID, request.InputsJSON, `[]`, ""); err != nil {
				return err
			}
		} else if errors.Is(findErr, store.ErrNotFound) {
			if err := c.update(ctx, request.ID, "failed", "", request.InputsJSON, `[]`, "workflow start was interrupted; retry the trigger"); err != nil {
				return err
			}
		} else {
			return findErr
		}
	}
	requests, err := c.store.ListWorkflowTriggerRequests(ctx, "pending")
	if err != nil {
		return err
	}
	for _, request := range requests {
		if request.TransitionID == "" {
			var definition workflowengine.Definition
			if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
				return err
			}
			if err := c.processEvent(ctx, &definition, &request, nil, false, false); err != nil {
				return err
			}
			continue
		}
		transition, err := c.store.GetTaskTransition(ctx, request.TransitionID)
		if err != nil {
			return err
		}
		var definition workflowengine.Definition
		if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
			return err
		}
		if err := c.process(ctx, *transition, &definition, &request, nil, false); err != nil {
			return err
		}
	}
	return nil
}
func (c *WorkflowTriggerCoordinator) process(ctx context.Context, transition store.TaskTransition, definition *workflowengine.Definition, request *store.WorkflowTriggerRequest, supplied map[string]string, confirmed bool) error {
	return c.processColumn(ctx, transition, definition, request, supplied, confirmed, false)
}

// processColumn takes a column trigger's request as far as it can go: past
// its conditions, inputs, the loop stop (unless force) and confirmation,
// to a run.
func (c *WorkflowTriggerCoordinator) processColumn(ctx context.Context, transition store.TaskTransition, definition *workflowengine.Definition, request *store.WorkflowTriggerRequest, supplied map[string]string, confirmed, force bool) error {
	var task map[string]any
	if err := json.Unmarshal([]byte(transition.TaskJSON), &task); err != nil {
		return err
	}
	// A card handed off from a main is the main's: this Legato's triggers
	// leave it alone unless the workflow opts in.
	if !definition.On.TaskEnteredColumn.HandedOff {
		if h, err := c.store.OpenHandoffForTask(ctx, transition.TaskID); err == nil {
			main := h.MainName
			if main == "" {
				main = h.MainInstanceID
			}
			return c.update(ctx, request.ID, "skipped", "", `{}`, `[]`,
				fmt.Sprintf("the card was handed off from %s, whose workflows run when it returns; set handed_off: true on the trigger to run on such cards", main))
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	conditions := definition.On.TaskEnteredColumn.Conditions
	if conditions != nil && conditions.UsesField("task.has_active_agent") {
		if _, err := c.store.GetAgentSessionByTaskID(ctx, transition.TaskID); err == nil {
			task["has_active_agent"] = true
		} else if errors.Is(err, store.ErrNotFound) {
			task["has_active_agent"] = false
		} else {
			return err
		}
	}
	if conditions != nil && conditions.UsesField("task.has_chimera_session") {
		if _, err := c.store.GetTask(ctx, transition.TaskID); err != nil {
			return err
		}
		chimeraSession, err := c.store.TaskChimeraSessionID(ctx, transition.TaskID)
		if err != nil {
			return err
		}
		task["has_chimera_session"] = chimeraSession != ""
	}
	matched, err := definition.On.TaskEnteredColumn.MatchesEvent(task, map[string]any{"from_column": transition.FromColumn, "to_column": transition.ToColumn})
	if err != nil {
		return err
	}
	if !matched {
		return c.update(ctx, request.ID, "skipped", "", `{}`, `[]`, "condition did not match")
	}
	inputs, missing := resolveTriggerInputs(definition, task, supplied, c.workflows.repositoryPresets(ctx))
	encodedInputs, _ := json.Marshal(inputs)
	encodedMissing, _ := json.Marshal(missing)
	if len(missing) > 0 {
		return c.update(ctx, request.ID, "waiting_input", "", string(encodedInputs), string(encodedMissing), "")
	}
	if !force {
		if stop, err := c.loopStop(ctx, definition, workflowengine.TriggerTaskEnteredColumn, transition.TaskID); err != nil {
			return err
		} else if stop != "" {
			return c.update(ctx, request.ID, "held", "", string(encodedInputs), `[]`, stop)
		}
	}
	if definition.On.TaskEnteredColumn.Confirm && !confirmed {
		return c.update(ctx, request.ID, "waiting_confirmation", "", string(encodedInputs), `[]`, "")
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
	return resolveMappedInputs(definition, definition.On.TaskEnteredColumn.Inputs, func(expr string) (any, bool) {
		switch {
		case expr == "event.task":
			value, ok := task["id"]
			return value, ok
		case strings.HasPrefix(expr, "task."):
			return lookupTriggerPath(task, strings.TrimPrefix(expr, "task."))
		}
		return nil, false
	}, supplied, presets)
}

// resolveMappedInputs gives a workflow's inputs from a trigger's inputs
// mapping (a ${{ expression }} read through lookup, or a literal), then the
// supplied values, a single repository preset and the defaults; missing
// are the required inputs still empty.
func resolveMappedInputs(definition *workflowengine.Definition, mapping map[string]any, lookup func(expr string) (any, bool), supplied map[string]string, presets []workflowengine.RepositoryPreset) (map[string]string, []string) {
	inputs := map[string]string{}
	for key, value := range supplied {
		inputs[key] = value
	}
	for key, mapped := range mapping {
		if _, ok := inputs[key]; ok {
			continue
		}
		if text, ok := mapped.(string); ok && strings.HasPrefix(text, "${{") {
			expr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "${{"), "}}"))
			if value, _ := lookup(expr); value != nil {
				if text, ok := value.(string); ok {
					inputs[key] = text
				} else if encoded, err := json.Marshal(value); err == nil {
					inputs[key] = string(encoded)
				}
			}
		} else if mapped != nil {
			inputs[key] = fmt.Sprint(mapped)
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
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
		return nil, fmt.Errorf("decode workflow definition: %w", err)
	}
	if request.TransitionID == "" {
		if err := c.update(ctx, request.ID, "pending", "", request.InputsJSON, request.MissingInputsJSON, ""); err != nil {
			return nil, err
		}
		request.Status = "pending"
		if err := c.processEvent(ctx, &definition, request, supplied, false, false); err != nil {
			return nil, err
		}
		return c.store.GetWorkflowTriggerRequest(ctx, requestID)
	}
	transition, err := c.store.GetTaskTransition(ctx, request.TransitionID)
	if err != nil {
		return nil, err
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
	if err := c.process(ctx, *transition, &definition, request, supplied, false); err != nil {
		return nil, err
	}
	return c.store.GetWorkflowTriggerRequest(ctx, requestID)
}

func (c *WorkflowTriggerCoordinator) Confirm(ctx context.Context, requestID string) (*store.WorkflowTriggerRequest, error) {
	request, err := c.store.GetWorkflowTriggerRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request.Status != "waiting_confirmation" {
		return nil, fmt.Errorf("trigger request is %s", request.Status)
	}
	if err := c.store.TransitionWorkflowTriggerRequest(ctx, request.ID, "waiting_confirmation", "starting"); err != nil {
		return nil, err
	}
	c.changed(request.ID)
	fail := func(err error) (*store.WorkflowTriggerRequest, error) {
		_ = c.update(ctx, request.ID, "failed", "", request.InputsJSON, `[]`, err.Error())
		return nil, err
	}
	var definition workflowengine.Definition
	if err := json.Unmarshal([]byte(request.DefinitionJSON), &definition); err != nil {
		return fail(fmt.Errorf("decode workflow definition: %w", err))
	}
	inputs := map[string]string{}
	if err := json.Unmarshal([]byte(request.InputsJSON), &inputs); err != nil {
		return fail(fmt.Errorf("decode trigger inputs: %w", err))
	}
	request.Status = "starting"
	if request.TransitionID == "" {
		if err := c.processEvent(ctx, &definition, request, inputs, true, false); err != nil {
			_ = c.update(ctx, request.ID, "failed", "", request.InputsJSON, `[]`, err.Error())
			return nil, err
		}
		return c.store.GetWorkflowTriggerRequest(ctx, requestID)
	}
	transition, err := c.store.GetTaskTransition(ctx, request.TransitionID)
	if err != nil {
		return fail(err)
	}
	if err := c.process(ctx, *transition, &definition, request, inputs, true); err != nil {
		_ = c.update(ctx, request.ID, "failed", "", request.InputsJSON, `[]`, err.Error())
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
	if request.Status != "waiting_input" && request.Status != "waiting_confirmation" && request.Status != "failed" {
		return fmt.Errorf("trigger request is %s", request.Status)
	}
	if err := c.store.TransitionWorkflowTriggerRequest(ctx, request.ID, request.Status, "skipped"); err != nil {
		return err
	}
	c.changed(request.ID)
	return nil
}
