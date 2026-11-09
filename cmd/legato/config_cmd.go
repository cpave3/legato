package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/service"
)

// runConfigShowCmd is `legato config show [--sources]`: the effective
// settings of the registry, with where each comes from.
func runConfigShowCmd(args []string) int {
	const command = "config.show"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("config", "show"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	defer db.Close()
	settings, err := service.NewSettingsService(db, nil).List(context.Background())
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	var text strings.Builder
	for _, s := range settings {
		value, _ := json.Marshal(s.Value)
		fmt.Fprintf(&text, "%s: %s", s.Key, strings.Trim(string(value), `"`))
		if parsed.Present["sources"] {
			fmt.Fprintf(&text, "  (%s)", sourceLabel(s))
		}
		text.WriteString("\n")
	}
	return writeCommandSuccess(command, map[string]any{"settings": settings}, text.String(), jsonMode)
}

func sourceLabel(s service.SettingValue) string {
	switch s.Source {
	case service.SettingFromFile:
		return "config.yaml, pinned"
	case service.SettingFromDatabase:
		label := "set here"
		if s.PendingRestart {
			label += ", applies at restart"
		}
		return label
	}
	return "default"
}

// runConfigExportCmd is `legato config export [--pinned]`: the settings set
// in the database (and with --pinned those config.yaml pins) as YAML shaped
// like config.yaml, secrets as names.
func runConfigExportCmd(args []string) int {
	const command = "config.export"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("config", "export"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	defer db.Close()
	out, err := service.NewSettingsService(db, nil).Export(context.Background(), parsed.Present["pinned"])
	if err != nil {
		return renderCommandError(command, commandDomainError(err), false)
	}
	_, _ = os.Stdout.Write(out)
	return exitOK
}

// runConfigImportCmd is `legato config import <file>`: the file (as `legato
// config export` writes it) becomes one change set to approve in the tray.
func runConfigImportCmd(args []string) int {
	const command = "config.import"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("config", "import"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 1 {
		return renderCommandError(command, usageError("missing_argument", "give the file to import"), jsonMode)
	}
	data, err := os.ReadFile(parsed.Positionals[0])
	if err != nil {
		return renderCommandError(command, &commandError{Code: "unreadable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	defer db.Close()
	changeSets := service.NewChangeSetService(service.ChangeSetOptions{
		Store: db, Approvals: service.NewApprovalService(db, service.ApprovalServiceOptions{}),
		Settings: service.NewSettingsService(db, nil), Classify: classifyCall,
	})
	out, err := changeSets.ProposeImport(context.Background(), data, "legato config import")
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	ipc.Broadcast(ipc.Message{Type: "approval_changed"})
	text := fmt.Sprintf("Change set %s: %d changes wait for your approval in the approval tray (approval %s, needs %s).", out.ID, out.Calls, out.ApprovalID, out.Level)
	if len(out.Skipped) > 0 {
		text += "\nLeft out, since they live only in config.yaml: " + strings.Join(out.Skipped, ", ")
	}
	return writeCommandSuccess(command, out, text, jsonMode)
}
