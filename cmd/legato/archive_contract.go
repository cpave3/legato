package main

import (
	"context"
	"fmt"
	"strings"
)

func runTaskArchiveListContract(args []string) int {
	const command = "task.archive.list"
	parsed, err := parseCommandArgs(args, commandFlagSpecs("task", "archive", "list"))
	jsonMode := parsed.Present["json"]
	if err != nil {
		return renderCommandError(command, err, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "archive list accepts no arguments"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	cards, listErr := db.ListArchivedTasks(context.Background())
	if listErr != nil {
		return renderCommandError(command, commandDomainError(listErr), jsonMode)
	}
	var text strings.Builder
	for _, card := range cards {
		fmt.Fprintf(&text, "%s\t%s\t%s\n", card.ID, card.Status, card.Title)
	}
	return writeCommandSuccess(command, cards, text.String(), jsonMode)
}

func runTaskArchiveRestoreContract(args []string) int {
	const command = "task.archive.restore"
	parsed, err := parseCommandArgs(args, commandFlagSpecs("task", "archive", "restore"))
	jsonMode := parsed.Present["json"]
	if err != nil {
		return renderCommandError(command, err, jsonMode)
	}
	if len(parsed.Positionals) != 1 {
		return renderCommandError(command, usageError("missing_argument", "archive restore requires one task ID"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	columns, restoreErr := db.ListColumnMappings(context.Background())
	if restoreErr == nil && len(columns) == 0 {
		restoreErr = fmt.Errorf("no configured columns")
	}
	if restoreErr == nil {
		restoreErr = db.RestoreTask(context.Background(), parsed.Positionals[0], columns[0].ColumnName)
	}
	if restoreErr != nil {
		return renderCommandError(command, commandDomainError(restoreErr), jsonMode)
	}
	return writeCommandSuccess(command, map[string]string{"task_id": parsed.Positionals[0]}, "Restored "+parsed.Positionals[0], jsonMode)
}
