package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
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
