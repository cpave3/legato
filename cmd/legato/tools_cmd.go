package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/service"
)

// cliTools are the tools legato tools acts on, with what closes them; tests
// replace it.
var cliTools = func() ([]service.ManagedTool, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	db, err := openStore(cfg)
	if err != nil {
		return nil, nil, err
	}
	return []service.ManagedTool{
		service.NewSTUpdater(service.ToolUpdaterOptions{}),
		service.NewChimeraUpdater(service.ToolUpdaterOptions{}),
		newClaudeTool(cfg, db),
	}, func() { db.Close() }, nil
}

// runToolsCmd runs legato tools list, update or rollback here, on the host:
// the tools' state is files under the data folder (Claude Code's under a
// lock), so it needs no running Legato. Running Legatos are then told to
// check their tools again.
func runToolsCmd(action string, args []string) int {
	command := "tools." + action
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("tools", action))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if sandboxed() {
		return renderCommandError(command, &commandError{Code: "host_only", Message: "legato tools runs on the host only, not in an agent's sandbox", Exit: exitForbidden}, jsonMode)
	}
	want := 1
	if action == "list" {
		want = 0
	}
	if len(parsed.Positionals) != want {
		return renderCommandError(command, usageError("usage", "usage: legato tools "+map[bool]string{true: "list", false: action + " <name>"}[want == 0]), jsonMode)
	}
	tools, closeTools, err := cliTools()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_failed", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	defer closeTools()
	ctx := context.Background()

	if action == "list" {
		statuses := make([]service.ToolStatus, 0, len(tools))
		for _, tool := range tools {
			statuses = append(statuses, tool.Check(ctx))
		}
		return writeCommandSuccess(command, statuses, formatTools(statuses), jsonMode)
	}

	name := parsed.Positionals[0]
	var tool service.ManagedTool
	for _, t := range tools {
		if t.Name() == name {
			tool = t
		}
	}
	if tool == nil {
		return renderCommandError(command, &commandError{Code: "not_found", Message: fmt.Sprintf("Legato keeps no tool named %q", name), Exit: exitNotFound}, jsonMode)
	}
	switch action {
	case "update":
		err = tool.Apply(ctx)
	case "rollback":
		rb, ok := tool.(service.RollbackTool)
		if !ok {
			err = service.ErrToolNotManaged
		} else {
			err = rb.Rollback(ctx)
		}
	}
	if err != nil {
		code := "failed"
		switch {
		case errors.Is(err, service.ErrToolNotManaged):
			code = "not_managed"
		case errors.Is(err, service.ErrNoUpdate):
			code = "no_update"
		case errors.Is(err, service.ErrNoLastGood):
			code = "no_last_good"
		}
		return renderCommandError(command, &commandError{Code: code, Message: err.Error(), Exit: exitInternal}, jsonMode)
	}
	ipc.Broadcast(ipc.Message{Type: ipcToolsChanged})
	st := tool.Status()
	return writeCommandSuccess(command, st, fmt.Sprintf("%s is now %s\n", st.Name, st.Version), jsonMode)
}

// formatTools is the tools for a person: name, version, state, and what a
// managed one can do.
func formatTools(statuses []service.ToolStatus) string {
	var b strings.Builder
	for _, st := range statuses {
		fmt.Fprintf(&b, "%-8s %-10s %s", st.Name, orDash(st.Version), st.State)
		if st.Available {
			fmt.Fprintf(&b, " (%s available)", st.Latest)
		}
		if st.LastGood != "" {
			fmt.Fprintf(&b, ", last known good %s", st.LastGood)
		}
		if st.Held != "" {
			fmt.Fprintf(&b, ", %s held back", st.Held)
		}
		if st.Error != "" {
			fmt.Fprintf(&b, ": %s", st.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
