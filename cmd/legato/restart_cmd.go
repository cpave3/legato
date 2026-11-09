package main

import (
	"syscall"

	"github.com/cpave3/legato/config"
)

// syscallZero checks a process exists without signalling it.
var syscallZero = syscall.Signal(0)

// runRestartCmd is `legato restart`: every Legato process of this
// instance's data folder (the TUI, `legato serve`) shuts down and starts
// again in place. Agents keep running in tmux.
func runRestartCmd(args []string) int {
	const command = "restart"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs(command))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "restart accepts no positional arguments"), jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	restartAll(config.ResolveDataDir(cfg))
	return writeCommandSuccess(command, map[string]any{"restarting": true}, "Asked Legato to restart.", jsonMode)
}
