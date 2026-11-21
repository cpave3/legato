package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// AgentStage is `legato agent stage <stage> [--note]`: the agent of the
// task's running session (executionID's, else the main slot's) reports
// where its work stands. It returns the stage recorded.
func AgentStage(ctx context.Context, db *store.Store, taskID, executionID, stage, note string) (string, error) {
	stage, err := service.ParseWorkStage(stage)
	if err != nil {
		return "", err
	}
	session, err := db.GetRunningAgentSession(ctx, taskID, executionID)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("Legato has no running agent session for task %s", taskID)
	}
	if err != nil {
		return "", err
	}
	// The lookup falls back to the main slot's session for an execution it
	// does not find: a report from another execution is not this one's.
	if executionID != "" && session.ExecutionID != executionID {
		return "", fmt.Errorf("execution %s does not own the running agent session of task %s", executionID, taskID)
	}
	if service.IsReeveTask(ctx, db, session.TaskID) {
		return "", service.ErrNoWorkStage
	}
	if _, err := service.ReportWorkStage(ctx, db, *session, stage, note); err != nil {
		return "", err
	}
	return stage, nil
}

// brokerAgentStage is `legato agent stage` from Legato's sandbox, for the
// caller's own session.
func brokerAgentStage(db *store.Store) BrokerOp {
	return func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
		var args struct {
			Stage string `json:"stage"`
			Note  string `json:"note"`
		}
		if err := json.Unmarshal(raw, &args); err != nil || args.Stage == "" {
			return nil, refuse("bad_request", "a stage is required")
		}
		stage, err := AgentStage(ctx, db, caller.TaskID, caller.ExecutionID, args.Stage, args.Note)
		if err != nil {
			return nil, refuse("rejected", err.Error())
		}
		return map[string]string{"stage": stage}, nil
	}
}
