package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

const (
	ReflectionSchemaVersion       = 1
	MaxReflectionObservations     = 32
	MaxReflectionDocumentBytes    = 64 * 1024
	MaxReflectionObservationBytes = 8 * 1024
	MaxReflectionPromptBytes      = 16 * 1024
	MaxReflectionMetadataDepth    = 4
)

var (
	ErrInvalidReflection = errors.New("invalid reflection document")
	ErrReflectionStale   = errors.New("reflection request is stale or already completed")
)

// ReflectionDocument is the versioned callback schema. Observation content and
// metadata are untrusted agent-provided data and must remain attributable.
type ReflectionDocument struct {
	SchemaVersion int                     `json:"schema_version"`
	Observations  []ReflectionObservation `json:"observations"`
}

type ReflectionObservation struct {
	Kind      string         `json:"kind"`
	Content   string         `json:"content"`
	Rationale string         `json:"rationale,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type ReflectionSubmission struct {
	Request  *store.LearningReflectionRequest `json:"request"`
	Evidence []store.LearningEvidence         `json:"evidence"`
	Replayed bool                             `json:"replayed"`
}

func (s *LearningService) CreateReflectionRequest(ctx context.Context, input CreateReflectionRequestInput) (*store.LearningReflectionRequest, error) {
	input.WorkflowRunID = strings.TrimSpace(input.WorkflowRunID)
	input.StepID = strings.TrimSpace(input.StepID)
	input.AttemptID = strings.TrimSpace(input.AttemptID)
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.WorkflowRunID == "" || input.StepID == "" || input.AttemptID == "" || input.TaskID == "" {
		return nil, fmt.Errorf("workflow_run_id, step_id, attempt_id, and task_id are required")
	}
	if input.Prompt == "" {
		input.Prompt = durableReflectionPrompt
	}
	if len([]byte(input.Prompt)) > MaxReflectionPromptBytes {
		return nil, fmt.Errorf("prompt exceeds %d bytes", MaxReflectionPromptBytes)
	}
	request := store.LearningReflectionRequest{
		ID: newLearningID("lrq-"), WorkflowRunID: input.WorkflowRunID, StepID: input.StepID,
		AttemptID: input.AttemptID, TaskID: input.TaskID, AgentSessionID: input.AgentSessionID,
		SchemaVersion: ReflectionSchemaVersion, Prompt: input.Prompt,
		Status: store.ReflectionRequestPending, ObservationsJSON: "[]",
	}
	created, err := s.store.CreateLearningReflectionRequest(ctx, request)
	if errors.Is(err, store.ErrConflict) {
		return nil, fmt.Errorf("reflection attempt already has a different request: %w", ErrReflectionStale)
	}
	return created, err
}

// MarkReflectionUnavailable durably closes a request whose prompt could not be
// delivered. Storage and lifecycle conflicts are returned to the caller.
func (s *LearningService) MarkReflectionUnavailable(ctx context.Context, request *store.LearningReflectionRequest, reason string) error {
	reason = strings.TrimSpace(reason)
	if request == nil || request.ID == "" {
		return fmt.Errorf("reflection request ID is required")
	}
	if reason == "" {
		return fmt.Errorf("unavailable reason is required")
	}
	return s.store.MarkReflectionUnavailable(ctx, request, reason)
}

// SubmitReflection validates and atomically persists a typed callback. Exact
// replays succeed without creating duplicate evidence; changed replays fail.
func (s *LearningService) SubmitReflection(ctx context.Context, requestID string, document ReflectionDocument) (*ReflectionSubmission, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("%w: request ID is required", ErrInvalidReflection)
	}
	if err := validateReflectionDocument(&document); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidReflection, err)
	}
	if len(encoded) > MaxReflectionDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds %d bytes", ErrInvalidReflection, MaxReflectionDocumentBytes)
	}
	request, err := s.store.GetLearningReflectionRequest(ctx, requestID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: request %q was not found", ErrReflectionStale, requestID)
		}
		return nil, err
	}
	if request.SchemaVersion != document.SchemaVersion {
		return nil, fmt.Errorf("%w: request expects schema_version %d", ErrInvalidReflection, request.SchemaVersion)
	}

	evidence := make([]store.LearningEvidence, len(document.Observations))
	for i, observation := range document.Observations {
		contentDigest := sha256.Sum256([]byte(observation.Content))
		metadata, err := json.Marshal(map[string]any{
			"schema_version":   document.SchemaVersion,
			"observation_kind": observation.Kind,
			"rationale":        observation.Rationale,
			"metadata":         observation.Metadata,
			"untrusted":        true,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: observation %d metadata: %v", ErrInvalidReflection, i, err)
		}
		author := "agent"
		if request.AgentSessionID != nil {
			author = fmt.Sprintf("agent_session:%d", *request.AgentSessionID)
		}
		evidence[i] = store.LearningEvidence{
			ID: newLearningID("lev-"), SourceKind: "agent_reflection",
			SourceRef: fmt.Sprintf("reflection:%s:%d", request.ID, i), Content: observation.Content,
			ContentDigest: "sha256:" + hex.EncodeToString(contentDigest[:]), Author: author,
			TaskID: request.TaskID, AgentSessionID: request.AgentSessionID,
			WorkflowRunID: request.WorkflowRunID, MetadataJSON: string(metadata),
		}
	}
	replayed, err := s.store.SubmitLearningReflection(ctx, request.ID, string(encoded), evidence)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("%w: request %q no longer accepts this callback", ErrReflectionStale, request.ID)
		}
		return nil, err
	}
	storedEvidence, err := s.store.ListLearningEvidenceByReflection(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	request, err = s.store.GetLearningReflectionRequest(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	return &ReflectionSubmission{Request: request, Evidence: storedEvidence, Replayed: replayed}, nil
}

// SubmitReflectionJSON strictly decodes external callback JSON before using the
// typed submission path.
func (s *LearningService) SubmitReflectionJSON(ctx context.Context, requestID string, data []byte) (*ReflectionSubmission, error) {
	if len(data) > MaxReflectionDocumentBytes {
		return nil, fmt.Errorf("%w: document exceeds %d bytes", ErrInvalidReflection, MaxReflectionDocumentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var document ReflectionDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidReflection, err)
	}
	if document.Observations == nil {
		return nil, fmt.Errorf("%w: observations must be an array", ErrInvalidReflection)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidReflection, err)
	}
	return s.SubmitReflection(ctx, requestID, document)
}

func validateReflectionDocument(document *ReflectionDocument) error {
	if document.SchemaVersion != ReflectionSchemaVersion {
		return fmt.Errorf("%w: schema_version must be %d", ErrInvalidReflection, ReflectionSchemaVersion)
	}
	if len(document.Observations) > MaxReflectionObservations {
		return fmt.Errorf("%w: observations count must not exceed %d", ErrInvalidReflection, MaxReflectionObservations)
	}
	for i := range document.Observations {
		observation := &document.Observations[i]
		observation.Kind = strings.TrimSpace(observation.Kind)
		observation.Content = strings.TrimSpace(observation.Content)
		observation.Rationale = strings.TrimSpace(observation.Rationale)
		if observation.Kind == "" || observation.Content == "" {
			return fmt.Errorf("%w: observation %d requires kind and content", ErrInvalidReflection, i)
		}
		if len([]byte(observation.Kind)) > 128 || len([]byte(observation.Content)) > MaxReflectionObservationBytes || len([]byte(observation.Rationale)) > MaxReflectionObservationBytes {
			return fmt.Errorf("%w: observation %d exceeds field bounds", ErrInvalidReflection, i)
		}
		if jsonDepth(observation.Metadata, 1) > MaxReflectionMetadataDepth {
			return fmt.Errorf("%w: observation %d metadata exceeds depth %d", ErrInvalidReflection, i, MaxReflectionMetadataDepth)
		}
		metadata, err := json.Marshal(observation.Metadata)
		if err != nil || len(metadata) > MaxReflectionObservationBytes {
			return fmt.Errorf("%w: observation %d metadata is invalid or too large", ErrInvalidReflection, i)
		}
	}
	return nil
}

func jsonDepth(value any, depth int) int {
	max := depth
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if childDepth := jsonDepth(child, depth+1); childDepth > max {
				max = childDepth
			}
		}
	case []any:
		for _, child := range typed {
			if childDepth := jsonDepth(child, depth+1); childDepth > max {
				max = childDepth
			}
		}
	}
	return max
}
