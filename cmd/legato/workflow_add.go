package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// errWorkflowExists refuses to overwrite a workflow file without --force.
var errWorkflowExists = errors.New("a workflow file is there already")

// workflowExampleInfo is one example `legato workflow add` can add.
type workflowExampleInfo struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// addedWorkflow is what `legato workflow add <name>` wrote.
type addedWorkflow struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// workflowExamples are the examples Legato embeds.
func workflowExamples() []workflowExampleInfo {
	var out []workflowExampleInfo
	for _, example := range workflowengine.Examples() {
		info := workflowExampleInfo{Name: example.Name}
		if definition, err := workflowengine.Parse(example.Name+".workflow.yaml", example.YAML); err == nil {
			info.Title, info.Description = definition.Title, strings.TrimSpace(definition.Description)
		}
		out = append(out, info)
	}
	return out
}

// addWorkflowExample writes an embedded example to the global workflows
// folder, or the project's with project, refusing to overwrite a file there
// unless force.
func addWorkflowExample(name, global, projectDir string, project, force bool) (addedWorkflow, error) {
	example, ok := workflowengine.ExampleNamed(name)
	if !ok && name == workflowengine.ReviewPassesWorkflowName {
		return addedWorkflow{}, fmt.Errorf("the built-in review-loop replaces review-pr-passes, and every Legato has it with nothing to add: legato workflow run review-loop --input repo=<owner/name> --input pr_number=<n>. An old review-pr-passes copy still runs; legato workflow upgrade <file> rewrites it to call review-loop")
	}
	if !ok {
		var names []string
		for _, e := range workflowengine.Examples() {
			names = append(names, e.Name)
		}
		return addedWorkflow{}, fmt.Errorf("no example workflow %q; the examples are %s", name, strings.Join(names, ", "))
	}
	dir := global
	if project {
		dir = projectDir
	}
	path := filepath.Join(dir, name+".workflow.yaml")
	if _, err := os.Stat(path); err == nil && !force {
		return addedWorkflow{}, fmt.Errorf("%w: %s (add --force to replace it)", errWorkflowExists, path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return addedWorkflow{}, err
	}
	if err := os.WriteFile(path, example.YAML, 0o644); err != nil {
		return addedWorkflow{}, err
	}
	return addedWorkflow{Name: name, Path: path}, nil
}

// runWorkflowAdd is `legato workflow add [<name>] [--project] [--force]
// [--json]`: with no name, the examples it can add.
func runWorkflowAdd(positional []string, flags map[string]string) int {
	jsonMode := flagPresent(flags, "json")
	global, project := workflowDirs()
	if len(positional) == 0 {
		examples := workflowExamples()
		if jsonMode {
			return encodeWorkflowOutput(examples, true)
		}
		fmt.Println("Example workflows to add (legato workflow add <name>):")
		for _, e := range examples {
			fmt.Printf("  %-18s %s\n", e.Name, e.Title)
		}
		return 0
	}
	if len(positional) != 1 {
		return renderCommandError("workflow.add", workflowCommandError(errors.New("usage: legato workflow add [<name>] [--project] [--force]")), jsonMode)
	}
	added, err := addWorkflowExample(positional[0], global, project, flagPresent(flags, "project"), flagPresent(flags, "force"))
	if err != nil {
		return renderCommandError("workflow.add", workflowCommandError(err), jsonMode)
	}
	if jsonMode {
		return encodeWorkflowOutput(added, true)
	}
	fmt.Printf("Added %s at %s. Start it with: legato workflow run %s --input ...\n", added.Name, added.Path, added.Name)
	return 0
}
