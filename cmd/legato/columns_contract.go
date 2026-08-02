package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

func runColumnsSyncContract(args []string) int {
	const command = "columns.sync"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("columns", "sync"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "columns sync accepts no positional arguments"), jsonMode)
	}

	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	defer db.Close()

	desired := make([]store.ColumnMapping, len(cfg.Board.Columns))
	for i, column := range cfg.Board.Columns {
		statuses := column.RemoteStatuses
		if statuses == nil {
			statuses = []string{}
		}
		encoded, marshalErr := json.Marshal(statuses)
		if marshalErr != nil {
			return renderCommandError(command, &commandError{Code: "config_invalid", Message: marshalErr.Error(), Exit: exitEnvironment}, jsonMode)
		}
		desired[i] = store.ColumnMapping{
			ColumnName:       column.Name,
			RemoteStatuses:   string(encoded),
			RemoteTransition: column.RemoteTransitionID,
			SortOrder:        i,
		}
	}

	result, err := db.SyncColumnMappings(context.Background(), desired)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	ipc.Broadcast(ipc.Message{Type: "task_update"})

	var text strings.Builder
	fmt.Fprintf(&text, "Synced %d columns", result.ColumnCount)
	if len(result.Added) > 0 {
		fmt.Fprintf(&text, "\nAdded: %s", strings.Join(result.Added, ", "))
	}
	if len(result.Updated) > 0 {
		fmt.Fprintf(&text, "\nUpdated: %s", strings.Join(result.Updated, ", "))
	}
	if len(result.Removed) > 0 {
		fmt.Fprintf(&text, "\nRemoved: %s", strings.Join(result.Removed, ", "))
		fmt.Fprintf(&text, "\nMoved %d tasks to %s", result.TasksMoved, result.Fallback)
	}
	text.WriteByte('\n')
	return writeCommandSuccess(command, result, text.String(), jsonMode)
}
