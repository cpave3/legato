package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/cpave3/legato/internal/engine/store"
)

// An agent step that finished with no output and no artifact kept nothing
// of its agent's work: what the agent wrote in its conversation went with it
// when Legato removed it (LEG-532). The run view, the run list and `legato
// workflow show` say so.

// stepOutputWarning is the warning for a step, or "" when it needs none.
func stepOutputWarning(step store.WorkflowStep, hasArtifacts bool) string {
	if step.Status != "succeeded" || hasArtifacts || !outputsEmpty(step.OutputsJSON) {
		return ""
	}
	switch step.ExecutorType {
	case "agent.run":
		if keepsAgent(step) {
			return "This step finished with no output. Its agent was kept, so what it wrote is only in its conversation."
		}
		return "This step finished with no output. Anything its agent wrote in chat is gone: the agent was removed."
	case "agent.dispatch":
		return "This step finished with no output. What the task's agent wrote is only in its conversation."
	}
	return ""
}

// outputsEmpty reports whether a step's recorded outputs hold nothing: none,
// or only empty values.
func outputsEmpty(outputsJSON string) bool {
	var outputs map[string]any
	if err := json.Unmarshal([]byte(outputsJSON), &outputs); err != nil {
		return outputsJSON == "" || outputsJSON == "null"
	}
	for _, value := range outputs {
		switch v := value.(type) {
		case nil:
		case string:
			if v != "" {
				return false
			}
		case []any:
			if len(v) > 0 {
				return false
			}
		case map[string]any:
			if len(v) > 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// runOutputWarning is the quiet form a run's row shows.
func runOutputWarning(step store.WorkflowStep) string {
	return fmt.Sprintf("“%s” finished with no output", step.Title)
}

// addOutputWarnings fills the output warnings of a run's view.
func addOutputWarnings(view *store.WorkflowRunView) {
	withArtifacts := map[string]bool{}
	for _, artifact := range view.Artifacts {
		withArtifacts[artifact.Path] = true
	}
	for i := range view.Steps {
		step := &view.Steps[i]
		step.OutputWarning = stepOutputWarning(*step, withArtifacts[step.Path])
		if step.OutputWarning != "" && view.Run.OutputWarning == "" {
			view.Run.OutputWarning = runOutputWarning(*step)
		}
	}
}

// addRunOutputWarnings fills the quiet output warning of each run in a list.
func (s *WorkflowService) addRunOutputWarnings(ctx context.Context, runs []store.WorkflowRun) {
	ids := make([]string, 0, len(runs))
	index := make(map[string]int, len(runs))
	for i, run := range runs {
		ids = append(ids, run.ID)
		index[run.ID] = i
	}
	steps, err := s.store.ListSucceededAgentStepsWithoutArtifacts(ctx, ids)
	if err != nil {
		log.Printf("workflows: output warnings: %v", err)
		return
	}
	for _, step := range steps {
		i, ok := index[step.RunID]
		if !ok || runs[i].OutputWarning != "" || stepOutputWarning(step, false) == "" {
			continue
		}
		runs[i].OutputWarning = runOutputWarning(step)
	}
}
