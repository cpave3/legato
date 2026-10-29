package cli

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The host side of the broker: what agent-side processes of a session may
// ask the host Legato to do, each for its own execution only. Each operation
// calls the same code the CLI command does when it opens the store itself.

// BrokerCaller is who a request comes from: the session its token names.
type BrokerCaller struct {
	TaskID      string
	ExecutionID string
}

// BrokerOp is one operation.
type BrokerOp func(ctx context.Context, caller BrokerCaller, args json.RawMessage) (any, error)

// BrokerDeps is what the operations need of the host.
type BrokerDeps struct {
	Push, OS service.Notifier
	// Ops adds operations served by other parts of the host (tools, bridges).
	Ops map[string]BrokerOp
}

func refuse(code, message string) error { return &broker.Error{Code: code, Message: message} }

// NewBrokerHandler answers requests with the operations an agent session
// may use, after resolving its token to a running execution. A request
// that names another task or execution is refused.
func NewBrokerHandler(db *store.Store, deps BrokerDeps) broker.Handler {
	ops := map[string]BrokerOp{
		"agent.state":           brokerAgentState(db, deps),
		"agent.session_created": brokerSessionCreated(db),
	}
	for name, op := range deps.Ops {
		ops[name] = op
	}
	return func(ctx context.Context, req broker.Request) (any, error) {
		execution, err := db.ExecutionByBrokerToken(ctx, service.HashBrokerToken(req.Token))
		if errors.Is(err, store.ErrNotFound) {
			return nil, refuse("unauthorized", "this token names no running agent session")
		}
		if err != nil {
			return nil, err
		}
		caller := BrokerCaller{TaskID: execution.ExecutionTaskID, ExecutionID: execution.ID}
		if caller.TaskID == "" {
			caller.TaskID = execution.TaskID
		}
		var named struct {
			TaskID      string `json:"task_id"`
			ExecutionID string `json:"execution_id"`
		}
		if len(req.Args) > 0 {
			if err := json.Unmarshal(req.Args, &named); err != nil {
				return nil, refuse("bad_request", "the arguments do not read")
			}
		}
		if named.TaskID != "" && named.TaskID != caller.TaskID || named.ExecutionID != "" && named.ExecutionID != caller.ExecutionID {
			return nil, refuse("forbidden", "a session can act only for itself")
		}
		op, ok := ops[req.Op]
		if !ok {
			return nil, refuse("unknown_op", "no such operation: "+req.Op)
		}
		return op(ctx, caller, req.Args)
	}
}

// brokerAgentState is `legato agent state`. The hooks of `agent stopped`
// and `agent permission-resolved` read their payload on their side and
// send the activity it comes to.
func brokerAgentState(db *store.Store, deps BrokerDeps) BrokerOp {
	return func(_ context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
		var args struct {
			Activity   string `json:"activity"`
			WorkingDir string `json:"working_dir"`
			SessionID  string `json:"session_id"`
			Event      string `json:"event"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, refuse("bad_request", "the arguments do not read")
			}
		}
		if err := AgentState(db, AgentStateReport{TaskID: caller.TaskID, ExecutionID: caller.ExecutionID, Activity: args.Activity,
			WorkingDir: args.WorkingDir, SessionID: args.SessionID, Event: args.Event}, deps.Push, deps.OS); err != nil {
			return nil, refuse("rejected", err.Error())
		}
		return map[string]bool{"ok": true}, nil
	}
}

// brokerSessionCreated is `legato agent session-created`, once the hook
// has checked the payload names the session's own task.
func brokerSessionCreated(db *store.Store) BrokerOp {
	return func(_ context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
		var args struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(raw, &args); err != nil || args.SessionID == "" {
			return nil, refuse("bad_request", "session_id is required")
		}
		if err := AgentSessionCreated(db, caller.TaskID, args.SessionID); err != nil {
			return nil, refuse("rejected", err.Error())
		}
		return map[string]bool{"ok": true}, nil
	}
}
