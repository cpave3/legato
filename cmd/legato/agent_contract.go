package main

import (
	"fmt"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
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
	case "summary":
		return runAgentSummaryContract(args[1:])
	case "status":
		return runAgentStatusContract(args[1:])
	default:
		return renderCommandError("agent", usageError("unknown_command", fmt.Sprintf("unknown agent command %q", args[0])), false)
	}
}

func runAgentStateContract(args []string) int {
	const command = "agent.state"
	parsed, parseErr := parseCommandArgs(args, map[string]flagSpec{
		"activity": {}, "working-dir": {}, "json": {Boolean: true},
	})
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
	if err := cli.AgentState(db, taskID, parsed.Values["activity"], parsed.Values["working-dir"], pushNotifier, osNotifier); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{
		"task_id": taskID, "activity": parsed.Values["activity"], "working_dir": parsed.Values["working-dir"],
	}, "", jsonMode)
}

func runAgentSessionCreatedContract(args []string) int {
	const command = "agent.session-created"
	parsed, parseErr := parseCommandArgs(args, map[string]flagSpec{"json": {Boolean: true}})
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
	if err := cli.AgentSessionCreated(db, parsed.Positionals[0], parsed.Positionals[1]); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": parsed.Positionals[0], "session_id": parsed.Positionals[1]}, "", jsonMode)
}

func runAgentSummaryContract(args []string) int {
	const command = "agent.summary"
	parsed, parseErr := parseCommandArgs(args, map[string]flagSpec{"exclude": {}, "json": {Boolean: true}})
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
	parsed, parseErr := parseCommandArgs(args, map[string]flagSpec{"format": {}, "json": {Boolean: true}})
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
