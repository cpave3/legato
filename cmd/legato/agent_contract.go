package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
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
	case "stopped":
		return runAgentStoppedContract(args[1:])
	case "allow-read":
		return runAgentAllowReadContract(args[1:])
	case "codex":
		return runAgentCodexContract(args[1:])
	case "codex-bridge":
		return runAgentCodexBridgeContract(args[1:])
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
	if code, done := viaBroker(command, jsonMode, "agent.state", map[string]string{
		"task_id": taskID, "execution_id": parsed.Values["execution-id"], "activity": parsed.Values["activity"],
		"working_dir": parsed.Values["working-dir"], "session_id": parsed.Values["session-id"], "event": parsed.Values["event"],
	}, map[string]any{"task_id": taskID, "activity": parsed.Values["activity"]}); done {
		return code
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
	pushNotifier, osNotifier := agentPushNotifier(cfg), agentOSNotifier(cfg)
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

// claudeStopActivity reads a Claude Code Stop hook payload and returns the
// activity the session is left in: background while work the turn started (a
// shell, a subagent) is still in flight and will wake the session, idle
// otherwise. A payload it cannot read is idle too: a Stop hook that fails is
// shown to the person at the end of every turn.
func claudeStopActivity(reader io.Reader) string {
	var payload struct {
		BackgroundTasks []json.RawMessage `json:"background_tasks"`
	}
	if err := json.NewDecoder(reader).Decode(&payload); err != nil || len(payload.BackgroundTasks) == 0 {
		return ""
	}
	return store.ActivityBackground
}

func runAgentStoppedContract(args []string) int {
	const command = "agent.stopped"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "stopped"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent stopped accepts one task ID"), jsonMode)
	}
	stateArgs := []string{taskID, "--activity", claudeStopActivity(os.Stdin), "--event", "Stop"}
	for _, flag := range []string{"execution-id", "working-dir"} {
		if value := parsed.Values[flag]; value != "" {
			stateArgs = append(stateArgs, "--"+flag, value)
		}
	}
	if jsonMode {
		stateArgs = append(stateArgs, "--json")
	}
	return runAgentStateContract(stateArgs)
}

// runAgentAllowReadContract answers the Claude Code plugin's hook before a
// Read: it prints the output that allows the read when the file is an image
// the person pasted for this session, and nothing otherwise. It reads no
// configuration and no database, and it exits 0 whatever it is given: a hook
// that fails would get in the way of the tool call it was asked about.
func runAgentAllowReadContract(args []string) int {
	taskID := strings.TrimSpace(os.Getenv("LEGATO_TASK_ID"))
	if parsed, err := parseCommandArgs(args, commandFlagSpecs("agent", "allow-read")); err == nil && len(parsed.Positionals) == 1 {
		taskID = parsed.Positionals[0]
	}
	fmt.Print(cli.ClaudeReadPermission(os.Stdin, taskID))
	return 0
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
	if _, _, ok := brokerSession(); ok {
		taskID, sessionID := parsed.Positionals[0], parsed.Positionals[1]
		data := map[string]any{"task_id": taskID, "session_id": sessionID}
		register := true
		if parsed.Present["hook-payload"] {
			var err error
			if register, err = cli.SessionCreatedPayloadNamesTask(taskID, sessionID, os.Stdin); err != nil {
				return renderCommandError(command, commandDomainError(err), jsonMode)
			}
			data["registered"] = register
		}
		if !register {
			return writeCommandSuccess(command, data, "", jsonMode)
		}
		if code, done := viaBroker(command, jsonMode, "agent.session_created", map[string]string{"task_id": taskID, "session_id": sessionID}, data); done {
			return code
		}
		// No broker answered and the session can reach the store: the payload
		// was read, so link the session directly.
		db, code := openCommandStore(command, jsonMode)
		if db == nil {
			return code
		}
		defer db.Close()
		if err := cli.AgentSessionCreated(db, taskID, sessionID); err != nil {
			return renderCommandError(command, commandDomainError(err), jsonMode)
		}
		return writeCommandSuccess(command, data, "", jsonMode)
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

// agentPushNotifier is the ntfy notifier agent state changes use, or nil.
func agentPushNotifier(cfg *config.Config) service.Notifier {
	if cfg.Notifications.Ntfy.Topic == "" {
		return nil
	}
	return service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
}

// agentOSNotifier is the desktop notifier agent state changes use, or nil.
func agentOSNotifier(cfg *config.Config) service.Notifier {
	if !cfg.Notifications.OS.Enabled {
		return nil
	}
	return service.NewOSNotifier()
}

// brokerSession is the host broker's socket and this session's token, when
// the session was given them.
func brokerSession() (socket, token string, ok bool) {
	socket, token = os.Getenv("LEGATO_BROKER_SOCKET"), os.Getenv("LEGATO_BROKER_TOKEN")
	return socket, token, socket != "" && token != ""
}

// sandboxed reports whether this process runs in Legato's sandbox, where the
// store cannot be reached and the broker is the only way to the host.
func sandboxed() bool { return os.Getenv("LEGATO_SANDBOXED") == "1" }

// viaBroker asks the host broker to do op for this session. done is true
// when the command is finished: the broker answered, or none answered and
// the session cannot reach the store (a hook then exits 0: a failed state
// report must not fail the agent's turn). When none answered and the store
// can be reached, done is false and the command carries on with the store.
func viaBroker(command string, jsonMode bool, op string, args, data any) (int, bool) {
	socket, token, ok := brokerSession()
	if !ok {
		return 0, false
	}
	err := broker.Call(context.Background(), socket, token, op, args, nil)
	switch {
	case err == nil:
		return writeCommandSuccess(command, data, "", jsonMode), true
	case errors.Is(err, broker.ErrUnreachable) && sandboxed():
		fmt.Fprintf(os.Stderr, "legato: %s not reported: %v\n", command, err)
		return 0, true
	case errors.Is(err, broker.ErrUnreachable):
		return 0, false
	}
	var refusal *broker.Error
	if errors.As(err, &refusal) && (refusal.Code == "forbidden" || refusal.Code == "unauthorized") {
		return renderCommandError(command, &commandError{Code: refusal.Code, Message: refusal.Message, Exit: exitForbidden}, jsonMode), true
	}
	return renderCommandError(command, commandDomainError(err), jsonMode), true
}
