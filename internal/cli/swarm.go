package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/swarm"
	"github.com/cpave3/legato/internal/service"
)

// PlanValidationResult is the JSON shape returned by SwarmValidatePlan.
type PlanValidationResult struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// SwarmValidatePlan loads a plan from disk and validates it without any
// side-effects (no DB writes, no IPC). Returns a structured result suitable
// for JSON serialization. I/O errors are returned as Go errors; validation
// failures are expressed inside the result itself.
func SwarmValidatePlan(planPath string, validateOpts swarm.ValidateOptions) (*PlanValidationResult, error) {
	plan, err := swarm.LoadPlan(planPath)
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}
	if err := swarm.ValidatePlan(plan, validateOpts); err != nil {
		return &PlanValidationResult{Valid: false, Error: err.Error()}, nil
	}
	return &PlanValidationResult{Valid: true}, nil
}

// SwarmIsConductor reports whether the calling process is the swarm
// conductor (LEGATO_AGENT_ROLE=conductor) or the user's own shell. Used to
// gate conductor-only verbs.
func SwarmIsConductor() bool {
	return os.Getenv("LEGATO_AGENT_ROLE") == "conductor" || os.Getenv("LEGATO_AGENT_ROLE") == ""
}

// SwarmIsWorker reports whether the calling process is a swarm worker.
// Workers are non-conductor agents with LEGATO_AGENT_ROLE set.
func SwarmIsWorker() bool {
	role := os.Getenv("LEGATO_AGENT_ROLE")
	return role != "" && role != "conductor"
}

// SwarmProposePlan submits a plan for HITL approval. Validates the plan, copies
// it to its canonical location under ~/.legato/plans/, broadcasts
// `plan_proposed` IPC to all running TUI instances, and blocks until a verdict
// arrives. On approval, persists the (possibly user-edited) sub-tasks to the
// DB. Prints a JSON result to stdout: {"status":"approved|rejected","plan_path":"...","notes":"..."}.
//
// autoApprove skips the IPC gate (for headless usage). timeout caps the wait;
// zero means wait forever.
func SwarmProposePlan(sw service.SwarmService, planPath string, autoApprove bool, timeout time.Duration, validateOpts swarm.ValidateOptions) error {
	plan, err := swarm.LoadPlan(planPath)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if err := swarm.ValidatePlan(plan, validateOpts); err != nil {
		return fmt.Errorf("validate plan: %w", err)
	}
	canonical, err := plan.WriteTo(plan.Swarm.WorkingDir, plan.Swarm.ParentTaskID)
	if err != nil {
		return fmt.Errorf("persist plan: %w", err)
	}

	if autoApprove {
		if err := sw.ApplyApprovedPlan(context.Background(), plan); err != nil {
			return fmt.Errorf("apply plan: %w", err)
		}
		emitVerdict("approved", canonical, "")
		return nil
	}

	// Block on user verdict via IPC request/reply.
	reply, err := ipc.BroadcastRequest(
		context.Background(),
		ipc.Message{
			Type:     "plan_proposed",
			TaskID:   plan.Swarm.ParentTaskID,
			PlanPath: canonical,
		},
		"plan_verdict",
		timeout,
	)
	if err != nil {
		return fmt.Errorf("plan approval: %w", err)
	}

	switch reply.Status {
	case "approved":
		// Re-load the plan in case the user edited it in-place.
		path := reply.PlanPath
		if path == "" {
			path = canonical
		}
		edited, lerr := swarm.LoadPlan(path)
		if lerr != nil {
			return fmt.Errorf("load edited plan: %w", lerr)
		}
		if err := swarm.ValidatePlan(edited, validateOpts); err != nil {
			return fmt.Errorf("validate edited plan: %w", err)
		}
		if err := sw.ApplyApprovedPlan(context.Background(), edited); err != nil {
			return fmt.Errorf("apply plan: %w", err)
		}
		emitVerdict("approved", path, "")
	case "rejected":
		emitVerdict("rejected", canonical, reply.Notes)
	default:
		return fmt.Errorf("unexpected verdict status %q", reply.Status)
	}
	return nil
}

// SwarmDispatch spawns the worker for a queued sub-task.
func SwarmDispatch(sw service.SwarmService, subtaskID string) error {
	if err := sw.Dispatch(context.Background(), subtaskID); err != nil {
		return err
	}
	ipc.Broadcast(ipc.Message{
		Type:    "swarm_changed",
		TaskID:  parentOf(sw, subtaskID),
		Status:  subtaskID,
		Content: "dispatched",
	})
	return nil
}

// SwarmMessage delivers text into a worker's tmux pane. When urgent is true
// the adapter's interrupt keys (e.g. Escape) are sent first to abort any
// active turn.
func SwarmMessage(sw service.SwarmService, subtaskID, text string, urgent bool) error {
	return sw.Message(context.Background(), subtaskID, text, urgent)
}

// SwarmBroadcast delivers text to every live worker in the swarm. When urgent
// is true interrupt keys are sent per-target before the message.
func SwarmBroadcast(sw service.SwarmService, parentID, text string, urgent bool) error {
	count, err := sw.Broadcast(context.Background(), parentID, text, urgent)
	if err != nil {
		return err
	}
	fmt.Printf("delivered to %d worker(s)\n", count)
	return nil
}

// SwarmClose ratifies completion or terminates a worker.
func SwarmClose(sw service.SwarmService, subtaskID string) error {
	parentID := parentOf(sw, subtaskID)
	if err := sw.Close(context.Background(), subtaskID); err != nil {
		return err
	}
	ipc.Broadcast(ipc.Message{
		Type:    "swarm_changed",
		TaskID:  parentID,
		Status:  subtaskID,
		Content: "closed",
	})
	return nil
}

// SwarmFinish ends the swarm, kills all live workers + the conductor, and
// appends the summary to the parent task description.
func SwarmFinish(sw service.SwarmService, parentID, summary string) error {
	if err := sw.Finish(context.Background(), parentID, summary); err != nil {
		return err
	}
	ipc.Broadcast(ipc.Message{
		Type:    "swarm_changed",
		TaskID:  parentID,
		Content: "finished",
	})
	return nil
}

// SwarmStatus prints the JSON snapshot for a swarm parent.
func SwarmStatus(sw service.SwarmService, parentID string) error {
	raw, err := sw.Snapshot(context.Background(), parentID)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

// SwarmInbox prints all unacked swarm events for a parent in human-readable
// form, then marks them acked. Conductors call this when they receive a
// `[legato] new swarm event #N` send-keys notification.
func SwarmInbox(sw service.SwarmService, parentID string) error {
	entries, err := sw.FetchInbox(context.Background(), parentID)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("(no pending events)")
		return nil
	}
	for _, e := range entries {
		header := fmt.Sprintf("#%d  %s", e.ID, e.Kind)
		if e.SubtaskID != "" {
			header += "  " + e.SubtaskID
		}
		if e.WorkerTitle != "" {
			header += "  " + fmt.Sprintf("(%s)", e.WorkerTitle)
		}
		fmt.Println(header)
		fmt.Println(e.Payload)
		fmt.Println()
	}
	return nil
}

// SwarmProgress is a worker-side report.
func SwarmProgress(sw service.SwarmService, subtaskID, text string) error {
	return sw.Progress(context.Background(), subtaskID, text)
}

// SwarmQuestion is a worker-side question to the conductor.
func SwarmQuestion(sw service.SwarmService, subtaskID, text string) error {
	return sw.Question(context.Background(), subtaskID, text)
}

// SwarmBuilt is the worker's completion signal.
func SwarmBuilt(sw service.SwarmService, subtaskID string) error {
	return sw.Built(context.Background(), subtaskID)
}

// emitVerdict prints the JSON result to stdout.
func emitVerdict(status, planPath, notes string) {
	out := map[string]string{
		"status":    status,
		"plan_path": planPath,
	}
	if notes != "" {
		out["notes"] = notes
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
}

// parentOf returns the parent_task_id for the given sub-task, or "" if not found.
func parentOf(sw service.SwarmService, subtaskID string) string {
	st, err := sw.GetSubtask(context.Background(), subtaskID)
	if err != nil {
		return ""
	}
	return st.ParentTaskID
}

// SwarmNextStep advances the swarm to the next step after validating the
// current step is terminal.
func SwarmNextStep(sw service.SwarmService, parentID string) error {
	return sw.NextStep(context.Background(), parentID)
}

// SwarmStartFromCLI is the CLI entry point for `legato swarm start <parent-id> <working-dir>`.
// Used by callers that want to start a swarm without going through the TUI overlay
// (e.g. tests or scripts).
func SwarmStartFromCLI(sw service.SwarmService, parentID, workingDir string) error {
	return sw.StartSwarm(context.Background(), parentID, workingDir)
}

