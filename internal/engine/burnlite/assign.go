package burnlite

import "context"

// The stages update_task may set. done and dropped come only from closing a
// Task (CloseCompleted, CloseNotPlanned).
const (
	StageIdea      = "idea"
	StageExploring = "exploring"
	StageReady     = "ready"
	StageBuilding  = "building"
	StageDone      = "done"
	StageDropped   = "dropped"
)

// ClaimTask claims a Task (key or id) under a holder. Claiming again under
// the same holder changes nothing; a Task another holder has claimed is an
// *Error with the code already_claimed. It needs the write:tasks scope.
func (c *Client) ClaimTask(ctx context.Context, key, holder string) error {
	var out map[string]any
	return c.call(ctx, "claim_task", map[string]any{"taskId": key, "holder": holder}, &out)
}

// ReleaseClaim releases the holder's claim on a Task. It needs the
// write:tasks scope.
func (c *Client) ReleaseClaim(ctx context.Context, key, holder string) error {
	var out map[string]any
	return c.call(ctx, "release_claim", map[string]any{"taskId": key, "holder": holder}, &out)
}

// SetStage sets an open Task's stage: StageIdea, StageExploring, StageReady
// or StageBuilding. It needs the write:tasks scope.
func (c *Client) SetStage(ctx context.Context, key, stage string) error {
	var out map[string]any
	return c.call(ctx, "update_task", map[string]any{"taskId": key, "stage": stage}, &out)
}
