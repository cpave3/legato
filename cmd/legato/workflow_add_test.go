package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWorkflowAddWritesAnExampleIntoTheChosenFolder(t *testing.T) {
	global, project := filepath.Join(t.TempDir(), "global"), filepath.Join(t.TempDir(), ".legato", "workflows")
	added, err := addWorkflowExample("review-pr-passes", global, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if added.Path != filepath.Join(global, "review-pr-passes.workflow.yaml") {
		t.Fatalf("path = %s", added.Path)
	}
	data, err := os.ReadFile(added.Path)
	if err != nil || !strings.Contains(string(data), "github.pr.review_passes") {
		t.Fatalf("written = %q, %v", data, err)
	}

	added, err = addWorkflowExample("review-pr", global, project, true, false)
	if err != nil || added.Path != filepath.Join(project, "review-pr.workflow.yaml") {
		t.Fatalf("--project: %+v, %v", added, err)
	}
}

func TestWorkflowAddRefusesToOverwriteWithoutForce(t *testing.T) {
	global := t.TempDir()
	path := filepath.Join(global, "review-pr-passes.workflow.yaml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := addWorkflowExample("review-pr-passes", global, "", false, false); !errors.Is(err, errWorkflowExists) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "# mine\n" {
		t.Fatal("the edited copy was overwritten")
	}
	if _, err := addWorkflowExample("review-pr-passes", global, "", false, true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "review_passes") {
		t.Fatal("--force did not write the example")
	}
}

func TestWorkflowAddListsItsExamplesAndRefusesAnUnknownOne(t *testing.T) {
	names := []string{}
	for _, example := range workflowExamples() {
		names = append(names, example.Name)
	}
	joined := "," + strings.Join(names, ",") + ","
	if !sort.StringsAreSorted(names) || !strings.Contains(joined, ",review-pr,") || !strings.Contains(joined, ",review-pr-passes,") || !strings.Contains(joined, ",triage,") || !strings.Contains(joined, ",discuss-pr,") {
		t.Fatalf("examples = %v", names)
	}
	if _, err := addWorkflowExample("nope", t.TempDir(), "", false, false); err == nil || !strings.Contains(err.Error(), "review-pr-passes") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkflowAddIsReachableFromTheCommandLine(t *testing.T) {
	root := commandTree()
	for _, args := range [][]string{
		{"workflow", "add"},
		{"workflow", "add", "--json"},
		{"workflow", "add", "review-pr-passes", "--project", "--force"},
	} {
		node, _, _, cmdErr := resolveCommand(root, args)
		if cmdErr != nil {
			t.Fatalf("legato %s: %v", strings.Join(args, " "), cmdErr)
		}
		if node.Name != "add" || node.Run == nil {
			t.Fatalf("legato %s resolved to %q", strings.Join(args, " "), node.Name)
		}
	}
	if got := helpTextFor(root, []string{"workflow", "add"}); !strings.Contains(got, "--project") || !strings.Contains(got, "--force") {
		t.Fatalf("help for workflow add:\n%s", got)
	}
}
