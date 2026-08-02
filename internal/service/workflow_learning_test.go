package service

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeWorkflowLearningService struct {
	prepareInput PrepareLearningPacketInput
	document     CuratorOperationsDocument
	submitRef    string
}

func (f *fakeWorkflowLearningService) PrepareLearningPacket(_ context.Context, input PrepareLearningPacketInput) (*PreparedLearningPacket, error) {
	f.prepareInput = input
	return &PreparedLearningPacket{
		Generation: &store.LearningCurationGeneration{ID: "lcur-1", PacketDigest: "sha256:abc"},
		Packet:     LearningPacket{SchemaVersion: 1, PacketDigest: "sha256:abc", Evidence: []store.LearningEvidence{}, Learnings: []store.Learning{}, Exclusions: []LearningPacketExclusion{}},
	}, nil
}

func (f *fakeWorkflowLearningService) SubmitCuratorOperations(_ context.Context, ref string, document CuratorOperationsDocument) (*CuratorSubmission, error) {
	f.submitRef, f.document = ref, document
	return &CuratorSubmission{Generation: &store.LearningCurationGeneration{ID: "lcur-1", PacketDigest: document.PacketDigest, Status: store.CurationGenerationCompleted}, Replayed: true}, nil
}

func TestLearningPacketPrepareExecutorResolvesContextAndReturnsPacket(t *testing.T) {
	learning := &fakeWorkflowLearningService{}
	executor := NewLearningPacketPrepareExecutor(learning)
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{
		RunID: "wr-1", AttemptID: "wa-1",
		With: map[string]any{
			"task":    map[string]any{"id": "TASK-1"},
			"project": map[string]any{"key": "repo:one", "label": "One"},
			"limits":  map[string]any{"max_items": int64(12), "max_bytes": int64(4096)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %q", result.Status)
	}
	if learning.prepareInput.WorkflowRunID != "wr-1" || learning.prepareInput.AttemptID != "wa-1" || learning.prepareInput.TaskID != "TASK-1" || learning.prepareInput.ProjectKey != "repo:one" || learning.prepareInput.ProjectLabel != "One" || learning.prepareInput.MaxItems != 12 || learning.prepareInput.MaxBytes != 4096 {
		t.Fatalf("input = %#v", learning.prepareInput)
	}
	if result.Outputs["generation_id"] != "lcur-1" || result.Outputs["packet_digest"] != "sha256:abc" {
		t.Fatalf("outputs = %#v", result.Outputs)
	}
	if _, ok := result.Outputs["packet"].(map[string]any); !ok {
		t.Fatalf("packet is not JSON-compatible: %#v", result.Outputs["packet"])
	}
}

func TestLearningProposalsSubmitExecutorAcceptsJSONString(t *testing.T) {
	learning := &fakeWorkflowLearningService{}
	executor := NewLearningProposalsSubmitExecutor(learning)
	result, err := executor.Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"attempt_id": "wa-prepare",
		"operations": `{"schema_version":1,"packet_digest":"sha256:abc","operations":[]}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if learning.submitRef != "wa-prepare" || learning.document.PacketDigest != "sha256:abc" {
		t.Fatalf("submission = %q %#v", learning.submitRef, learning.document)
	}
	if result.Status != "succeeded" || result.Outputs["generation_id"] != "lcur-1" || result.Outputs["replayed"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestLearningProposalsSubmitExecutorAcceptsJSONCompatibleOperations(t *testing.T) {
	learning := &fakeWorkflowLearningService{}
	_, err := NewLearningProposalsSubmitExecutor(learning).Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"generation": map[string]any{"attempt_id": "wa-prepare"},
		"operations": map[string]any{"schema_version": float64(1), "packet_digest": "sha256:abc", "operations": []any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if learning.submitRef != "wa-prepare" {
		t.Fatalf("reference = %q", learning.submitRef)
	}
}
