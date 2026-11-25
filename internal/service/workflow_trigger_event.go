package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// TriggerEvent is one event of a trigger other than a column move: a due
// time of a schedule, a review on a pull request.
type TriggerEvent struct {
	Definition *workflowengine.Definition
	Kind       string
	// Key is unique per event of the kind for the workflow: one request
	// per key, whichever Legato on the database fires it first.
	Key string
	// Item is what the loop stop counts runs by.
	Item string
	// TaskID is the task the event is about, "" for none.
	TaskID string
	// Context is what ${{ … }} in the trigger's inputs reads: event.*,
	// schedule.*.
	Context map[string]any
	// At is when it fired; the coordinator's clock when zero.
	At time.Time
	// Skip, when set, records the request as skipped with this reason
	// and starts nothing: a chat fallback another trigger answered.
	Skip string
}

type triggerEventRecord struct {
	TaskID  string         `json:"task_id,omitempty"`
	Context map[string]any `json:"context,omitempty"`
}

// Fire makes the request for an event and takes it as far as it goes. It
// returns nil, nil for a paused trigger or an event already seen.
func (c *WorkflowTriggerCoordinator) Fire(ctx context.Context, event TriggerEvent) (*store.WorkflowTriggerRequest, error) {
	definition := event.Definition
	if on, err := c.triggerEnabled(ctx, definition, event.Kind); err != nil || !on {
		return nil, err
	}
	at := event.At
	if at.IsZero() {
		at = c.clock()
	}
	definitionJSON, _ := json.Marshal(definition)
	digest := sha256.Sum256(definitionJSON)
	record, _ := json.Marshal(triggerEventRecord{TaskID: event.TaskID, Context: event.Context})
	request, err := c.store.InsertWorkflowTriggerEvent(ctx, store.WorkflowTriggerRequest{
		ID: workflowID("wtr-"), WorkflowName: definition.Name, DefinitionSource: definition.Source, DefinitionDigest: hex.EncodeToString(digest[:]),
		DefinitionJSON: string(definitionJSON), Status: "pending", TriggerKind: event.Kind, Item: event.Item, EventKey: event.Key,
		EventJSON: string(record), CreatedAt: at.UTC().Format(time.DateTime),
	})
	if errors.Is(err, store.ErrTriggerEventSeen) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if event.Skip != "" {
		if err := c.update(ctx, request.ID, "skipped", "", "{}", "[]", event.Skip); err != nil {
			return nil, err
		}
		return c.store.GetWorkflowTriggerRequest(ctx, request.ID)
	}
	c.changed(request.ID)
	if err := c.processEvent(ctx, definition, request, nil, false, false); err != nil {
		return nil, err
	}
	return c.store.GetWorkflowTriggerRequest(ctx, request.ID)
}

// eventTaskID is the task an event request is about, "" for none.
func eventTaskID(request store.WorkflowTriggerRequest) string {
	var record triggerEventRecord
	_ = json.Unmarshal([]byte(request.EventJSON), &record)
	return record.TaskID
}

// processEvent takes an event request as far as it can go: inputs, the
// loop stop (unless force), confirmation, a run.
func (c *WorkflowTriggerCoordinator) processEvent(ctx context.Context, definition *workflowengine.Definition, request *store.WorkflowTriggerRequest, supplied map[string]string, confirmed, force bool) error {
	common, ok := definition.On.Common(request.TriggerKind)
	if !ok {
		return c.update(ctx, request.ID, "failed", "", request.InputsJSON, `[]`, "the workflow no longer has a "+request.TriggerKind+" trigger")
	}
	var record triggerEventRecord
	_ = json.Unmarshal([]byte(request.EventJSON), &record)
	inputs, missing := resolveMappedInputs(definition, common.Inputs, record.Context, supplied, c.workflows.repositoryPresets(ctx))
	encodedInputs, _ := json.Marshal(inputs)
	encodedMissing, _ := json.Marshal(missing)
	if len(missing) > 0 {
		return c.update(ctx, request.ID, "waiting_input", "", string(encodedInputs), string(encodedMissing), "")
	}
	if !force {
		if stop, err := c.loopStop(ctx, definition, request.TriggerKind, request.Item); err != nil {
			return err
		} else if stop != "" {
			return c.update(ctx, request.ID, "held", "", string(encodedInputs), `[]`, stop)
		}
	}
	if common.Confirm && !confirmed {
		return c.update(ctx, request.ID, "waiting_confirmation", "", string(encodedInputs), `[]`, "")
	}
	view, err := c.workflows.StartTriggered(ctx, definition, inputs, request.ID)
	if err != nil {
		if existing, findErr := c.store.FindWorkflowRunByTriggerRequest(ctx, request.ID); findErr == nil {
			view = existing
		} else {
			return c.update(ctx, request.ID, "failed", "", string(encodedInputs), `[]`, err.Error())
		}
	}
	return c.update(ctx, request.ID, "started", view.Run.ID, string(encodedInputs), `[]`, "")
}
