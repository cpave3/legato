package main

import (
	"encoding/json"
	"fmt"
	"os"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// runWorkflowUpgrade upgrades a schema_version 1 workflow file to version 2
// in place (LEG-484), as the web editor's Upgrade to v2 does: only what
// changes is rewritten, so comments stay. In a file of either version it
// rewrites the deprecated inline review step to call the built-in
// review-loop (LEG-599). --stdout prints the upgraded file
// and writes nothing. What the upgrade could not do is printed as
// file:line:col: severity: message, and exits 2: the file needs editing by
// hand. A file that does not load is refused, as validate prints it.
func runWorkflowUpgrade(positional []string, flags map[string]string) int {
	jsonMode, stdout := flagPresent(flags, "json"), flagPresent(flags, "stdout")
	fail := func(err *commandError) int { return renderCommandError("workflow.upgrade", err, jsonMode) }
	if len(positional) != 1 {
		return fail(usageError("usage", "usage: legato workflow upgrade <file> [--stdout] [--json]"))
	}
	path := positional[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(workflowCommandError(err))
	}
	result, err := workflowengine.Upgrade(path, data)
	if err != nil {
		_, diagnostics := workflowengine.Check(path, data)
		if jsonMode {
			return fail(&commandError{Code: "workflow_invalid", Message: err.Error(), Exit: exitUsage, Details: map[string]any{"diagnostics": diagnostics}})
		}
		for _, d := range diagnostics {
			fmt.Fprintf(os.Stderr, "%s:%s\n", path, d)
		}
		return exitUsage
	}
	if result.Changed && !stdout {
		if err := workflowengine.ReplaceFile(path, result.YAML, workflowengine.FileDigest(data)); err != nil {
			return fail(workflowCommandError(err))
		}
	}
	switch {
	case jsonMode:
		out := map[string]any{"path": path, "changed": result.Changed, "notes": result.Notes}
		if stdout {
			out["yaml"] = string(result.YAML)
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
	case stdout:
		_, _ = os.Stdout.Write(result.YAML)
	case !result.Changed:
		fmt.Fprintf(os.Stderr, "%s is schema_version %d already\n", path, workflowengine.LatestSchemaVersion)
	case wasVersion2(path, data):
		fmt.Fprintf(os.Stderr, "%s: deprecated steps rewritten (github.pr.review_passes now calls the built-in review-loop)\n", path)
	default:
		fmt.Fprintf(os.Stderr, "%s upgraded to schema_version %d\n", path, workflowengine.LatestSchemaVersion)
	}
	for _, note := range result.Notes {
		if !jsonMode {
			fmt.Fprintf(os.Stderr, "%s:%d:%d: %s: %s\n", path, note.Line, note.Column, note.Severity, note.Message)
		}
	}
	if len(result.Notes) > 0 {
		return exitUsage
	}
	return 0
}

// wasVersion2 says whether the file was version 2 before the upgrade.
func wasVersion2(path string, data []byte) bool {
	definition, err := workflowengine.Parse(path, data)
	return err == nil && definition.SchemaVersion == workflowengine.SchemaVersion2
}
