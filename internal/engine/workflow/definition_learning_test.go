package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestValidateLearningWorkflowExecutorTypes(t *testing.T) {
	_, err := workflow.Parse("learning.yaml", []byte(`
schema_version: 1
name: learning
title: Curate learning
inputs:
  task: {type: task, required: true}
steps:
  - id: packet
    title: Prepare packet
    type: learning.packet.prepare
    with:
      task: ${{ workflow.inputs.task }}
      limits: {max_items: 20, max_bytes: 32768}
    outputs:
      generation_id: {type: string}
      packet_digest: {type: string}
      packet: {type: json}
  - id: submit
    title: Submit proposals
    type: learning.proposals.submit
    with:
      attempt_id: ${{ steps.packet.outputs.generation_id }}
      operations: {schema_version: 1, packet_digest: "digest", operations: []}
    outputs:
      summary: {type: json}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestValidateLearningExecutorRequiredFields(t *testing.T) {
	_, err := workflow.Parse("learning.yaml", []byte(`
schema_version: 1
name: learning
title: Curate learning
steps:
  - id: packet
    title: Prepare packet
    type: learning.packet.prepare
    with: {}
`))
	if err == nil || (!strings.Contains(err.Error(), "task") && !strings.Contains(err.Error(), "limits")) {
		t.Fatalf("error = %v", err)
	}
}
