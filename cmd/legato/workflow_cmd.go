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
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

var workflowServiceFactory = func(db *store.Store, cfg *config.Config) (*service.WorkflowService, error) {
	wd, _ := os.Getwd()
	return buildWorkflowService(db, cfg, nil, nil, nil, wd)
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
		fmt.Fprintln(os.Stderr, "usage: legato workflow [list|validate|run|runs|show|cancel|retry|step] ...")
		return 1
	}
	positional, flags, lists, err := parseWorkflowArgs(args[1:])
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), false)
	}
	cfg, err := config.Load()
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), flags["json"] != "")
	}
	db, err := store.New(config.ResolveDBPath(cfg))
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), flags["json"] != "")
	}
	defer db.Close()
	svc, err := workflowServiceFactory(db, cfg)
	if err != nil {
		return renderCommandError("workflow."+args[0], workflowCommandError(err), flags["json"] != "")
	}
	ctx := context.Background()
	global, project := workflowDirs()
	verb := args[0]
	fail := func(err error) int {
		return renderCommandError("workflow."+verb, workflowCommandError(err), flags["json"] != "")
	}
	switch verb {
	case "list":
		return encodeWorkflowOutput(cli.WorkflowDefinitions(global, project), flags["json"] != "")
	case "validate":
		if len(positional) != 1 {
			return fail(fmt.Errorf("usage: legato workflow validate <name-or-file>"))
		}
		definition, err := resolveWorkflowDefinition(positional[0], global, project)
		if err != nil {
			return fail(err)
		}
		return encodeWorkflowOutput(definition, true)
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
		definition, err := resolveWorkflowDefinition(positional[0], global, project)
		if err != nil {
			return fail(err)
		}
		inputs, err := keyValues(lists["input"])
		if err != nil {
			return fail(err)
		}
		if err := cli.WorkflowRun(ctx, svc, definition, inputs, nil, os.Stdout); err != nil {
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
		if err := cli.WorkflowTrigger(ctx, svc, positional[0], positional[1], os.Stdout); err != nil {
			return fail(err)
		}
	case "retry":
		if len(positional) != 2 {
			return fail(fmt.Errorf("usage: legato workflow retry <run-id> <step-id>"))
		}
		if err := cli.WorkflowRetry(ctx, svc, positional[0], positional[1], os.Stdout); err != nil {
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

func resolveWorkflowDefinition(name, global, project string) (*workflowengine.Definition, error) {
	if data, err := os.ReadFile(name); err == nil {
		return workflowengine.Parse(name, data)
	}
	result := workflowengine.DiscoverPaths(legacyWorkflowDir(), global, project)
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
