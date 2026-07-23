package cli

import (
	"context"
	"encoding/json"
	"io"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/service"
)

func PlanSubmit(svc *service.PlanService, taskID, name, bundleDir string, origin service.PlanOriginInput, w io.Writer) error {
	view, err := svc.SubmitWithOrigin(context.Background(), taskID, name, bundleDir, origin)
	if err != nil {
		return err
	}
	broadcastPlanChanged(view.Plan.ID, view.Plan.TaskID, view.Revision.ID, "submitted")
	return writeJSON(w, view)
}

func PlanShow(svc *service.PlanService, planID string, w io.Writer) error {
	view, err := svc.Plan(context.Background(), planID)
	if err != nil {
		return err
	}
	return writeJSON(w, view)
}

func PlanAnswer(svc *service.PlanService, planID, threadID, text string) error {
	if err := svc.Answer(context.Background(), planID, threadID, text); err != nil {
		return err
	}
	broadcastPlanChanged(planID, "", "", "answer")
	return nil
}

func PlanComplete(svc *service.PlanService, planID string, w io.Writer) error {
	result, err := svc.Complete(context.Background(), planID)
	if err != nil {
		return err
	}
	broadcastPlanChanged(planID, "", "", "completed")
	return writeJSON(w, result)
}

func PlanWithdraw(svc *service.PlanService, planID string) error {
	if err := svc.Withdraw(context.Background(), planID); err != nil {
		return err
	}
	broadcastPlanChanged(planID, "", "", "withdrawn")
	return nil
}

func PlanStatus(svc *service.PlanService, planID string, w io.Writer) error {
	view, err := svc.Plan(context.Background(), planID)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(map[string]any{
		"plan_id": view.Plan.ID, "status": view.Plan.Status,
		"revision":                     view.Plan.LatestRevision,
		"cleanup_after_implementation": view.Plan.CleanupAfterImplementation,
		"source_bundle_path":           view.Plan.SourceBundlePath,
		"completed_at":                 view.Plan.CompletedAt,
	})
}

func broadcastPlanChanged(planID, taskID, revisionID, kind string) {
	ipc.Broadcast(ipc.Message{Type: "plan_changed", PlanID: planID, TaskID: taskID, RevisionID: revisionID, Kind: kind})
}
