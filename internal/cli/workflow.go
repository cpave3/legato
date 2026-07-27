package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cpave3/legato/internal/engine/ipc"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func WorkflowDefinitions(globalDir, projectDir string) workflowengine.DiscoveryResult {
	return workflowengine.Discover(globalDir, projectDir)
}
func WorkflowValidate(path string, data []byte) (*workflowengine.Definition, error) {
	return workflowengine.Parse(path, data)
}
func WorkflowRun(ctx context.Context, svc *service.WorkflowService, definition *workflowengine.Definition, inputs map[string]string, workspaceID *int, w io.Writer) error {
	view, err := svc.Start(ctx, definition, inputs, workspaceID)
	if err != nil {
		return err
	}
	broadcastWorkflowChanged(view.Run.ID, view.Run.CurrentStepID, "started")
	return json.NewEncoder(w).Encode(view)
}
func WorkflowRuns(ctx context.Context, svc *service.WorkflowService, status string, limit int, w io.Writer) error {
	runs, err := svc.ListRuns(ctx, status, limit)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(runs)
}
func WorkflowShow(ctx context.Context, svc *service.WorkflowService, runID string, w io.Writer) error {
	view, err := svc.Show(ctx, runID)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(view)
}
func WorkflowComplete(ctx context.Context, svc *service.WorkflowService, runID, stepID, attemptID string, values, artifacts map[string]string, w io.Writer) error {
	if runID == "" || stepID == "" || attemptID == "" {
		return fmt.Errorf("run, step, and attempt identity are required")
	}
	view, err := svc.Complete(ctx, runID, stepID, attemptID, values, artifacts)
	if err != nil {
		return err
	}
	broadcastWorkflowChanged(runID, view.Run.CurrentStepID, "completed_step")
	return json.NewEncoder(w).Encode(view)
}
func WorkflowFail(ctx context.Context, svc *service.WorkflowService, runID, stepID, attemptID, reason string, w io.Writer) error {
	view, err := svc.Fail(ctx, runID, stepID, attemptID, reason)
	if err != nil {
		return err
	}
	broadcastWorkflowChanged(runID, stepID, "failed_step")
	return json.NewEncoder(w).Encode(view)
}
func WorkflowTrigger(ctx context.Context, svc *service.WorkflowService, runID, stepID string, w io.Writer) error {
	view, err := svc.TriggerStep(ctx, runID, stepID)
	if err != nil {
		return err
	}
	broadcastWorkflowChanged(runID, stepID, "triggered")
	return json.NewEncoder(w).Encode(view)
}
func WorkflowRetry(ctx context.Context, svc *service.WorkflowService, runID, stepID string, w io.Writer) error {
	view, err := svc.Retry(ctx, runID, stepID)
	if err != nil {
		return err
	}
	broadcastWorkflowChanged(runID, stepID, "retried")
	return json.NewEncoder(w).Encode(view)
}
func WorkflowCancel(ctx context.Context, svc *service.WorkflowService, runID string, w io.Writer) error {
	view, err := svc.Cancel(ctx, runID, "cancelled by user")
	if err != nil {
		return err
	}
	broadcastWorkflowChanged(runID, view.Run.CurrentStepID, "cancelled")
	return json.NewEncoder(w).Encode(view)
}
func broadcastWorkflowChanged(runID, stepID, kind string) {
	ipc.Broadcast(ipc.Message{Type: "workflow_changed", WorkflowRunID: runID, WorkflowStepID: stepID, Kind: kind})
}
