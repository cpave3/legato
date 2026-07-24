package main

import (
	"encoding/json"
	"fmt"

	"github.com/cpave3/legato/internal/cli"
)

func runWorkspaceContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("workspace", usageError("missing_argument", "workspace command is required"), false)
	}
	if args[0] != "list" {
		return renderCommandError("workspace", usageError("unknown_command", fmt.Sprintf("unknown workspace command %q", args[0])), false)
	}
	const command = "workspace.list"
	parsed, parseErr := parseCommandArgs(args[1:], map[string]flagSpec{"format": {}, "json": {Boolean: true}})
	jsonMode := parsed.Present["json"] || parsed.Values["format"] == "json"
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "workspace list accepts no positional arguments"), jsonMode)
	}
	format := parsed.Values["format"]
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return renderCommandError(command, usageError("invalid_format", "workspace format must be text or json"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	queryFormat := format
	if jsonMode {
		queryFormat = "json"
	}
	out, err := cli.WorkspaceList(db, queryFormat)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	var data any = out
	if queryFormat == "json" {
		if err := json.Unmarshal([]byte(out), &data); err != nil {
			return renderCommandError(command, &commandError{Code: "internal_error", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
	}
	return writeCommandSuccess(command, data, out, jsonMode)
}
