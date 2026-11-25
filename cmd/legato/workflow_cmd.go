package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

var workflowServiceFactory = func(db *store.Store, cfg *config.Config) (*service.WorkflowService, error) {
	wd, _ := os.Getwd()
	bus := events.New()
	board := service.NewBoardService(db, bus)
	syncService := newTicketSync(cfg, db, bus, nil)
	svc, err := buildWorkflowService(db, cfg, workflowRuntimeOptions{Bus: bus, Board: board, Sync: syncService, WorkDir: wd})
	if err != nil {
		return nil, err
	}
	// The CLI is not a host: `workflow step complete` runs in the agent's
	// own pane, so it records the step and serve or the TUI advances the
	// run (docs/ai/workflows.md § Agent completion).
	svc.SetRecordOnly()
	return svc, nil
}

func workflowDirs() (string, string) {
	global := filepath.Join(filepath.Dir(config.ResolveConfigPath()), "workflows")
	cwd, _ := os.Getwd()
	return global, filepath.Join(cwd, ".legato", "workflows")
}

func legacyWorkflowDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".legato", "workflows")
}

func workflowCommandError(err error) *commandError {
	return &commandError{Code: "workflow_error", Message: err.Error(), Exit: exitInternal}
}

func runWorkflowCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: legato workflow [list|add|validate|upgrade|run|runs|show|cancel|retry|step] ...")
		return 1
	}
	positional, flags, lists, err := parseWorkflowArgs(args[1:])
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), false)
	}
	if args[0] == "add" {
		return runWorkflowAdd(positional, flags)
	}
	if args[0] == "validate" {
		return runWorkflowValidate(positional, flagPresent(flags, "json"))
	}
	if args[0] == "upgrade" {
		return runWorkflowUpgrade(positional, flags)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), flags["json"] != "")
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), flags["json"] != "")
	}
	defer db.Close()
	svc, err := workflowServiceFactory(db, cfg)
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), flags["json"] != "")
	}
	ctx := context.Background()
	repositories := workflowRepositoriesFor(cfg, db)()
	verb := args[0]
	fail := func(err error) int {
		return renderCommandError("workflow."+verb, workflowCommandError(err), flags["json"] != "")
	}
	switch verb {
	case "list":
		return encodeWorkflowOutput(cli.WorkflowDefinitions(workflowFolders(repositories)), flags["json"] != "")
	case "runs":
		limit, _ := strconv.Atoi(flags["limit"])
		if err := cli.WorkflowRuns(ctx, svc, flags["status"], limit, os.Stdout); err != nil {
			return fail(err)
		}
	case "show":
		if len(positional) != 1 {
			return fail(fmt.Errorf("usage: legato workflow show <run-id>"))
		}
		if err := cli.WorkflowShow(ctx, svc, positional[0], os.Stdout); err != nil {
			return fail(err)
		}
	case "run":
		if len(positional) != 1 {
			return fail(fmt.Errorf("usage: legato workflow run <name> --input key=value"))
		}
		definition, err := resolveWorkflowDefinition(positional[0], repositories)
		if err != nil {
			return fail(err)
		}
		inputs, err := keyValues(lists["input"])
		if err != nil {
			return fail(err)
		}
		if err := cli.WorkflowRun(ctx, svc, definition, inputs, os.Stdout); err != nil {
			return fail(err)
		}
	case "cancel":
		if len(positional) != 1 {
			return fail(fmt.Errorf("usage: legato workflow cancel <run-id>"))
		}
		if err := cli.WorkflowCancel(ctx, svc, positional[0], os.Stdout); err != nil {
			return fail(err)
		}
	case "trigger":
		if len(positional) != 2 {
			return fail(fmt.Errorf("usage: legato workflow trigger <run-id> <step-id>"))
		}
		if err := cli.WorkflowTrigger(ctx, svc, positional[0], positional[1], flagPresent(flags, "force"), os.Stdout); err != nil {
			return fail(err)
		}
	case "retry":
		if len(positional) != 2 {
			return fail(fmt.Errorf("usage: legato workflow retry <run-id> <step-id>"))
		}
		if err := cli.WorkflowRetry(ctx, svc, positional[0], positional[1], flagPresent(flags, "force"), os.Stdout); err != nil {
			return fail(err)
		}
	case "step":
		if len(positional) < 1 {
			return fail(fmt.Errorf("usage: legato workflow step [complete|fail]"))
		}
		runID := firstNonEmpty(flags["run"], os.Getenv("LEGATO_WORKFLOW_RUN_ID"))
		stepID := firstNonEmpty(flags["step"], os.Getenv("LEGATO_WORKFLOW_STEP_ID"))
		attemptID := firstNonEmpty(flags["attempt"], os.Getenv("LEGATO_WORKFLOW_ATTEMPT_ID"))
		switch positional[0] {
		case "complete":
			values, err := keyValues(lists["output"])
			if err != nil {
				return fail(err)
			}
			artifacts, err := keyValues(lists["artifact"])
			if err != nil {
				return fail(err)
			}
			if err := cli.WorkflowComplete(ctx, svc, runID, stepID, attemptID, values, artifacts, os.Stdout); err != nil {
				return fail(err)
			}
		case "fail":
			if len(positional) != 2 {
				return fail(fmt.Errorf("failure reason is required"))
			}
			if err := cli.WorkflowFail(ctx, svc, runID, stepID, attemptID, positional[1], os.Stdout); err != nil {
				return fail(err)
			}
		default:
			return fail(fmt.Errorf("unknown workflow step command %q", positional[0]))
		}
	default:
		return fail(fmt.Errorf("unknown workflow command %q", verb))
	}
	return 0
}

// runWorkflowValidate checks a workflow file, or a discovered workflow by
// name, without opening Legato's database. A file's problems are printed
// as file:line:col: message, one a line, or as diagnostics with --json.
func runWorkflowValidate(positional []string, jsonMode bool) int {
	fail := func(err *commandError) int { return renderCommandError("workflow.validate", err, jsonMode) }
	if len(positional) != 1 {
		return fail(usageError("usage", "usage: legato workflow validate <name-or-file>"))
	}
	name := positional[0]
	data, err := os.ReadFile(name)
	if err != nil {
		var repositories []workflowengine.RepositoryPreset
		if cfg, err := config.Load(); err == nil {
			repositories = workflowRepositoryPresets(cfg)
		}
		definition, err := resolveWorkflowDefinition(name, repositories)
		if err != nil {
			return fail(workflowCommandError(err))
		}
		return encodeWorkflowOutput(definition, true)
	}
	definition, diagnostics := workflowengine.Check(name, data)
	if definition != nil && definition.HasChatFallback() {
		diagnostics = append(diagnostics, validateChatFallbacks(data, definition)...)
	}
	if definition != nil {
		for _, d := range diagnostics {
			fmt.Fprintf(os.Stderr, "%s:%d:%d: %s: %s\n", name, d.Line, d.Column, d.Severity, d.Message)
		}
		return encodeWorkflowOutput(definition, true)
	}
	if jsonMode {
		return fail(&commandError{
			Code: "workflow_invalid", Message: diagnostics[0].Message, Exit: exitUsage,
			Details: map[string]any{"diagnostics": diagnostics},
		})
	}
	for _, d := range diagnostics {
		fmt.Fprintf(os.Stderr, "%s:%s\n", name, d)
	}
	return exitUsage
}

// validateChatFallbacks warns of the file's fallback chat triggers that a
// discovered workflow's unfiltered trigger leaves nothing to answer. With
// no database, a trigger is on as its file says.
func validateChatFallbacks(data []byte, definition *workflowengine.Definition) []workflowengine.Diagnostic {
	var repositories []workflowengine.RepositoryPreset
	if cfg, err := config.Load(); err == nil {
		repositories = workflowRepositoryPresets(cfg)
	}
	others := workflowengine.DiscoverFolders(workflowFolders(repositories)).Definitions
	return workflowengine.ChatFallbackDiagnostics(data, definition, others, func(d *workflowengine.Definition, kind string) bool {
		for _, trigger := range d.On.List() {
			if trigger.Kind == kind {
				return trigger.EnabledByDefault
			}
		}
		return false
	})
}

func resolveWorkflowDefinition(name string, repositories []workflowengine.RepositoryPreset) (*workflowengine.Definition, error) {
	if data, err := os.ReadFile(name); err == nil {
		return workflowengine.Parse(name, data)
	}
	result := workflowengine.WithBuiltIns(workflowengine.DiscoverFolders(workflowFolders(repositories)), service.BuiltInWorkflows()...)
	for _, definition := range result.Definitions {
		if definition.Name == name {
			return definition, nil
		}
	}
	return nil, fmt.Errorf("workflow %q not found", name)
}
func keyValues(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, value := range values {
		key, val, ok := splitKeyValue(value)
		if !ok {
			return nil, fmt.Errorf("expected key=value, got %q", value)
		}
		out[key] = val
	}
	return out, nil
}
func splitKeyValue(value string) (string, string, bool) {
	for i, r := range value {
		if r == '=' && i > 0 {
			return value[:i], value[i+1:], true
		}
	}
	return "", "", false
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func encodeWorkflowOutput(value any, jsonMode bool) int {
	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(value)
	} else {
		_ = json.NewEncoder(os.Stdout).Encode(value)
	}
	return 0
}
