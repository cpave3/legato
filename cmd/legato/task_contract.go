package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func runTaskContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("task", usageError("missing_argument", "usage: legato task <command> [options]"), false)
	}
	switch args[0] {
	case "list":
		return runTaskListContract(args[1:])
	case "create":
		return runTaskCreateContract(args[1:])
	case "show":
		return runTaskShowContract(args[1:])
	case "update":
		return runTaskUpdateContract(args[1:])
	case "description":
		return runTaskDescriptionContract(args[1:])
	case "note":
		return runTaskNoteContract(args[1:])
	case "notes":
		return runTaskNotesContract(args[1:])
	case "link":
		return runTaskLinkContract(args[1:])
	case "unlink":
		return runTaskUnlinkContract(args[1:])
	case "worktree":
		return runTaskWorktreeContract(args[1:])
	case "github":
		return runTaskGitHubContract(args[1:])
	default:
		return renderCommandError("task", usageError("unknown_command", fmt.Sprintf("unknown task command %q", args[0])), false)
	}
}

func runTaskListContract(args []string) int {
	const command = "task.list"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "list"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "task list accepts no arguments"), jsonMode)
	}
	limit := 0
	if parsed.Present["limit"] {
		value, err := strconv.Atoi(parsed.Values["limit"])
		if err != nil || value < 1 {
			return renderCommandError(command, usageError("invalid_value", "--limit must be a positive integer"), jsonMode)
		}
		limit = value
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	tasks, err := cli.TaskList(db, cli.TaskListOptions{
		Status: parsed.Values["status"], Workspace: parsed.Values["workspace"], Limit: limit,
	})
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	var text strings.Builder
	for _, task := range tasks {
		fmt.Fprintf(&text, "%s\t%s\t%s", task.ID, task.Status, task.Title)
		if task.Workspace != "" {
			fmt.Fprintf(&text, "\t%s", task.Workspace)
		}
		text.WriteByte('\n')
	}
	return writeCommandSuccess(command, tasks, text.String(), jsonMode)
}

func openCommandStore(command string, jsonMode bool) (*store.Store, int) {
	cfg, err := config.Load()
	if err != nil {
		return nil, renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	db, err := openStore(cfg)
	if err != nil {
		return nil, renderCommandError(command, &commandError{Code: "database_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	return db, exitOK
}

func runTaskCreateContract(args []string) int {
	const command = "task.create"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "create"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) != 1 {
		return renderCommandError(command, usageError("missing_argument", "task create requires exactly one title"), jsonMode)
	}
	description, inputErr := readTextInput(parsed.Values["description"], parsed.Present["description"], parsed.Values["description-file"], parsed.Present["description-file"], os.Stdin)
	if inputErr != nil {
		return renderCommandError(command, inputErr, jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	if _, err := service.SeedAndResolveWorkspaceDefaults(context.Background(), db, cfg); err != nil {
		return renderCommandError(command, &commandError{Code: "workspace_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	workspaceName := cfg.DefaultWorkspace
	if parsed.Present["workspace"] {
		workspaceName = parsed.Values["workspace"]
	}
	var card *service.Card
	if workspaceName == "" {
		card, err = cli.TaskCreate(db, parsed.Positionals[0], description, parsed.Values["status"], parsed.Values["priority"])
	} else {
		card, err = cli.TaskCreate(db, parsed.Positionals[0], description, parsed.Values["status"], parsed.Values["priority"], workspaceName)
	}
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, card, card.ID, jsonMode)
}

func runTaskShowContract(args []string) int {
	const command = "task.show"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "show"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "task show accepts one task ID"), jsonMode)
	}
	format := parsed.Values["format"]
	if format == "" {
		if jsonMode {
			format = "json"
		} else {
			format = "description"
		}
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	out, err := cli.TaskShow(db, taskID, format)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	var data any = out
	if format == "json" {
		if err := json.Unmarshal([]byte(out), &data); err != nil {
			return renderCommandError(command, &commandError{Code: "internal_error", Message: "encode task result", Exit: exitInternal}, jsonMode)
		}
	}
	return writeCommandSuccess(command, data, out, jsonMode)
}

func runTaskUpdateContract(args []string) int {
	const command = "task.update"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "update"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "task update accepts one task ID"), jsonMode)
	}
	description, inputErr := readTextInput(parsed.Values["description"], parsed.Present["description"], parsed.Values["description-file"], parsed.Present["description-file"], os.Stdin)
	if inputErr != nil {
		return renderCommandError(command, inputErr, jsonMode)
	}
	opts := cli.TaskUpdateOptions{}
	if parsed.Present["status"] {
		opts.Status = stringPointer(parsed.Values["status"])
	}
	if parsed.Present["title"] {
		opts.Title = stringPointer(parsed.Values["title"])
	}
	if parsed.Present["description"] || parsed.Present["description-file"] {
		opts.Description = &description
	}
	if parsed.Present["workspace"] {
		opts.Workspace = stringPointer(parsed.Values["workspace"])
	}
	if opts.Status == nil && opts.Title == nil && opts.Description == nil && opts.Workspace == nil {
		return renderCommandError(command, usageError("missing_argument", "task update requires at least one field"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	if err := cli.TaskUpdateFields(db, taskID, opts); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	data, err := taskJSONData(db, taskID)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, data, "", jsonMode)
}

func runTaskDescriptionContract(args []string) int {
	const command = "task.description"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "description"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	positionals := parsed.Positionals
	taskID := ""
	inline := parsed.Values["description"]
	hasInline := parsed.Present["description"]
	if len(positionals) == 2 {
		taskID, inline, hasInline, positionals = positionals[0], positionals[1], true, nil
	} else if len(positionals) == 1 && os.Getenv("LEGATO_TASK_ID") != "" {
		taskID, inline, hasInline, positionals = os.Getenv("LEGATO_TASK_ID"), positionals[0], true, nil
	} else {
		var idErr *commandError
		taskID, positionals, idErr = resolveTaskID(positionals)
		if idErr != nil {
			return renderCommandError(command, idErr, jsonMode)
		}
	}
	if len(positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "invalid task description arguments"), jsonMode)
	}
	description, inputErr := readTextInput(inline, hasInline, parsed.Values["description-file"], parsed.Present["description-file"], os.Stdin)
	if inputErr != nil {
		return renderCommandError(command, inputErr, jsonMode)
	}
	if !hasInline && !parsed.Present["description-file"] {
		return renderCommandError(command, usageError("missing_argument", "task description requires text or --description-file"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	if err := cli.TaskDescription(db, taskID, description); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": taskID, "description": description}, "", jsonMode)
}

func taskJSONData(db *store.Store, taskID string) (any, error) {
	out, err := cli.TaskShow(db, taskID, "json")
	if err != nil {
		return nil, err
	}
	var data any
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func commandDomainError(err error) *commandError {
	if errors.Is(err, store.ErrNotFound) {
		return &commandError{Code: "task_not_found", Message: err.Error(), Exit: exitNotFound}
	}
	if errors.Is(err, service.ErrRemoteTaskReadOnly) {
		return &commandError{Code: "remote_task_read_only", Message: err.Error(), Exit: exitForbidden}
	}
	if errors.Is(err, cli.ErrInvalidInput) {
		return &commandError{Code: "invalid_input", Message: err.Error(), Exit: exitUsage}
	}
	return &commandError{Code: "internal_error", Message: err.Error(), Exit: exitInternal}
}

func stringPointer(value string) *string { return &value }

func runTaskNoteContract(args []string) int {
	const command = "task.note"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "note"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID := ""
	message := ""
	switch {
	case len(parsed.Positionals) == 2:
		taskID, message = parsed.Positionals[0], parsed.Positionals[1]
	case len(parsed.Positionals) == 1 && os.Getenv("LEGATO_TASK_ID") != "":
		taskID, message = os.Getenv("LEGATO_TASK_ID"), parsed.Positionals[0]
	default:
		return renderCommandError(command, usageError("missing_argument", "task note requires a message and task context"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	if err := cli.TaskNote(db, taskID, message); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": taskID, "message": message}, "", jsonMode)
}

func runTaskNotesContract(args []string) int {
	const command = "task.notes"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "notes"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "task notes accepts one task ID"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	notes, err := cli.TaskNotes(db, taskID)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	var text strings.Builder
	for _, note := range notes {
		fmt.Fprintf(&text, "### %s", note.CreatedAt)
		if note.Source != "" {
			fmt.Fprintf(&text, " · %s", note.Source)
		}
		fmt.Fprintf(&text, "\n\n%s\n\n", note.Body)
	}
	return writeCommandSuccess(command, notes, text.String(), jsonMode)
}

func runTaskLinkContract(args []string) int {
	const command = "task.link"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("task", "link"))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "task link accepts one task ID"), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	if err := cli.TaskLink(db, taskID, parsed.Values["branch"], parsed.Values["repo"], parsed.Values["sha"]); err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": taskID, "linked": true}, fmt.Sprintf("Linked task %s", taskID), jsonMode)
}

func runTaskUnlinkContract(args []string) int {
	return runLegacyTaskContextMutation("task.unlink", args, func(db *store.Store, taskID string, rest []string) error {
		if len(rest) != 0 {
			return usageError("unexpected_argument", "task unlink accepts one task ID")
		}
		return cli.TaskUnlink(db, taskID)
	})
}

func runLegacyTaskContextMutation(command string, args []string, mutate func(*store.Store, string, []string) error) int {
	path := strings.Split(command, ".")
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs(path...))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	if err := mutate(db, taskID, rest); err != nil {
		var cmdErr *commandError
		if errors.As(err, &cmdErr) {
			return renderCommandError(command, cmdErr, jsonMode)
		}
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, map[string]any{"task_id": taskID, "updated": true}, "", jsonMode)
}

func runTaskGitHubContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("task.github", usageError("missing_argument", "task github requires context, refresh, or rediscover"), false)
	}
	command := "task.github." + args[0]
	parsed, parseErr := parseCommandArgs(args[1:], commandFlagSpecs("task", "github", args[0]))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, jsonMode)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", command+" accepts one task ID"), jsonMode)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	db, code := openCommandStore(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	if args[0] == "context" {
		repo := parsed.Values["repo"]
		parts := strings.Split(repo, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return renderCommandError(command, usageError("invalid_value", "--repo must be owner/name"), jsonMode)
		}
		codeContext := &store.CodeContext{Repo: repo, Branch: parsed.Values["branch"], HeadSHA: parsed.Values["sha"], BaseBranch: parsed.Values["base-branch"], RepoSource: "manual", BranchSource: "manual"}
		err = service.SetTaskCodeContext(context.Background(), db, taskID, codeContext)
	} else {
		client, _, clientErr := buildGitHubClient(cfg, nil)
		if clientErr != nil {
			return renderCommandError(command, commandDomainError(clientErr), jsonMode)
		}
		svc := service.NewPRTrackingServiceWithInspector(db, events.New(), client, 0, 0, checkoutInspector(), githubRepositoryMappings(cfg)...)
		actions := svc.(service.PRTaskActions)
		switch args[0] {
		case "refresh":
			err = actions.RefreshPR(context.Background(), taskID)
		case "rediscover":
			err = actions.RediscoverPR(context.Background(), taskID)
		default:
			return renderCommandError("task.github", usageError("unknown_command", fmt.Sprintf("unknown github command %q", args[0])), jsonMode)
		}
	}
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	ipc.Broadcast(ipc.Message{Type: "pr_linked", TaskID: taskID})
	data, err := taskJSONData(db, taskID)
	if err != nil {
		return renderCommandError(command, commandDomainError(err), jsonMode)
	}
	return writeCommandSuccess(command, data, "", jsonMode)
}

func runTaskWorktreeContract(args []string) int {
	if len(args) == 0 {
		return renderCommandError("task.worktree", usageError("missing_argument", "task worktree requires set or clear"), false)
	}
	switch args[0] {
	case "set":
		const command = "task.worktree.set"
		parsed, parseErr := parseCommandArgs(args[1:], commandFlagSpecs("task", "worktree", "set"))
		jsonMode := parsed.Present["json"]
		if parseErr != nil {
			return renderCommandError(command, parseErr, jsonMode)
		}
		taskID, rest, idErr := resolveTaskID(parsed.Positionals)
		if idErr != nil {
			return renderCommandError(command, idErr, jsonMode)
		}
		if len(rest) != 0 {
			return renderCommandError(command, usageError("unexpected_argument", "worktree set accepts one task ID"), jsonMode)
		}
		value := func(flag, env string) string {
			if parsed.Present[flag] {
				return parsed.Values[flag]
			}
			return os.Getenv(env)
		}
		meta := store.TaskWorktree{Path: value("path", "YG_WORKTREE"), PrimaryDir: value("primary-dir", "YG_PRIMARY"), Branch: value("branch", "YG_BRANCH"), BaseBranch: value("base-branch", "YG_BASE")}
		db, code := openCommandStore(command, jsonMode)
		if db == nil {
			return code
		}
		defer db.Close()
		recorder := service.NewWorktreeWorkflow(db, nil, checkoutInspector())
		if err := cli.TaskWorktreeSet(db, taskID, meta, recorder); err != nil {
			return renderCommandError(command, commandDomainError(err), jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"task_id": taskID, "worktree": meta}, "", jsonMode)
	case "clear":
		return runLegacyTaskContextMutation("task.worktree.clear", args[1:], func(db *store.Store, taskID string, rest []string) error {
			if len(rest) != 0 {
				return usageError("unexpected_argument", "worktree clear accepts one task ID")
			}
			return cli.TaskWorktreeClear(db, taskID)
		})
	default:
		return renderCommandError("task.worktree", usageError("unknown_command", fmt.Sprintf("unknown worktree command %q", args[0])), false)
	}
}
