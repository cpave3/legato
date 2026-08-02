package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// workflowLearningService is deliberately limited to the two mutations needed
// by the learning workflow executors.
type workflowLearningService interface {
	PrepareLearningPacket(context.Context, PrepareLearningPacketInput) (*PreparedLearningPacket, error)
	SubmitCuratorOperations(context.Context, string, CuratorOperationsDocument) (*CuratorSubmission, error)
}

type LearningPacketPrepareExecutor struct{ learning workflowLearningService }

func NewLearningPacketPrepareExecutor(learning workflowLearningService) *LearningPacketPrepareExecutor {
	return &LearningPacketPrepareExecutor{learning: learning}
}
func (e *LearningPacketPrepareExecutor) Type() string { return "learning.packet.prepare" }
func (e *LearningPacketPrepareExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func (e *LearningPacketPrepareExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if e.learning == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("learning service is required")
	}
	taskID, err := workflowObjectString(step.With["task"], "id")
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("task: %w", err)
	}
	projectKey, projectLabel := "", ""
	if project, ok := step.With["project"]; ok && project != nil {
		if projectKey, err = workflowObjectString(project, "key"); err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("project: %w", err)
		}
		projectLabel, err = workflowOptionalObjectString(project, "label")
		if err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("project: %w", err)
		}
	}
	limits, ok := step.With["limits"].(map[string]any)
	if !ok {
		return WorkflowExecutionResult{}, fmt.Errorf("limits must be an object")
	}
	maxItems, err := workflowPositiveInt(limits["max_items"])
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("limits.max_items: %w", err)
	}
	maxBytes, err := workflowPositiveInt(limits["max_bytes"])
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("limits.max_bytes: %w", err)
	}
	prepared, err := e.learning.PrepareLearningPacket(ctx, PrepareLearningPacketInput{
		WorkflowRunID: step.RunID, AttemptID: step.AttemptID, TaskID: taskID,
		ProjectKey: projectKey, ProjectLabel: projectLabel, MaxItems: maxItems, MaxBytes: maxBytes,
	})
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if prepared == nil || prepared.Generation == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("learning packet preparation returned no generation")
	}
	packet, err := jsonCompatible(prepared.Packet)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("encode learning packet: %w", err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"generation_id": prepared.Generation.ID, "packet_digest": prepared.Generation.PacketDigest, "packet": packet,
	}}, nil
}

type LearningProposalsSubmitExecutor struct{ learning workflowLearningService }

func NewLearningProposalsSubmitExecutor(learning workflowLearningService) *LearningProposalsSubmitExecutor {
	return &LearningProposalsSubmitExecutor{learning: learning}
}
func (e *LearningProposalsSubmitExecutor) Type() string { return "learning.proposals.submit" }
func (e *LearningProposalsSubmitExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func (e *LearningProposalsSubmitExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if e.learning == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("learning service is required")
	}
	reference, err := learningAttemptReference(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	document, err := curatorOperationsDocument(step.With["operations"])
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("operations: %w", err)
	}
	submission, err := e.learning.SubmitCuratorOperations(ctx, reference, document)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if submission == nil || submission.Generation == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("curator submission returned no generation")
	}
	summary := map[string]any{"generation_id": submission.Generation.ID, "packet_digest": submission.Generation.PacketDigest, "status": submission.Generation.Status, "replayed": submission.Replayed, "operation_count": len(document.Operations)}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"generation_id": submission.Generation.ID, "packet_digest": submission.Generation.PacketDigest,
		"status": submission.Generation.Status, "replayed": submission.Replayed, "summary": summary,
	}}, nil
}

func learningAttemptReference(values map[string]any) (string, error) {
	if value, ok := values["attempt_id"]; ok {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text), nil
		}
		return "", fmt.Errorf("attempt_id must be a non-empty string")
	}
	if value, ok := values["generation"]; ok {
		switch value := value.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value), nil
			}
		case map[string]any:
			if text, ok := value["attempt_id"].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text), nil
			}
		}
		return "", fmt.Errorf("generation must be an attempt reference")
	}
	return "", fmt.Errorf("attempt_id or generation is required")
}

func curatorOperationsDocument(value any) (CuratorOperationsDocument, error) {
	var data []byte
	var err error
	if text, ok := value.(string); ok {
		data = []byte(text)
	} else {
		data, err = json.Marshal(value)
	}
	if err != nil {
		return CuratorOperationsDocument{}, err
	}
	if len(data) > MaxCuratorDocumentBytes {
		return CuratorOperationsDocument{}, fmt.Errorf("document too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document CuratorOperationsDocument
	if err := decoder.Decode(&document); err != nil {
		return document, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return document, fmt.Errorf("trailing JSON")
	}
	return document, nil
}

func workflowObjectString(value any, field string) (string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("must be an object")
	}
	text, ok := object[field].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return strings.TrimSpace(text), nil
}
func workflowOptionalObjectString(value any, field string) (string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("must be an object")
	}
	item, ok := object[field]
	if !ok || item == nil {
		return "", nil
	}
	text, ok := item.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return strings.TrimSpace(text), nil
}
func workflowPositiveInt(value any) (int, error) {
	n, err := workflowInt(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return n, nil
}
func jsonCompatible(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
