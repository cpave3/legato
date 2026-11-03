package main

import (
	"github.com/cpave3/legato/internal/engine/version"
)

// runVersionCmd prints the version of this binary. It reads no
// configuration, so it runs anywhere: the updater runs a new binary this way
// before it trusts it.
func runVersionCmd(args []string) int {
	const command = "version"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("version"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "version accepts no positional arguments"), jsonMode)
	}
	data := map[string]any{"version": version.String(), "release": version.IsRelease()}
	return writeCommandSuccess(command, data, "legato "+version.String(), jsonMode)
}
