package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// newMacroAgents is the agent service legato macro run starts agents
// through; tests replace it.
var newMacroAgents = func(cfg *config.Config, db *store.Store) (service.MacroAgents, error) {
	agents, _, err := commandAgentService(cfg, db, "", nil, events.New(), nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("starting agents requires tmux: %w", err)
	}
	starter, ok := agents.(service.MacroAgents)
	if !ok {
		return nil, errors.New("this agent service cannot start macros")
	}
	return starter, nil
}

// runMacroRunContract is legato macro run <name> <task> [--pr N].
func runMacroRunContract(args []string) int {
	const command = "macro.run"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("macro", "run"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 2 || strings.TrimSpace(parsed.Positionals[0]) == "" || strings.TrimSpace(parsed.Positionals[1]) == "" {
		return renderCommandError(command, usageError("missing_argument", "macro run requires a macro name and a task ID"), jsonMode)
	}
	name, taskID := strings.TrimSpace(parsed.Positionals[0]), strings.TrimSpace(parsed.Positionals[1])
	var prNumber int
	if raw := strings.TrimSpace(parsed.Values["pr"]); raw != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(raw, "#"))
		if err != nil || n <= 0 {
			return renderCommandError(command, usageError("invalid_input", "--pr takes a pull request number"), jsonMode)
		}
		prNumber = n
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	ctx := context.Background()
	macros := service.NewMacroService(service.NewSettingsService(db, nil))
	if _, err := macros.Get(ctx, name); err != nil {
		return renderCommandError(command, macroCommandError(err), jsonMode)
	}
	task, err := db.GetTask(ctx, taskID)
	if err != nil {
		return renderCommandError(command, commandDomainError(fmt.Errorf("task %s: %w", taskID, err)), jsonMode)
	}
	var pr *service.MacroPR
	if prNumber > 0 {
		if pr, err = taskPR(ctx, db, task, prNumber); err != nil {
			return renderCommandError(command, &commandError{Code: "pr_not_found", Message: err.Error(), Exit: exitNotFound}, jsonMode)
		}
	}
	agents, err := newMacroAgents(cfg, db)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "dependency_unavailable", Message: err.Error(), Exit: exitDependency}, jsonMode)
	}
	macros.SetAgents(agents)
	result, err := macros.Start(ctx, service.MacroStart{Name: name, TaskID: taskID, PR: pr})
	if err != nil {
		return renderCommandError(command, macroCommandError(err), jsonMode)
	}
	text := fmt.Sprintf("Started the agent on %s with %s", taskID, name)
	if result.Queued {
		text = fmt.Sprintf("%s queued as the next message to the agent on %s", name, taskID)
	}
	return writeCommandSuccess(command, result, text, jsonMode)
}

// taskPR finds one of a task's pull requests by number.
func taskPR(ctx context.Context, db *store.Store, task *store.Task, number int) (*service.MacroPR, error) {
	branches, err := db.ListTaskBranches(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	for _, pr := range service.TaskPullRequests(task, branches) {
		if pr.PRNumber == number && pr.Repo != "" {
			return &service.MacroPR{Repo: pr.Repo, Number: pr.PRNumber, URL: pr.PRURL}, nil
		}
	}
	return nil, fmt.Errorf("task %s has no pull request #%d", task.ID, number)
}

func macroCommandError(err error) *commandError {
	if errors.Is(err, service.ErrMacroNotFound) {
		return &commandError{Code: "macro_not_found", Message: err.Error(), Exit: exitNotFound}
	}
	return commandDomainError(err)
}
