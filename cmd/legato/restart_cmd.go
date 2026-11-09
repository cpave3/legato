package main

import "syscall"

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
	// The configuration is not loaded: a restart after a bad edit is how
	// that edit is rolled back.
	restartAll()
	return writeCommandSuccess(command, map[string]any{"restarting": true}, "Asked Legato to restart.", jsonMode)
}
