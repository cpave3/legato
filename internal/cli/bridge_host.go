package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// BridgeHost is what a native bridge needs of the host Legato: the running
// agent session it serves, binding its endpoint to that session, and saving
// the conversation it can be resumed from. The store does it for a bridge
// that can open the store; the broker does it for one in Legato's sandbox.
type BridgeHost interface {
	// Session checks that the task's running agent belongs to executionID
	// (when given) and returns what the bridge needs to know of it.
	Session(ctx context.Context, taskID, executionID string) (BridgeSession, error)
	Bind(ctx context.Context, endpoint string) error
	// Unbind clears the binding when it is still endpoint's.
	Unbind(ctx context.Context, endpoint string) error
	// SaveIdentity records the provider's session ID, where it works and
	// where the provider keeps it, to resume the task's conversation.
	SaveIdentity(ctx context.Context, kind, providerSessionID, cwd, storageDir string) error
}

// BridgeSession is the running agent session a bridge serves.
type BridgeSession struct {
	// Endpoint is the endpoint bound now, "" when none.
	Endpoint string `json:"endpoint"`
	// RecordsIdentity is false for a session whose conversation the task
	// does not resume (a swarm worker, a child agent).
	RecordsIdentity bool `json:"records_identity"`
	// Models are the model IDs the native panel offers, from the host's
	// configuration, for a bridge that cannot read it.
	Models []string `json:"models,omitempty"`
}

// storeBridgeHost is the host as the store: a bridge that can open it.
type storeBridgeHost struct {
	s       *store.Store
	taskID  string
	session *store.AgentSession
}

// StoreBridgeHost is the host as reached through the store.
func StoreBridgeHost(s *store.Store) BridgeHost { return &storeBridgeHost{s: s} }

func (h *storeBridgeHost) Session(ctx context.Context, taskID, executionID string) (BridgeSession, error) {
	session, err := h.s.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return BridgeSession{}, fmt.Errorf("no running agent to bridge for task %s: %w", taskID, err)
	}
	if executionID != "" && session.ExecutionID != "" && session.ExecutionID != executionID {
		return BridgeSession{}, fmt.Errorf("execution %s does not own the running agent for task %s", executionID, taskID)
	}
	h.taskID, h.session = taskID, session
	out := BridgeSession{RecordsIdentity: recordsIdentity(session, taskID)}
	if session.ChimeraEndpoint != nil {
		out.Endpoint = *session.ChimeraEndpoint
	}
	if out.RecordsIdentity {
		if _, err := h.s.GetTask(ctx, taskID); err != nil {
			out.RecordsIdentity = false
		}
	}
	return out, nil
}

func (h *storeBridgeHost) Bind(ctx context.Context, endpoint string) error {
	if err := h.s.UpdateChimeraDiscoveryForSession(ctx, h.session.ID, h.taskID, endpoint); err != nil {
		return fmt.Errorf("binding the bridge to task %s: %w", h.taskID, err)
	}
	ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: h.taskID})
	return nil
}

func (h *storeBridgeHost) Unbind(ctx context.Context, endpoint string) error {
	cleared, err := h.s.ClearChimeraDiscoveryForSession(ctx, h.session.ID, h.taskID, endpoint)
	if err == nil && cleared {
		ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: h.taskID})
	}
	return err
}

func (h *storeBridgeHost) SaveIdentity(ctx context.Context, kind, providerSessionID, cwd, storageDir string) error {
	changed, err := h.s.RecordTaskNativeSession(ctx, h.taskID, h.session.ID, h.session.ExecutionID, kind, providerSessionID, cwd, storageDir)
	if err != nil {
		return err
	}
	if changed {
		ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: h.taskID})
	}
	return nil
}

// recordsIdentity reports whether the task resumes this session's
// conversation: not a swarm worker's, nor a child agent's.
func recordsIdentity(session *store.AgentSession, taskID string) bool {
	return !(session.Role == "worker" || session.ExecutionKind == store.ExecutionSwarmWorker ||
		(session.SubtaskID != nil && *session.SubtaskID != "") ||
		(session.ParentTaskID != nil && *session.ParentTaskID != "" && !(session.Role == "conductor" && *session.ParentTaskID == taskID)) ||
		(session.AttributionTaskID != "" && session.AttributionTaskID != taskID))
}

// brokerBridgeHost is the host as reached through the broker.
type brokerBridgeHost struct{ socket, token string }

// BrokerBridgeHost is the host as reached through the broker at socket,
// for the session token names.
func BrokerBridgeHost(socket, token string) BridgeHost {
	return &brokerBridgeHost{socket: socket, token: token}
}

// bridgeCallTimeout is longer than a hook's: a bridge that cannot bind
// serves nothing.
const bridgeCallTimeout = 10 * time.Second

func (h *brokerBridgeHost) call(ctx context.Context, op string, args, out any) error {
	ctx, cancel := context.WithTimeout(ctx, bridgeCallTimeout)
	defer cancel()
	return broker.Call(ctx, h.socket, h.token, op, args, out)
}

func (h *brokerBridgeHost) Session(ctx context.Context, taskID, executionID string) (BridgeSession, error) {
	var out BridgeSession
	err := h.call(ctx, "endpoint.session", map[string]string{"task_id": taskID, "execution_id": executionID}, &out)
	return out, err
}

func (h *brokerBridgeHost) Bind(ctx context.Context, endpoint string) error {
	return h.call(ctx, "endpoint.bind", map[string]string{"endpoint": endpoint}, nil)
}

func (h *brokerBridgeHost) Unbind(ctx context.Context, endpoint string) error {
	return h.call(ctx, "endpoint.unbind", map[string]string{"endpoint": endpoint}, nil)
}

func (h *brokerBridgeHost) SaveIdentity(ctx context.Context, kind, providerSessionID, cwd, storageDir string) error {
	return h.call(ctx, "resume.save", map[string]string{"kind": kind, "provider_session_id": providerSessionID, "cwd": cwd, "storage_dir": storageDir}, nil)
}

// bridgeOps serve BridgeHost over the broker, through the store, for the
// caller's own task.
func bridgeOps(db *store.Store, deps BrokerDeps) map[string]BrokerOp {
	host := func(ctx context.Context, caller BrokerCaller) (*storeBridgeHost, error) {
		h := &storeBridgeHost{s: db}
		if _, err := h.Session(ctx, caller.TaskID, caller.ExecutionID); err != nil {
			return nil, refuse("rejected", err.Error())
		}
		return h, nil
	}
	endpointArg := func(raw json.RawMessage) (string, error) {
		var args struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.Unmarshal(raw, &args); err != nil || args.Endpoint == "" {
			return "", refuse("bad_request", "endpoint is required")
		}
		return args.Endpoint, nil
	}
	return map[string]BrokerOp{
		"endpoint.session": func(ctx context.Context, caller BrokerCaller, _ json.RawMessage) (any, error) {
			h := &storeBridgeHost{s: db}
			session, err := h.Session(ctx, caller.TaskID, caller.ExecutionID)
			if err != nil {
				return nil, refuse("rejected", err.Error())
			}
			session.Models = deps.ClaudeModels
			return session, nil
		},
		"endpoint.bind": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			endpoint, err := endpointArg(raw)
			if err != nil {
				return nil, err
			}
			h, err := host(ctx, caller)
			if err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, h.Bind(ctx, endpoint)
		},
		"endpoint.unbind": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			endpoint, err := endpointArg(raw)
			if err != nil {
				return nil, err
			}
			h, err := host(ctx, caller)
			if err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, h.Unbind(ctx, endpoint)
		},
		"resume.save": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			var args struct {
				Kind              string `json:"kind"`
				ProviderSessionID string `json:"provider_session_id"`
				CWD               string `json:"cwd"`
				StorageDir        string `json:"storage_dir"`
			}
			if err := json.Unmarshal(raw, &args); err != nil || args.Kind == "" || args.ProviderSessionID == "" {
				return nil, refuse("bad_request", "kind and provider_session_id are required")
			}
			h, err := host(ctx, caller)
			if err != nil {
				return nil, err
			}
			session, _ := h.Session(ctx, caller.TaskID, caller.ExecutionID)
			if !session.RecordsIdentity {
				return nil, refuse("forbidden", "this session's conversation is not the task's to resume")
			}
			if err := h.SaveIdentity(ctx, args.Kind, args.ProviderSessionID, args.CWD, args.StorageDir); err != nil {
				return nil, refuse("rejected", err.Error())
			}
			return map[string]bool{"ok": true}, nil
		},
	}
}
