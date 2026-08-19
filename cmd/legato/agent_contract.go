package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/service"
)

func runAgentContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("agent", usageError("missing_argument", "agent command is required"), false)
	}
	switch args[0] {
	case "state":
		return runAgentStateContract(args[1:])
	case "session-created":
		return runAgentSessionCreatedContract(args[1:])
	case "permission-resolved":
		return runAgentPermissionResolvedContract(args[1:])
	case "peers":
		return runAgentPeersContract(args[1:])
	case "message":
		return runAgentMessageContract(args[1:])
	case "broadcast":
		return runAgentBroadcastContract(args[1:])
	case "summary":
		return runAgentSummaryContract(args[1:])
	case "status":
		return runAgentStatusContract(args[1:])
	default:
		return renderCommandError("agent", usageError("unknown_command", fmt.Sprintf("unknown agent command %q", args[0])), false)
	}
}

var newAgentContractTmux = func() (service.TmuxManager, error) {
	return tmux.New(tmux.Options{})
}

func loadAgentServiceForContract(command string, jsonMode bool) (service.AgentService, *store.Store, int) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, err := openStore(cfg)
	if err != nil {
		return nil, nil, renderCommandError(command, &commandError{Code: "database_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	manager, err := newAgentContractTmux()
	if err != nil {
		db.Close()
		return nil, nil, renderCommandError(command, &commandError{Code: "dependency_unavailable", Message: err.Error(), Exit: exitDependency}, jsonMode)
	}
	wd, err := os.Getwd()
	if err != nil {
		db.Close()
		return nil, nil, renderCommandError(command, &commandError{Code: "environment_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	return service.NewAgentService(db, manager, wd, service.AgentServiceOptions{DisablePeerAwareness: true}), db, exitOK
}

func runAgentPeersContract(args []string) int {
	const command = "agent.peers"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "peers"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent peers accepts no positional arguments"), jsonMode)
	}
	taskID := strings.TrimSpace(parsed.Values["task"])
	if taskID == "" {
		taskID = strings.TrimSpace(os.Getenv("LEGATO_TASK_ID"))
	}
	executionID := strings.TrimSpace(parsed.Values["execution-id"])
	if executionID == "" {
		executionID = strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))
	}
	if taskID == "" && executionID == "" && strings.TrimSpace(parsed.Values["directory"]) == "" {
		return renderCommandError(command, &commandError{Code: "agent_context_missing", Message: "provide --task, --execution-id, --directory, or run inside a Legato agent", Exit: exitEnvironment}, jsonMode)
	}
	agents, db, code := loadAgentServiceForContract(command, jsonMode)
	if agents == nil {
		return code
	}
	defer db.Close()
	peers, err := agents.ListPeers(context.Background(), taskID, executionID, parsed.Values["directory"])
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	var text strings.Builder
	for _, peer := range peers {
		fmt.Fprintf(&text, "task=%s\texecution=%s\tactivity=%s\t%s\n", peer.TaskID, peer.ExecutionID, peer.Activity, peer.Title)
	}
	return writeCommandSuccess(command, map[string]any{"peers": peers}, text.String(), jsonMode)
}

func runAgentMessageContract(args []string) int {
	const command = "agent.message"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "message"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 2 || strings.TrimSpace(parsed.Positionals[1]) == "" {
		return renderCommandError(command, usageError("missing_argument", "agent message requires task ID and text"), jsonMode)
	}
	agents, db, code := loadAgentServiceForContract(command, jsonMode)
	if agents == nil {
		return code
	}
	defer db.Close()
	senderTaskID := strings.TrimSpace(os.Getenv("LEGATO_TASK_ID"))
	if err := agents.SendPeerMessage(context.Background(), senderTaskID, parsed.Positionals[0], parsed.Positionals[1]); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"recipient_id": parsed.Positionals[0], "queued": true}, "message queued", jsonMode)
}

func runAgentBroadcastContract(args []string) int {
	const command = "agent.broadcast"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "broadcast"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 1 || strings.TrimSpace(parsed.Positionals[0]) == "" {
		return renderCommandError(command, usageError("missing_argument", "agent broadcast requires text"), jsonMode)
	}
	taskID := strings.TrimSpace(parsed.Values["task"])
	if taskID == "" {
		taskID = strings.TrimSpace(os.Getenv("LEGATO_TASK_ID"))
	}
	executionID := strings.TrimSpace(parsed.Values["execution-id"])
	if executionID == "" {
		executionID = strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))
	}
	if taskID == "" && executionID == "" && strings.TrimSpace(parsed.Values["directory"]) == "" {
		return renderCommandError(command, &commandError{Code: "agent_context_missing", Message: "provide --task, --execution-id, --directory, or run inside a Legato agent", Exit: exitEnvironment}, jsonMode)
	}
	agents, db, code := loadAgentServiceForContract(command, jsonMode)
	if agents == nil {
		return code
	}
	defer db.Close()
	result, err := agents.BroadcastToPeers(context.Background(), taskID, executionID, parsed.Values["directory"], parsed.Positionals[0])
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	text := fmt.Sprintf("delivered to %d peer(s)", len(result.Delivered))
	for _, failure := range result.Failed {
		text += fmt.Sprintf("\nfailed %s: %s", failure.TaskID, failure.Error)
	}
	return writeCommandSuccess(command, result, text, jsonMode)
}

func runAgentStateContract(args []string) int {
	const command = "agent.state"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "state"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 || !parsed.Present["activity"] {
		return renderCommandError(command, usageError("missing_argument", "agent state requires --activity"), jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	var pushNotifier service.Notifier
	if cfg.Notifications.Ntfy.Topic != "" {
		pushNotifier = service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
	}
	var osNotifier service.Notifier
	if cfg.Notifications.OS.Enabled {
		osNotifier = service.NewOSNotifier()
	}
	report := cli.AgentStateReport{TaskID: taskID, ExecutionID: parsed.Values["execution-id"], Activity: parsed.Values["activity"], WorkingDir: parsed.Values["working-dir"], SessionID: parsed.Values["session-id"], Event: parsed.Values["event"]}
	if err := cli.AgentState(db, report, pushNotifier, osNotifier); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{
		"task_id": taskID, "activity": parsed.Values["activity"], "working_dir": parsed.Values["working-dir"],
	}, "", jsonMode)
}

type chimeraPermissionResolvedPayload struct {
	SessionStatus    string `json:"session_status"`
	RemainingPending *int   `json:"remaining_pending"`
}

func chimeraPermissionResolvedActivity(reader io.Reader) (string, error) {
	var payload chimeraPermissionResolvedPayload
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode PermissionResolved payload: %w", err)
	}
	if payload.RemainingPending == nil {
		return "", fmt.Errorf("remaining_pending is required")
	}
	switch payload.SessionStatus {
	case "waiting_for_permission":
		if *payload.RemainingPending < 1 {
			return "", fmt.Errorf("waiting_for_permission requires remaining_pending greater than zero")
		}
		return "waiting", nil
	case "running":
		if *payload.RemainingPending != 0 {
			return "", fmt.Errorf("running requires remaining_pending zero")
		}
		return "working", nil
	default:
		return "", fmt.Errorf("unsupported session_status %q", payload.SessionStatus)
	}
}

func runAgentPermissionResolvedContract(args []string) int {
	const command = "agent.permission-resolved"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "permission-resolved"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent permission-resolved accepts one task ID"), jsonMode)
	}
	activity, err := chimeraPermissionResolvedActivity(os.Stdin)
	if err != nil {
		return renderCommandError(command, usageError("invalid_payload", err.Error()), jsonMode)
	}
	stateArgs := []string{taskID, "--activity", activity, "--event", "PermissionResolved"}
	for _, flag := range []string{"execution-id", "working-dir", "session-id"} {
		if value := parsed.Values[flag]; value != "" {
			stateArgs = append(stateArgs, "--"+flag, value)
		}
	}
	if jsonMode {
		stateArgs = append(stateArgs, "--json")
	}
	return runAgentStateContract(stateArgs)
}

func runAgentSessionCreatedContract(args []string) int {
	const command = "agent.session-created"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "session-created"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 2 {
		return renderCommandError(command, usageError("missing_argument", "agent session-created requires task ID and session ID"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	data := map[string]any{"task_id": parsed.Positionals[0], "session_id": parsed.Positionals[1]}
	if parsed.Present["hook-payload"] {
		registered, err := cli.AgentSessionCreatedFromHook(db, parsed.Positionals[0], parsed.Positionals[1], os.Stdin)
		if err != nil {
			return renderCommandError(command, commandDomainError(err), jsonMode)
		}
		data["registered"] = registered
	} else if err := cli.AgentSessionCreated(db, parsed.Positionals[0], parsed.Positionals[1]); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, data, "", jsonMode)
}

func runAgentSummaryContract(args []string) int {
	const command = "agent.summary"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "summary"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent summary accepts no positional arguments"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	out, err := cli.AgentSummary(db, parsed.Values["exclude"])
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"summary": out}, out, jsonMode)
}

func runAgentStatusContract(args []string) int {
	const command = "agent.status"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "status"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 || !parsed.Present["format"] {
		return renderCommandError(command, usageError("missing_argument", "agent status requires --format"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	out, err := cli.AgentStatus(db, taskID, parsed.Values["format"])
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": taskID, "status": out, "format": parsed.Values["format"]}, out, jsonMode)
}
